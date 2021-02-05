// Copyright 2016 The go-xdn Authors
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

// Package les implements the Light Dnp Subprotocol.
package les

import (
	"fmt"
	"sync"
	"time"

	"github.com/xdn/go-xdn/accounts"
	"github.com/xdn/go-xdn/common"
	"github.com/xdn/go-xdn/common/hexutil"
	"github.com/xdn/go-xdn/consensus"
	"github.com/xdn/go-xdn/core"
	"github.com/xdn/go-xdn/core/bloombits"
	"github.com/xdn/go-xdn/core/types"
	"github.com/xdn/go-xdn/xdn"
	"github.com/xdn/go-xdn/xdn/downloader"
	"github.com/xdn/go-xdn/xdn/filters"
	"github.com/xdn/go-xdn/xdn/gasprice"
	"github.com/xdn/go-xdn/xdndb"
	"github.com/xdn/go-xdn/event"
	"github.com/xdn/go-xdn/internal/xdnapi"
	"github.com/xdn/go-xdn/light"
	"github.com/xdn/go-xdn/log"
	"github.com/xdn/go-xdn/node"
	"github.com/xdn/go-xdn/p2p"
	"github.com/xdn/go-xdn/p2p/discv5"
	"github.com/xdn/go-xdn/params"
	rpc "github.com/xdn/go-xdn/rpc"
)

type LightDnp struct {
	odr         *LesOdr
	relay       *LesTxRelay
	chainConfig *params.ChainConfig
	// Channel for shutting down the service
	shutdownChan chan bool
	// Handlers
	peers           *peerSet
	txPool          *light.TxPool
	blockchain      *light.LightChain
	protocolManager *ProtocolManager
	serverPool      *serverPool
	reqDist         *requestDistributor
	retriever       *retrieveManager
	// DB interfaces
	chainDb xdndb.Database // Block chain database

	bloomRequests                              chan chan *bloombits.Retrieval // Channel receiving bloom data retrieval requests
	bloomIndexer, chtIndexer, bloomTrieIndexer *core.ChainIndexer

	ApiBackend *LesApiBackend

	eventMux       *event.TypeMux
	engine         consensus.Engine
	accountManager *accounts.Manager

	networkId     uint64
	netRPCService *xdnapi.PublicNetAPI

	wg sync.WaitGroup
}

func New(ctx *node.ServiceContext, config *xdn.Config) (*LightDnp, error) {
	chainDb, err := xdn.CreateDB(ctx, config, "lightchaindata")
	if err != nil {
		return nil, err
	}
	chainConfig, genesisHash, genesisErr := core.SetupGenesisBlock(chainDb, config.Genesis)
	if _, isCompat := genesisErr.(*params.ConfigCompatError); genesisErr != nil && !isCompat {
		return nil, genesisErr
	}
	log.Info("Initialised chain configuration", "config", chainConfig)

	peers := newPeerSet()
	quitSync := make(chan struct{})

	lxdn := &LightDnp{
		chainConfig:      chainConfig,
		chainDb:          chainDb,
		eventMux:         ctx.EventMux,
		peers:            peers,
		reqDist:          newRequestDistributor(peers, quitSync),
		accountManager:   ctx.AccountManager,
		engine:           xdn.CreateConsensusEngine(ctx, config, chainConfig, chainDb),
		shutdownChan:     make(chan bool),
		networkId:        config.NetworkId,
		bloomRequests:    make(chan chan *bloombits.Retrieval),
		bloomIndexer:     xdn.NewBloomIndexer(chainDb, light.BloomTrieFrequency),
		chtIndexer:       light.NewChtIndexer(chainDb, true),
		bloomTrieIndexer: light.NewBloomTrieIndexer(chainDb, true),
	}

	lxdn.relay = NewLesTxRelay(peers, lxdn.reqDist)
	lxdn.serverPool = newServerPool(chainDb, quitSync, &lxdn.wg)
	lxdn.retriever = newRetrieveManager(peers, lxdn.reqDist, lxdn.serverPool)
	lxdn.odr = NewLesOdr(chainDb, lxdn.chtIndexer, lxdn.bloomTrieIndexer, lxdn.bloomIndexer, lxdn.retriever)
	if lxdn.blockchain, err = light.NewLightChain(lxdn.odr, lxdn.chainConfig, lxdn.engine); err != nil {
		return nil, err
	}
	lxdn.bloomIndexer.Start(lxdn.blockchain)
	// Rewind the chain in case of an incompatible config upgrade.
	if compat, ok := genesisErr.(*params.ConfigCompatError); ok {
		log.Warn("Rewinding chain to upgrade configuration", "err", compat)
		lxdn.blockchain.SetHead(compat.RewindTo)
		core.WriteChainConfig(chainDb, genesisHash, chainConfig)
	}

	lxdn.txPool = light.NewTxPool(lxdn.chainConfig, lxdn.blockchain, lxdn.relay)
	if lxdn.protocolManager, err = NewProtocolManager(lxdn.chainConfig, true, ClientProtocolVersions, config.NetworkId, lxdn.eventMux, lxdn.engine, lxdn.peers, lxdn.blockchain, nil, chainDb, lxdn.odr, lxdn.relay, quitSync, &lxdn.wg); err != nil {
		return nil, err
	}
	lxdn.ApiBackend = &LesApiBackend{lxdn, nil}
	gpoParams := config.GPO
	if gpoParams.Default == nil {
		gpoParams.Default = config.GasPrice
	}
	lxdn.ApiBackend.gpo = gasprice.NewOracle(lxdn.ApiBackend, gpoParams)
	return lxdn, nil
}

