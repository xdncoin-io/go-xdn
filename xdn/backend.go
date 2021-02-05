// Copyright 2014 The go-xdn Authors
// This file is part of the go-xdn library.
//
// The go-xdn library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-xdn library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-xdn library. If not, see <http://www.gnu.org/licenses/>.

// Package xdn implements the Dnp protocol.
package xdn

import (
	"errors"
	"fmt"
	"math/big"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/xdn/go-xdn/accounts"
	"github.com/xdn/go-xdn/common"
	"github.com/xdn/go-xdn/common/hexutil"
	"github.com/xdn/go-xdn/consensus"
	// "github.com/xdn/go-xdn/consensus/clique"
	// "github.com/xdn/go-xdn/consensus/ethash"
	"github.com/xdn/go-xdn/consensus/xdnoc"
	"github.com/xdn/go-xdn/core"
	"github.com/xdn/go-xdn/core/bloombits"
	"github.com/xdn/go-xdn/core/types"
	"github.com/xdn/go-xdn/core/vm"
	"github.com/xdn/go-xdn/xdn/downloader"
	"github.com/xdn/go-xdn/xdn/filters"
	"github.com/xdn/go-xdn/xdn/gasprice"
	"github.com/xdn/go-xdn/xdndb"
	"github.com/xdn/go-xdn/event"
	"github.com/xdn/go-xdn/internal/xdnapi"
	"github.com/xdn/go-xdn/log"
	"github.com/xdn/go-xdn/miner"
	"github.com/xdn/go-xdn/node"
	"github.com/xdn/go-xdn/p2p"
	"github.com/xdn/go-xdn/params"
	"github.com/xdn/go-xdn/rlp"
	"github.com/xdn/go-xdn/rpc"
)

type LesServer interface {
	Start(srvr *p2p.Server)
	Stop()
	Protocols() []p2p.Protocol
	SetBloomBitsIndexer(bbIndexer *core.ChainIndexer)
}

// Dnp implements the Dnp full node service.
type Dnp struct {
	config      *Config
	chainConfig *params.ChainConfig

	// Channel for shutting down the service
	shutdownChan  chan bool    // Channel for shutting down the xdn
	stopDbUpgrade func() error // stop chain db sequential key upgrade

	// Handlers
	txPool          *core.TxPool
	blockchain      *core.BlockChain
	protocolManager *ProtocolManager
	lesServer       LesServer

	// DB interfaces
	chainDb xdndb.Database // Block chain database

	eventMux       *event.TypeMux
	engine         consensus.Engine
	accountManager *accounts.Manager

	bloomRequests chan chan *bloombits.Retrieval // Channel receiving bloom data retrieval requests
	bloomIndexer  *core.ChainIndexer             // Bloom indexer operating during block imports

	ApiBackend *DnpApiBackend

	miner     *miner.Miner
	gasPrice  *big.Int
	xdnerbase common.Address

	networkId     uint64
	netRPCService *xdnapi.PublicNetAPI

	lock sync.RWMutex // Protects the variadic fields (e.g. gas price and xdnerbase)
}

func (s *Dnp) AddLesServer(ls LesServer) {
	s.lesServer = ls
	ls.SetBloomBitsIndexer(s.bloomIndexer)
}

