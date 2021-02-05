package xdnoc

import (
	"fmt"
	"errors"
	"github.com/xdn/go-xdn/common"
	//"github.com/xdn/go-xdn/crypto"
	"github.com/xdn/go-xdn/core/types"
	"github.com/xdn/go-xdn/consensus"
	"github.com/xdn/go-xdn/core/state"
	"github.com/xdn/go-xdn/rpc"
	"github.com/xdn/go-xdn/poc"
	//lru "github.com/hashicorp/golang-lru"
	"github.com/xdn/go-xdn/params"
	"time"
	"io/ioutil"
	"strings"
	"strconv"
	"math/big"
	"os"
	"runtime"
	"github.com/xdn/go-xdn/common/math"
	set "gopkg.in/fatih/set.v0"
)	

var (
	initBaseTarget = big.NewInt(5000000000000000)
	maxBaseTarget = big.NewInt(999999999999999999)

	blockReward  *big.Int = big.NewInt(1e+18)

	maxUncles                     = 2 // Maximum number of uncles allowed in a single block

	errLargeBlockTime    = errors.New("timestamp too big")
	errZeroBlockTime     = errors.New("timestamp equals parent's")
	errTooManyUncles     = errors.New("too many uncles")
	errDuplicateUncle    = errors.New("duplicate uncle")
	errUncleIsAncestor   = errors.New("uncle is ancestor")
	errDanglingUncle     = errors.New("uncle's parent is not ancestor")
)

type Dnpoc struct {

}

func New() *Dnpoc {
	return &Dnpoc{}
}

func (d *Dnpoc) Author(header *types.Header) (common.Address, error) {
	return header.Coinbase, nil
}

