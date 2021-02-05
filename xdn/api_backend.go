// Copyright 2015 The go-xdn Authors
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

package xdn

import (
	"context"
	"math/big"

	"github.com/xdn/go-xdn/accounts"
	"github.com/xdn/go-xdn/common"
	"github.com/xdn/go-xdn/common/math"
	"github.com/xdn/go-xdn/core"
	"github.com/xdn/go-xdn/core/bloombits"
	"github.com/xdn/go-xdn/core/state"
	"github.com/xdn/go-xdn/core/types"
	"github.com/xdn/go-xdn/core/vm"
	"github.com/xdn/go-xdn/xdn/downloader"
	"github.com/xdn/go-xdn/xdn/gasprice"
	"github.com/xdn/go-xdn/xdndb"
	"github.com/xdn/go-xdn/event"
	"github.com/xdn/go-xdn/params"
	"github.com/xdn/go-xdn/rpc"
)

// DnpApiBackend implements xdnapi.Backend for full nodes
type DnpApiBackend struct {
	xdn *Dnp
	gpo *gasprice.Oracle
}

func (b *DnpApiBackend) ChainConfig() *params.ChainConfig {
	return b.xdn.chainConfig
}

func (b *DnpApiBackend) CurrentBlock() *types.Block {
	return b.xdn.blockchain.CurrentBlock()
}

func (b *DnpApiBackend) SetHead(number uint64) {
	b.xdn.protocolManager.downloader.Cancel()
	b.xdn.blockchain.SetHead(number)
}

func (b *DnpApiBackend) HeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Header, error) {
	// Pending block is only known by the miner
	if blockNr == rpc.PendingBlockNumber {
		block := b.xdn.miner.PendingBlock()
		return block.Header(), nil
	}
	// Otherwise resolve and return the block
	if blockNr == rpc.LatestBlockNumber {
		return b.xdn.blockchain.CurrentBlock().Header(), nil
	}
	return b.xdn.blockchain.GetHeaderByNumber(uint64(blockNr)), nil
}

func (b *DnpApiBackend) BlockByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Block, error) {
	// Pending block is only known by the miner
	if blockNr == rpc.PendingBlockNumber {
		block := b.xdn.miner.PendingBlock()
		return block, nil
	}
	// Otherwise resolve and return the block
	if blockNr == rpc.LatestBlockNumber {
		return b.xdn.blockchain.CurrentBlock(), nil
	}
	return b.xdn.blockchain.GetBlockByNumber(uint64(blockNr)), nil
}

func (b *DnpApiBackend) StateAndHeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*state.StateDB, *types.Header, error) {
	// Pending state is only known by the miner
	if blockNr == rpc.PendingBlockNumber {
		block, state := b.xdn.miner.Pending()
		return state, block.Header(), nil
	}
	// Otherwise resolve the block number and return its state
	header, err := b.HeaderByNumber(ctx, blockNr)
	if header == nil || err != nil {
		return nil, nil, err
	}
	stateDb, err := b.xdn.BlockChain().StateAt(header.Root)
	return stateDb, header, err
}

func (b *DnpApiBackend) GetBlock(ctx context.Context, blockHash common.Hash) (*types.Block, error) {
	return b.xdn.blockchain.GetBlockByHash(blockHash), nil
}

func (b *DnpApiBackend) GetReceipts(ctx context.Context, blockHash common.Hash) (types.Receipts, error) {
	return core.GetBlockReceipts(b.xdn.chainDb, blockHash, core.GetBlockNumber(b.xdn.chainDb, blockHash)), nil
}

func (b *DnpApiBackend) GetTd(blockHash common.Hash) *big.Int {
	return b.xdn.blockchain.GetTdByHash(blockHash)
}

func (b *DnpApiBackend) GetEVM(ctx context.Context, msg core.Message, state *state.StateDB, header *types.Header, vmCfg vm.Config) (*vm.EVM, func() error, error) {
	state.SetBalance(msg.From(), math.MaxBig256)
	vmError := func() error { return nil }

	context := core.NewEVMContext(msg, header, b.xdn.BlockChain(), nil)
	return vm.NewEVM(context, state, b.xdn.chainConfig, vmCfg), vmError, nil
}