// New creates a new Dnp object (including the
// initialisation of the common Dnp object)
func New(ctx *node.ServiceContext, config *Config) (*Dnp, error) {
	if config.SyncMode == downloader.LightSync {
		return nil, errors.New("can't run xdn.Dnp in light sync mode, use les.LightDnp")
	}
	if !config.SyncMode.IsValid() {
		return nil, fmt.Errorf("invalid sync mode %d", config.SyncMode)
	}
	chainDb, err := CreateDB(ctx, config, "chaindata")
	if err != nil {
		return nil, err
	}
	stopDbUpgrade := upgradeDeduplicateData(chainDb)
	chainConfig, genesisHash, genesisErr := core.SetupGenesisBlock(chainDb, config.Genesis)
	if _, ok := genesisErr.(*params.ConfigCompatError); genesisErr != nil && !ok {
		return nil, genesisErr
	}
	log.Info("Initialised chain configuration", "config", chainConfig)

	xdn := &Dnp{
		config:         config,
		chainDb:        chainDb,
		chainConfig:    chainConfig,
		eventMux:       ctx.EventMux,
		accountManager: ctx.AccountManager,
		engine:         CreateConsensusEngine(ctx, config, chainConfig, chainDb),
		shutdownChan:   make(chan bool),
		stopDbUpgrade:  stopDbUpgrade,
		networkId:      config.NetworkId,
		gasPrice:       config.GasPrice,
		xdnerbase:      config.Dnperbase,
		bloomRequests:  make(chan chan *bloombits.Retrieval),
		bloomIndexer:   NewBloomIndexer(chainDb, params.BloomBitsBlocks),
	}

	log.Info("Initialising Dnp protocol", "versions", ProtocolVersions, "network", config.NetworkId)

	if !config.SkipBcVersionCheck {
		bcVersion := core.GetBlockChainVersion(chainDb)
		if bcVersion != core.BlockChainVersion && bcVersion != 0 {
			return nil, fmt.Errorf("Blockchain DB version mismatch (%d / %d). Run gxdn upgradedb.\n", bcVersion, core.BlockChainVersion)
		}
		core.WriteBlockChainVersion(chainDb, core.BlockChainVersion)
	}

	vmConfig := vm.Config{EnablePreimageRecording: config.EnablePreimageRecording}
	xdn.blockchain, err = core.NewBlockChain(chainDb, xdn.chainConfig, xdn.engine, vmConfig)
	if err != nil {
		return nil, err
	}
	// Rewind the chain in case of an incompatible config upgrade.
	if compat, ok := genesisErr.(*params.ConfigCompatError); ok {
		log.Warn("Rewinding chain to upgrade configuration", "err", compat)
		xdn.blockchain.SetHead(compat.RewindTo)
		core.WriteChainConfig(chainDb, genesisHash, chainConfig)
	}
	xdn.bloomIndexer.Start(xdn.blockchain)

	if config.TxPool.Journal != "" {
		config.TxPool.Journal = ctx.ResolvePath(config.TxPool.Journal)
	}
	xdn.txPool = core.NewTxPool(config.TxPool, xdn.chainConfig, xdn.blockchain)

	if xdn.protocolManager, err = NewProtocolManager(xdn.chainConfig, config.SyncMode, config.NetworkId, xdn.eventMux, xdn.txPool, xdn.engine, xdn.blockchain, chainDb); err != nil {
		return nil, err
	}
	xdn.miner = miner.New(xdn, xdn.chainConfig, xdn.EventMux(), xdn.engine)
	xdn.miner.SetExtra(makeExtraData(config.ExtraData))

	xdn.ApiBackend = &DnpApiBackend{xdn, nil}
	gpoParams := config.GPO
	if gpoParams.Default == nil {
		gpoParams.Default = config.GasPrice
	}
	xdn.ApiBackend.gpo = gasprice.NewOracle(xdn.ApiBackend, gpoParams)

	return xdn, nil
}

func makeExtraData(extra []byte) []byte {
	if len(extra) == 0 {
		// create default extradata
		extra, _ = rlp.EncodeToBytes([]interface{}{
			uint(params.VersionMajor<<16 | params.VersionMinor<<8 | params.VersionPatch),
			"gxdn",
			runtime.Version(),
			runtime.GOOS,
		})
	}
	if uint64(len(extra)) > params.MaximumExtraDataSize {
		log.Warn("Miner extra data exceed limit", "extra", hexutil.Bytes(extra), "limit", params.MaximumExtraDataSize)
		extra = nil
	}
	return extra
}

// CreateDB creates the chain database.
func CreateDB(ctx *node.ServiceContext, config *Config, name string) (xdndb.Database, error) {
	db, err := ctx.OpenDatabase(name, config.DatabaseCache, config.DatabaseHandles)
	if err != nil {
		return nil, err
	}
	if db, ok := db.(*xdndb.LDBDatabase); ok {
		db.Meter("xdn/db/chaindata/")
	}
	return db, nil
}