func (d *Dnpoc) VerifyHeader(chain consensus.ChainReader, header *types.Header, seal bool) error {
	number := header.Number.Uint64()
	if chain.GetHeader(header.Hash(), number) != nil {
		return nil
	}
	parent := chain.GetHeader(header.ParentHash, number-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	return d.verifyHeader(chain, header, parent, false, seal)
}

func (d *Dnpoc) VerifyHeaders(chain consensus.ChainReader, headers []*types.Header, seals []bool) (chan<- struct{}, <-chan error) {
	if len(headers) == 0 {
		abort, results := make(chan struct{}), make(chan error, len(headers))
		for i := 0; i < len(headers); i++ {
			results <- nil
		}
		return abort, results
	}

	// Spawn as many workers as allowed threads
	workers := runtime.GOMAXPROCS(0)
	if len(headers) < workers {
		workers = len(headers)
	}

	// Create a task channel and spawn the verifiers
	var (
		inputs = make(chan int)
		done   = make(chan int, workers)
		errors = make([]error, len(headers))
		abort  = make(chan struct{})
	)
	for i := 0; i < workers; i++ {
		go func() {
			for index := range inputs {
				errors[index] = d.verifyHeaderWorker(chain, headers, seals, index)
				done <- index
			}
		}()
	}

	errorsOut := make(chan error, len(headers))
	go func() {
		defer close(inputs)
		var (
			in, out = 0, 0
			checked = make([]bool, len(headers))
			inputs  = inputs
		)
		for {
			select {
			case inputs <- in:
				if in++; in == len(headers) {
					// Reached end of headers. Stop sending to workers.
					inputs = nil
				}
			case index := <-done:
				for checked[index] = true; checked[out]; out++ {
					errorsOut <- errors[out]
					if out == len(headers)-1 {
						return
					}
				}
			case <-abort:
				return
			}
		}
	}()
	return abort, errorsOut
}

func (d *Dnpoc) verifyHeaderWorker(chain consensus.ChainReader, headers []*types.Header, seals []bool, index int) error {
	var parent *types.Header
	if index == 0 {
		parent = chain.GetHeader(headers[0].ParentHash, headers[0].Number.Uint64()-1)
	} else if headers[index-1].Hash() == headers[index].ParentHash {
		parent = headers[index-1]
	}
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	if chain.GetHeader(headers[index].Hash(), headers[index].Number.Uint64()) != nil {
		return nil // known block
	}
	return d.verifyHeader(chain, headers[index], parent, false, seals[index])
}

func (d *Dnpoc) verifyHeader(chain consensus.ChainReader, header *types.Header, parent *types.Header, uncle bool, seal bool) error {
	// Ensure that the header's extra-data section is of a reasonable size
	if uint64(len(header.Extra)) > params.MaximumExtraDataSize {
		return fmt.Errorf("extra-data too long: %d > %d", len(header.Extra), params.MaximumExtraDataSize)
	}

	//fmt.Printf("haha header:%v, parent:%v\r\n", header, parent)


	// Verify the header's timestamp
	if uncle {
		if header.Time.Cmp(math.MaxBig256) > 0 {
			return errLargeBlockTime
		}
	} else {
		if header.Time.Cmp(big.NewInt(time.Now().Unix())) > 0 {
			return consensus.ErrFutureBlock
		}
	}
	if header.Time.Cmp(parent.Time) <= 0 {
		return errZeroBlockTime
	}
	// Verify the block's difficulty based in it's timestamp and parent's difficulty
	// expected := CalcDifficulty(chain.Config(), header.Time.Uint64(), parent)
	// if expected.Cmp(header.Difficulty) != 0 {
	// 	return fmt.Errorf("invalid difficulty: have %v, want %v", header.Difficulty, expected)
	// }
	// Verify that the gas limit is <= 2^63-1
	if header.GasLimit.Cmp(math.MaxBig63) > 0 {
		return fmt.Errorf("invalid gasLimit: have %v, max %v", header.GasLimit, math.MaxBig63)
	}
	// Verify that the gasUsed is <= gasLimit
	if header.GasUsed.Cmp(header.GasLimit) > 0 {
		return fmt.Errorf("invalid gasUsed: have %v, gasLimit %v", header.GasUsed, header.GasLimit)
	}

	// Verify that the gas limit remains within allowed bounds
	diff := new(big.Int).Set(parent.GasLimit)
	diff = diff.Sub(diff, header.GasLimit)
	diff.Abs(diff)

	limit := new(big.Int).Set(parent.GasLimit)
	limit = limit.Div(limit, params.GasLimitBoundDivisor)

	if diff.Cmp(limit) >= 0 || header.GasLimit.Cmp(params.MinGasLimit) < 0 {
		return fmt.Errorf("invalid gas limit: have %v, want %v += %v", header.GasLimit, parent.GasLimit, limit)
	}
	// Verify that the block number is parent's +1
	if diff := new(big.Int).Sub(header.Number, parent.Number); diff.Cmp(big.NewInt(1)) != 0 {
		return consensus.ErrInvalidNumber
	}
	// Verify the engine specific seal securing the block
	if seal {
		if err := d.VerifySeal(chain, header); err != nil {
			return err
		}
	}
	return nil
}

func (d *Dnpoc) verifyCascadingFields(chain consensus.ChainReader, header *types.Header, parents []*types.Header) error {
	return nil
}

func (d *Dnpoc) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	// Verify that there are at most 2 uncles included in this block
	if len(block.Uncles()) > maxUncles {
		return errTooManyUncles
	}
	// Gather the set of past uncles and ancestors
	uncles, ancestors := set.New(), make(map[common.Hash]*types.Header)

	number, parent := block.NumberU64()-1, block.ParentHash()
	for i := 0; i < 7; i++ {
		ancestor := chain.GetBlock(parent, number)
		if ancestor == nil {
			break
		}
		ancestors[ancestor.Hash()] = ancestor.Header()
		for _, uncle := range ancestor.Uncles() {
			uncles.Add(uncle.Hash())
		}
		parent, number = ancestor.ParentHash(), number-1
	}
	ancestors[block.Hash()] = block.Header()
	uncles.Add(block.Hash())

	// Verify each of the uncles that it's recent, but not an ancestor
	for _, uncle := range block.Uncles() {
		// Make sure every uncle is rewarded only once
		hash := uncle.Hash()
		if uncles.Has(hash) {
			return errDuplicateUncle
		}
		uncles.Add(hash)

		// Make sure the uncle has a valid ancestry
		if ancestors[hash] != nil {
			return errUncleIsAncestor
		}
		if ancestors[uncle.ParentHash] == nil || uncle.ParentHash == block.ParentHash() {
			return errDanglingUncle
		}
		if err := d.verifyHeader(chain, uncle, ancestors[uncle.ParentHash], true, true); err != nil {
			return err
		}
	}
	return nil
}