func (b *DnpApiBackend) SubscribeRemovedLogsEvent(ch chan<- core.RemovedLogsEvent) event.Subscription {
	return b.xdn.BlockChain().SubscribeRemovedLogsEvent(ch)
}

func (b *DnpApiBackend) SubscribeChainEvent(ch chan<- core.ChainEvent) event.Subscription {
	return b.xdn.BlockChain().SubscribeChainEvent(ch)
}

func (b *DnpApiBackend) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return b.xdn.BlockChain().SubscribeChainHeadEvent(ch)
}

func (b *DnpApiBackend) SubscribeChainSideEvent(ch chan<- core.ChainSideEvent) event.Subscription {
	return b.xdn.BlockChain().SubscribeChainSideEvent(ch)
}

func (b *DnpApiBackend) SubscribeLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return b.xdn.BlockChain().SubscribeLogsEvent(ch)
}

func (b *DnpApiBackend) SendTx(ctx context.Context, signedTx *types.Transaction) error {
	return b.xdn.txPool.AddLocal(signedTx)
}

func (b *DnpApiBackend) GetPoolTransactions() (types.Transactions, error) {
	pending, err := b.xdn.txPool.Pending()
	if err != nil {
		return nil, err
	}
	var txs types.Transactions
	for _, batch := range pending {
		txs = append(txs, batch...)
	}
	return txs, nil
}

func (b *DnpApiBackend) GetPoolTransaction(hash common.Hash) *types.Transaction {
	return b.xdn.txPool.Get(hash)
}

func (b *DnpApiBackend) GetPoolNonce(ctx context.Context, addr common.Address) (uint64, error) {
	return b.xdn.txPool.State().GetNonce(addr), nil
}

func (b *DnpApiBackend) Stats() (pending int, queued int) {
	return b.xdn.txPool.Stats()
}

func (b *DnpApiBackend) TxPoolContent() (map[common.Address]types.Transactions, map[common.Address]types.Transactions) {
	return b.xdn.TxPool().Content()
}

func (b *DnpApiBackend) SubscribeTxPreEvent(ch chan<- core.TxPreEvent) event.Subscription {
	return b.xdn.TxPool().SubscribeTxPreEvent(ch)
}

func (b *DnpApiBackend) Downloader() *downloader.Downloader {
	return b.xdn.Downloader()
}

func (b *DnpApiBackend) ProtocolVersion() int {
	return b.xdn.DnpVersion()
}

func (b *DnpApiBackend) SuggestPrice(ctx context.Context) (*big.Int, error) {
	return b.gpo.SuggestPrice(ctx)
}

func (b *DnpApiBackend) ChainDb() xdndb.Database {
	return b.xdn.ChainDb()
}

func (b *DnpApiBackend) EventMux() *event.TypeMux {
	return b.xdn.EventMux()
}

func (b *DnpApiBackend) AccountManager() *accounts.Manager {
	return b.xdn.AccountManager()
}

func (b *DnpApiBackend) BloomStatus() (uint64, uint64) {
	sections, _, _ := b.xdn.bloomIndexer.Sections()
	return params.BloomBitsBlocks, sections
}

func (b *DnpApiBackend) ServiceFilter(ctx context.Context, session *bloombits.MatcherSession) {
	for i := 0; i < bloomFilterThreads; i++ {
		go session.Multiplex(bloomRetrievalBatch, bloomRetrievalWait, b.xdn.bloomRequests)
	}
}

func (b *DnpApiBackend) GetPocBlocks(ctx context.Context, blockPage rpc.BlockNumber, blockCnt uint64) ([]*types.BlockSummary, error) {
	ch := b.xdn.blockchain.CurrentBlock().Header()
	rs := make([]*types.BlockSummary, 0)
	for i := ch.Number.Uint64() - uint64(blockPage - 1) * blockCnt; i + uint64(blockPage) * blockCnt > ch.Number.Uint64() && i >= uint64(0); i-- {
		bl := b.xdn.blockchain.GetBlockByNumber(i)
		if bl != nil {
			rs = append(rs, &types.BlockSummary{
				Coinbase: bl.Coinbase(),
				Root: bl.Hash(),
				Number: bl.Number(),
				Time: bl.Time(),
				TxCount: len(bl.Transactions()),
			})
		}
	}
	return rs, nil
}