// CreateConsensusEngine creates the required type of consensus engine instance for an Dnp service
func CreateConsensusEngine(ctx *node.ServiceContext, config *Config, chainConfig *params.ChainConfig, db xdndb.Database) consensus.Engine {
	//If proof-of-authority is requested, set it up
	// if chainConfig.Clique != nil {
	// 	return clique.New(chainConfig.Clique, db)
	// }
	// // Otherwise assume proof-of-work
	// switch {
	// case config.PowFake:
	// 	log.Warn("Dnpash used in fake mode")
	// 	return ethash.NewFaker()
	// case config.PowTest:
	// 	log.Warn("Dnpash used in test mode")
	// 	return ethash.NewTester()
	// case config.PowShared:
	// 	log.Warn("Dnpash used in shared mode")
	// 	return ethash.NewShared()
	// default:
	// 	engine := ethash.New(ctx.ResolvePath(config.DnpashCacheDir), config.DnpashCachesInMem, config.DnpashCachesOnDisk,
	// 		config.DnpashDatasetDir, config.DnpashDatasetsInMem, config.DnpashDatasetsOnDisk)
	// 	engine.SetThreads(-1) // Disable CPU mining
	// 	return engine
	// }
	return xdnoc.New()
}

// APIs returns the collection of RPC services the xdn package offers.
// NOTE, some of these services probably need to be moved to somewhere else.
func (s *Dnp) APIs() []rpc.API {
	apis := xdnapi.GetAPIs(s.ApiBackend)

	// Append any APIs exposed explicitly by the consensus engine
	apis = append(apis, s.engine.APIs(s.BlockChain())...)

	// Append all the local APIs and return
	return append(apis, []rpc.API{
		{
			Namespace: "xdn",
			Version:   "1.0",
			Service:   NewPublicDnpAPI(s),
			Public:    true,
		}, {
			Namespace: "xdn",
			Version:   "1.0",
			Service:   NewPublicMinerAPI(s),
			Public:    true,
		}, {
			Namespace: "xdn",
			Version:   "1.0",
			Service:   downloader.NewPublicDownloaderAPI(s.protocolManager.downloader, s.eventMux),
			Public:    true,
		}, {
			Namespace: "miner",
			Version:   "1.0",
			Service:   NewPrivateMinerAPI(s),
			Public:    false,
		}, {
			Namespace: "xdn",
			Version:   "1.0",
			Service:   filters.NewPublicFilterAPI(s.ApiBackend, false),
			Public:    true,
		}, {
			Namespace: "admin",
			Version:   "1.0",
			Service:   NewPrivateAdminAPI(s),
		}, {
			Namespace: "debug",
			Version:   "1.0",
			Service:   NewPublicDebugAPI(s),
			Public:    true,
		}, {
			Namespace: "debug",
			Version:   "1.0",
			Service:   NewPrivateDebugAPI(s.chainConfig, s),
		}, {
			Namespace: "net",
			Version:   "1.0",
			Service:   s.netRPCService,
			Public:    true,
		},
	}...)
}

func (s *Dnp) ResetWithGenesisBlock(gb *types.Block) {
	s.blockchain.ResetWithGenesisBlock(gb)
}

func (s *Dnp) Dnperbase() (eb common.Address, err error) {
	s.lock.RLock()
	xdnerbase := s.xdnerbase
	s.lock.RUnlock()

	if xdnerbase != (common.Address{}) {
		return xdnerbase, nil
	}
	if wallets := s.AccountManager().Wallets(); len(wallets) > 0 {
		if accounts := wallets[0].Accounts(); len(accounts) > 0 {
			return accounts[0].Address, nil
		}
	}
	return common.Address{}, fmt.Errorf("xdnerbase address must be explicitly specified")
}