func (d *Dnpoc) VerifySeal(chain consensus.ChainReader, header *types.Header) error {
	gensig := header.GenSig
	number := header.Number.Uint64()
	lastTime := header.LastTime
	thisTime := header.Time
	plotID := header.PlotID.Uint64()
	nonce := header.Nonce.Uint64()
	baseTarget := header.BaseTarget
	addrPlotID := poc.CalcPlotID(header.Coinbase)

	if plotID != addrPlotID {
		return errors.New("plotID mismatch")
	}

	genHash := poc.GenHash(gensig, number)
	scoopID := poc.GetScoopID(genHash)

	cells := poc.GenCell(nonce, plotID)
	scoop_1 := cells[64 * scoopID : 64 * scoopID + 32]
	scoop_2 := cells[64 * scoopID + 32 : 64 * scoopID + 64]

	//fmt.Printf("haha scoop_1:%v,scoop_2:%v\r\n", scoop_1, scoop_2)

	target := poc.CalcTarget(scoop_1, scoop_2, gensig)
	ntarget := new(big.Int).SetBytes((target.Bytes())[24:])

	deadline := poc.CalcDeadLine(ntarget, baseTarget)

	//fmt.Printf("haha gensig=%v,plotID:%v,nonce:%v,scoopID:%v,target:%v,baseTarget:%v\r\n", gensig, plotID, nonce, scoopID, target, baseTarget)
	//fmt.Printf("haha ntarget:%v\r\n", ntarget)
	//fmt.Printf("haha verify deadline:%v, head_deadline:%v, lastTime:%v, thisTime:%v\r\n", deadline, header.DeadLine, lastTime, thisTime)

	if deadline.Cmp(header.DeadLine) != 0 {
		return errors.New("deadline compute error")
	}
	if !(new(big.Int).Add(deadline, lastTime).Cmp(thisTime) < 0) {
		return errors.New("deadline not satisfy")
	}
	now := time.Now().Unix()
	if new(big.Int).Sub(thisTime, new(big.Int).SetUint64(uint64(15))).Cmp(new(big.Int).SetUint64(uint64(now))) > 0 {
		return errors.New("time mismatch")
	}

	return nil
}

func (d *Dnpoc) Prepare(chain consensus.ChainReader, header *types.Header) error {
	header.Difficulty = new(big.Int).SetUint64(1)
	fmt.Printf("haha xdn:prepare\r\n")
	return nil
}

func (d *Dnpoc) Finalize(chain consensus.ChainReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header, receipts []*types.Receipt) (*types.Block, error) {
	// header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))
	// header.UncleHash = types.CalcUncleHash(nil)
	// return types.NewBlock(header, txs, nil, receipts), nil
	PocRewards(chain.Config(), state, header, uncles)
	header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))
	// Header seems complete, assemble into a block and return
	return types.NewBlock(header, txs, uncles, receipts), nil
}

func (d *Dnpoc) Seal(chain consensus.ChainReader, block *types.Block, stop <-chan struct{}) (*types.Block, error) {
	// header := block.Header()

	// fmt.Printf("haha Seal\r\n")
	// time.Sleep(30 * time.Second)
	number := block.Number()
	baseTarget := d.calcBaseTarget(chain, number.Uint64())

	// return block.WithSeal(header), nil
	abort := make(chan struct{})
	found := make(chan *types.Block)

	go func() {
		d.mine(block, baseTarget, abort, found)
	}()

	var result *types.Block
	select {
	case <-stop:
		// Outside abort, stop all miner threads
		close(abort)
	case result = <-found:
		// One of the threads found a block, abort all others
		close(abort)
	}
	// Wait for all miners to terminate and return the block
	return result, nil
}