func lesTopic(genesisHash common.Hash, protocolVersion uint) discv5.Topic {
	var name string
	switch protocolVersion {
	case lpv1:
		name = "LES"
	case lpv2:
		name = "LES2"
	default:
		panic(nil)
	}
	return discv5.Topic(name + "@" + common.Bytes2Hex(genesisHash.Bytes()[0:8]))
}

type LightDummyAPI struct{}

// Dnperbase is the address that mining rewards will be send to
func (s *LightDummyAPI) Dnperbase() (common.Address, error) {
	return common.Address{}, fmt.Errorf("not supported")
}

// Coinbase is the address that mining rewards will be send to (alias for Dnperbase)
func (s *LightDummyAPI) Coinbase() (common.Address, error) {
	return common.Address{}, fmt.Errorf("not supported")
}

// Hashrate returns the POW hashrate
func (s *LightDummyAPI) Hashrate() hexutil.Uint {
	return 0
}

// Mining returns an indication if this node is currently mining.
func (s *LightDummyAPI) Mining() bool {
	return false
}

// APIs returns the collection of RPC services the xdn package offers.
// NOTE, some of these services probably need to be moved to somewhere else.
func (s *LightDnp) APIs() []rpc.API {
	return append(xdnapi.GetAPIs(s.ApiBackend), []rpc.API{
		{
			Namespace: "xdn",
			Version:   "1.0",
			Service:   &LightDummyAPI{},
			Public:    true,
		}, {
			Namespace: "xdn",
			Version:   "1.0",
			Service:   downloader.NewPublicDownloaderAPI(s.protocolManager.downloader, s.eventMux),
			Public:    true,
		}, {
			Namespace: "xdn",
			Version:   "1.0",
			Service:   filters.NewPublicFilterAPI(s.ApiBackend, true),
			Public:    true,
		}, {
			Namespace: "net",
			Version:   "1.0",
			Service:   s.netRPCService,
			Public:    true,
		},
	}...)
}

func (s *LightDnp) ResetWithGenesisBlock(gb *types.Block) {
	s.blockchain.ResetWithGenesisBlock(gb)
}

func (s *LightDnp) BlockChain() *light.LightChain      { return s.blockchain }
func (s *LightDnp) TxPool() *light.TxPool              { return s.txPool }
func (s *LightDnp) Engine() consensus.Engine           { return s.engine }
func (s *LightDnp) LesVersion() int                    { return int(s.protocolManager.SubProtocols[0].Version) }
func (s *LightDnp) Downloader() *downloader.Downloader { return s.protocolManager.downloader }
func (s *LightDnp) EventMux() *event.TypeMux           { return s.eventMux }

// Protocols implements node.Service, returning all the currently configured
// network protocols to start.
func (s *LightDnp) Protocols() []p2p.Protocol {
	return s.protocolManager.SubProtocols
}

// Start implements node.Service, starting all internal goroutines needed by the
// Dnp protocol implementation.
func (s *LightDnp) Start(srvr *p2p.Server) error {
	s.startBloomHandlers()
	log.Warn("Light client mode is an experimental feature")
	s.netRPCService = xdnapi.NewPublicNetAPI(srvr, s.networkId)
	// search the topic belonging to the oldest supported protocol because
	// servers always advertise all supported protocols
	protocolVersion := ClientProtocolVersions[len(ClientProtocolVersions)-1]
	s.serverPool.start(srvr, lesTopic(s.blockchain.Genesis().Hash(), protocolVersion))
	s.protocolManager.Start()
	return nil
}

// Stop implements node.Service, terminating all internal goroutines used by the
// Dnp protocol.
func (s *LightDnp) Stop() error {
	s.odr.Stop()
	if s.bloomIndexer != nil {
		s.bloomIndexer.Close()
	}
	if s.chtIndexer != nil {
		s.chtIndexer.Close()
	}
	if s.bloomTrieIndexer != nil {
		s.bloomTrieIndexer.Close()
	}
	s.blockchain.Stop()
	s.protocolManager.Stop()
	s.txPool.Stop()

	s.eventMux.Stop()

	time.Sleep(time.Millisecond * 200)
	s.chainDb.Close()
	close(s.shutdownChan)

	return nil
}