// set in js console via admin interface or wrapper from cli flags
func (self *Dnp) SetDnperbase(xdnerbase common.Address) {
	self.lock.Lock()
	self.xdnerbase = xdnerbase
	self.lock.Unlock()

	self.miner.SetDnperbase(xdnerbase)
}

func (s *Dnp) StartMining(local bool) error {
	eb, err := s.Dnperbase()
	if err != nil {
		log.Error("Cannot start mining without xdnerbase", "err", err)
		return fmt.Errorf("xdnerbase missing: %v", err)
	}
	// if clique, ok := s.engine.(*clique.Clique); ok {
	// 	wallet, err := s.accountManager.Find(accounts.Account{Address: eb})
	// 	if wallet == nil || err != nil {
	// 		log.Error("Dnperbase account unavailable locally", "err", err)
	// 		return fmt.Errorf("signer missing: %v", err)
	// 	}
	// 	clique.Authorize(eb, wallet.SignHash)
	// }
	if local {
		// If local (CPU) mining is started, we can disable the transaction rejection
		// mechanism introduced to speed sync times. CPU mining on mainnet is ludicrous
		// so noone will ever hit this path, whereas marking sync done on CPU mining
		// will ensure that private networks work in single miner mode too.
		atomic.StoreUint32(&s.protocolManager.acceptTxs, 1)
	}
	go s.miner.Start(eb)
	return nil
}

func (s *Dnp) StopMining()         { s.miner.Stop() }
func (s *Dnp) IsMining() bool      { return s.miner.Mining() }
func (s *Dnp) Miner() *miner.Miner { return s.miner }

func (s *Dnp) AccountManager() *accounts.Manager  { return s.accountManager }
func (s *Dnp) BlockChain() *core.BlockChain       { return s.blockchain }
func (s *Dnp) TxPool() *core.TxPool               { return s.txPool }
func (s *Dnp) EventMux() *event.TypeMux           { return s.eventMux }
func (s *Dnp) Engine() consensus.Engine           { return s.engine }
func (s *Dnp) ChainDb() xdndb.Database            { return s.chainDb }
func (s *Dnp) IsListening() bool                  { return true } // Always listening
func (s *Dnp) DnpVersion() int                    { return int(s.protocolManager.SubProtocols[0].Version) }
func (s *Dnp) NetVersion() uint64                 { return s.networkId }
func (s *Dnp) Downloader() *downloader.Downloader { return s.protocolManager.downloader }

// Protocols implements node.Service, returning all the currently configured
// network protocols to start.
func (s *Dnp) Protocols() []p2p.Protocol {
	if s.lesServer == nil {
		return s.protocolManager.SubProtocols
	}
	return append(s.protocolManager.SubProtocols, s.lesServer.Protocols()...)
}

// Start implements node.Service, starting all internal goroutines needed by the
// Dnp protocol implementation.
func (s *Dnp) Start(srvr *p2p.Server) error {
	// Start the bloom bits servicing goroutines
	s.startBloomHandlers()

	// Start the RPC service
	s.netRPCService = xdnapi.NewPublicNetAPI(srvr, s.NetVersion())

	// Figure out a max peers count based on the server limits
	maxPeers := srvr.MaxPeers
	if s.config.LightServ > 0 {
		maxPeers -= s.config.LightPeers
		if maxPeers < srvr.MaxPeers/2 {
			maxPeers = srvr.MaxPeers / 2
		}
	}
	// Start the networking layer and the light server if requested
	s.protocolManager.Start(maxPeers)
	if s.lesServer != nil {
		s.lesServer.Start(srvr)
	}
	return nil
}

// Stop implements node.Service, terminating all internal goroutines used by the
// Dnp protocol.
func (s *Dnp) Stop() error {
	if s.stopDbUpgrade != nil {
		s.stopDbUpgrade()
	}
	s.bloomIndexer.Close()
	s.blockchain.Stop()
	s.protocolManager.Stop()
	if s.lesServer != nil {
		s.lesServer.Stop()
	}
	s.txPool.Stop()
	s.miner.Stop()
	s.eventMux.Stop()

	s.chainDb.Close()
	close(s.shutdownChan)

	return nil
}