func (d *Dnpoc) mine(block *types.Block, bt *big.Int, abort chan struct{}, found chan *types.Block) {
	// Extract some data from the header
	var (
		header = block.Header()
		//hash   = header.HashNoNonce().Bytes()
		//target = new(big.Int).Div(maxUint256, header.Difficulty)
		number  = header.Number.Uint64()
		lastTime = block.LastTime()
		gensig  = header.GenSig

		baseTarget = new(big.Int).Set(bt)

		minDeadLine = new(big.Int).SetUint64(9999999999)
		minPubID, minNonce uint64

		addrPlotID = poc.CalcPlotID(header.Coinbase)
	)
	// Start generating random nonces until we abort or find a good one
	// var (
	// 	attempts = int64(0)
	// 	nonce    = seed
	// )

	// 授权逻辑
	node, err := GetNodeByPlotID(addrPlotID)
	if err != nil {
		fmt.Printf("get node by plotid error:%v\r\n", err)
		return
	}

	genHash := poc.GenHash(gensig, number)
	scoopID := poc.GetScoopID(genHash)

	///data/gaom/ndp/test/P_DATA
	dirBytes, err := ioutil.ReadFile("PLOT")
	if err != nil {
		fmt.Printf("read plot file err\r\n")
		return
	}


	fmt.Printf("tmpnow=%v\r\n", time.Now().UnixNano() / 1e6)


	dirs := strings.Split(string(dirBytes), ",")
	for dx := 0; dx < len(dirs) - 1; dx++ {
		dirname := dirs[dx]
		fmt.Printf("dirs=%v, dirname=%v\r\n", dirs, dirname)
		rd, err := ioutil.ReadDir(dirname)
		if err != nil {
			fmt.Printf("read dir err: %v,err:%v\r\n", dirname, err)
			return
		}
		for _, fi := range rd {
			total := 0
			index := 0
			var pubID, nonce uint64
			var name string
			var data []byte
			LABEL_INNER_FOR:
				for {
					select {
					case <- abort:
						fmt.Printf("Dnpash nonce search aborted\r\n")
						return
					default:
						if total == 0 {
							name = fi.Name()
							ss := strings.Split(name, "_")
							if len(ss) != 3 {
								break LABEL_INNER_FOR
							} else {
								pubID, err = strconv.ParseUint(ss[0], 10, 64)
								if err != nil {
									break LABEL_INNER_FOR
								}
								if pubID != addrPlotID {
									fmt.Printf("plotID not same,pubID:%v, addrPlotID:%v\r\n", pubID, addrPlotID)
									break LABEL_INNER_FOR
								}
								nonce, err = strconv.ParseUint(ss[1], 10, 64)
								if err != nil {
									break LABEL_INNER_FOR
								}
								total, err = strconv.Atoi(ss[2])
								if err != nil {
									break LABEL_INNER_FOR
								}
								index = 0
							}

							func () {
								f, err := os.Open(dirname + "/" + name)
								defer f.Close()
								if err != nil {
									return
								}
								data = make([]byte, 64 * total)   // total scoops
								//offset := (4096 - scoopID) * 64 * total
								offset := scoopID * 64 * total
								_, _ = f.ReadAt(data, int64(offset))
								//fmt.Printf("haha file data:%v\r\n", data[0: 300])
							}()
						} else {
							//fmt.Printf("haha data:%v\r\n", data[64*index:64*index + 64])
							// auth
							if nonce + uint64(index) >= node.MinNonce && nonce + uint64(index) <= node.MaxNonce {

								scoop_1 := make([]byte, 32)
								scoop_2 := make([]byte, 32)
								for k := 0; k < 32; k++ {
									scoop_1[k] = data[64 * index + k]
									scoop_2[k] = data[64 * index + 32 + k] 
								}

								//fmt.Printf("haha:scoop_1:%v,scoop_2:%v\r\n", scoop_1, scoop_2)
								target := poc.CalcTarget(scoop_1, scoop_2, gensig)
								ntarget := new(big.Int).SetBytes((target.Bytes())[24:])

								//fmt.Printf("haha ntarget:%v\r\n", ntarget)

								deadline := poc.CalcDeadLine(ntarget, baseTarget)
								if deadline.Cmp(minDeadLine) < 0 {  // set the minimum deadline
									minDeadLine.Set(deadline) 
									minPubID = pubID
									minNonce = nonce + uint64(index)
								}

								now := time.Now().Unix()

								//fmt.Printf("haha scoopID:%v, baseTarget:%v, deadline:%v, minDeadLine:%v, minPubID:%v, minNonce:%v, lastTime:%v, now:%v\r\n", 
								//	scoopID, baseTarget, deadline, minDeadLine, minPubID, minNonce, lastTime, now)

								if new(big.Int).Add(minDeadLine, lastTime).Cmp(new(big.Int).SetUint64(uint64(now))) < 0 { // found
									// Correct nonce found, create a new header with it
									header = types.CopyHeader(header)
									header.Nonce = types.EncodeNonce(minNonce)
									header.PlotID = types.EncodeNonce(minPubID)
									header.GenSig = gensig
									header.Time = new(big.Int).SetUint64(uint64(now))
									fmt.Printf("haha baseTarget:%v\r\n", baseTarget)
									header.BaseTarget = new(big.Int).Set(baseTarget)
									header.DeadLine = new(big.Int).Set(minDeadLine)

									// Seal and return a block (if still needed)
									select {
									case found <- block.WithSeal(header):
										fmt.Printf("Dnpash nonce found and reported,nonce:%v\r\n", minNonce)
									case <-abort:
										fmt.Printf("Dnpash nonce found but discarded,nonce:%v\r\n", minNonce)
									}
									return
								}
							}

							//time.Sleep(time.Second)

							index++
							if index == total {
								break LABEL_INNER_FOR
							}
						}
					}
				}
		}
	}


	fmt.Printf("tmpnow111=%v\r\n", time.Now().UnixNano() / 1e6)


	// if find over, wait for time ok
	for {
		select {
			case <- abort:
				fmt.Printf("Dnpash nonce search aborted\r\n")
				return
			default:
				now := time.Now().Unix()
				if new(big.Int).Add(minDeadLine, lastTime).Cmp(new(big.Int).SetUint64(uint64(now))) < 0 { // found
					// Correct nonce found, create a new header with it
					header = types.CopyHeader(header)
					header.Nonce = types.EncodeNonce(minNonce)
					header.PlotID = types.EncodeNonce(minPubID)
					header.GenSig = gensig
					header.Time = new(big.Int).SetUint64(uint64(now))
					fmt.Printf("haha baseTarget:%v\r\n", baseTarget)
					header.BaseTarget = new(big.Int).Set(baseTarget)
					header.DeadLine = new(big.Int).Set(minDeadLine)

					// Seal and return a block (if still needed)
					select {
					case found <- block.WithSeal(header):
						fmt.Printf("Dnpash nonce found and reported,nonce:%v\r\n", minNonce)
					case <-abort:
						fmt.Printf("Dnpash nonce found but discarded,nonce:%v\r\n", minNonce)
					}
					return
				}
				time.Sleep(1 * time.Second)
		}
	}

}

// APIs implements consensus.Engine, returning the user facing RPC API to allow
// controlling the signer voting.
func (d *Dnpoc) APIs(chain consensus.ChainReader) []rpc.API {
	return nil
}


var (
	big8  = big.NewInt(8)
	big32 = big.NewInt(32)
)

func PocRewards(config *params.ChainConfig, state *state.StateDB, header *types.Header, uncles []*types.Header) {
	// Accumulate the rewards for the miner and any included uncles
	reward := new(big.Int).Set(blockReward)
	reward.Mul(reward, new(big.Int).SetUint64(15))
	reward.Div(reward, new(big.Int).SetUint64(2))
	r := new(big.Int)
	for _, uncle := range uncles {
		r.Add(uncle.Number, big8)
		r.Sub(r, header.Number)
		r.Mul(r, blockReward)
		r.Div(r, big8)
		state.AddBalance(uncle.Coinbase, r)

		r.Div(blockReward, big32)
		reward.Add(reward, r)
	}
	state.AddBalance(header.Coinbase, reward)
}

func (d *Dnpoc) calcBaseTarget(chain consensus.ChainReader, number uint64) *big.Int {
	if number <= uint64(5) {
		return initBaseTarget
	}
	aver := new(big.Int).SetUint64(0)
	timeDiff := new(big.Int).SetUint64(0)
	for i := uint64(number) - 1; i > uint64(number) - 5; i-- {
		h := chain.GetHeaderByNumber(i)
		aver.Add(aver, h.BaseTarget)
		timeDiff.Add(timeDiff, h.DeadLine)
	}
	aver.Div(aver, new(big.Int).SetUint64(4))
	timeDiff.Div(timeDiff, new(big.Int).SetUint64(4))
	newBaseTarget := new(big.Int).Set(aver)
	newBaseTarget.Mul(newBaseTarget, timeDiff)
	newBaseTarget.Div(newBaseTarget, new(big.Int).SetUint64(60))

	//if newBaseTarget.Cmp(new(big.Int)) < 0 || newBaseTarget.Cmp(new(big.Int)) == 0 || newBaseTarget.Cmp(maxBaseTarget) > 0 {
	if newBaseTarget.Cmp(new(big.Int)) < 0 {
		newBaseTarget.Set(initBaseTarget)
	}

	saver := new(big.Int).Set(aver)
	saver.Mul(saver, new(big.Int).SetUint64(9))
	saver.Div(saver, new(big.Int).SetUint64(10))

	if newBaseTarget.Cmp(saver) < 0 {
		newBaseTarget.Set(saver)
	} else {
		baver := new(big.Int).Set(aver)
		baver.Mul(baver, new(big.Int).SetUint64(11))
		baver.Div(baver, new(big.Int).SetUint64(10))

		if newBaseTarget.Cmp(baver) > 0 {
			newBaseTarget.Set(baver)
		}
	}

	return newBaseTarget
}
