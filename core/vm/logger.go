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

package vm

import (
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"time"

	"github.com/xdn/go-xdn/common"
	"github.com/xdn/go-xdn/common/hexutil"
	"github.com/xdn/go-xdn/common/math"
	"github.com/xdn/go-xdn/core/types"
)

type Storage map[common.Hash]common.Hash

func (self Storage) Copy() Storage {
	cpy := make(Storage)
	for key, value := range self {
		cpy[key] = value
	}

	return cpy
}

// LogConfig are the configuration options for structured logger the EVM
type LogConfig struct {
	DisableMemory  bool // disable memory capture
	DisableStack   bool // disable stack capture
	DisableStorage bool // disable storage capture
	Limit          int  // maximum length of output, but zero means unlimited
}

//go:generate gencodec -type StructLog -field-override structLogMarshaling -out gen_structlog.go

// StructLog is emitted to the EVM each cycle and lists information about the current internal state
// prior to the execution of the statement.
type StructLog struct {
	Pc         uint64                      `json:"pc"`
	Op         OpCode                      `json:"op"`
	Gas        uint64                      `json:"gas"`
	GasCost    uint64                      `json:"gasCost"`
	Memory     []byte                      `json:"memory"`
	MemorySize int                         `json:"memSize"`
	Stack      []*big.Int                  `json:"stack"`
	Storage    map[common.Hash]common.Hash `json:"-"`
	Depth      int                         `json:"depth"`
	Err        error                       `json:"error"`
}

// overrides for gencodec
type structLogMarshaling struct {
	Stack   []*math.HexOrDecimal256
	Gas     math.HexOrDecimal64
	GasCost math.HexOrDecimal64
	Memory  hexutil.Bytes
	OpName  string `json:"opName"`
}

func (s *StructLog) OpName() string {
	return s.Op.String()
}

// Tracer is used to collect execution traces from an EVM transaction
// execution. CaptureState is called for each step of the VM with the
// current VM state.
// Note that reference types are actual VM data structures; make copies
// if you need to retain them beyond the current call.
type Tracer interface {
	CaptureState(env *EVM, pc uint64, op OpCode, gas, cost uint64, memory *Memory, stack *Stack, contract *Contract, depth int, err error) error
	CaptureEnd(output []byte, gasUsed uint64, t time.Duration, err error) error
}

// StructLogger is an EVM state logger and implements Tracer.
//
// StructLogger can capture state based on the given Log configuration and also keeps
// a track record of modified storage which is used in reporting snapshots of the
// contract their storage.
type StructLogger struct {
	cfg LogConfig

	logs          []StructLog
	changedValues map[common.Address]Storage
}

// NewStructLogger returns a new logger
func NewStructLogger(cfg *LogConfig) *StructLogger {
	logger := &StructLogger{
		changedValues: make(map[common.Address]Storage),
	}
	if cfg != nil {
		logger.cfg = *cfg
	}
	return logger
}

// CaptureState logs a new structured log message and pushes it out to the environment
//
// CaptureState also tracks SSTORE ops to track dirty values.
func (l *StructLogger) CaptureState(env *EVM, pc uint64, op OpCode, gas, cost uint64, memory *Memory, stack *Stack, contract *Contract, depth int, err error) error {
	// check if already accumulated the specified number of logs
	if l.cfg.Limit != 0 && l.cfg.Limit <= len(l.logs) {
		return ErrTraceLimitReached
	}

	// initialise new changed values storage container for this contract
	// if not present.
	if l.changedValues[contract.Address()] == nil {
		l.changedValues[contract.Address()] = make(Storage)
	}

	// capture SSTORE opcodes and determine the changed value and store
	// it in the local storage container.
	if op == SSTORE && stack.len() >= 2 {
		var (
			value   = common.BigToHash(stack.data[stack.len()-2])
			address = common.BigToHash(stack.data[stack.len()-1])
		)
		l.changedValues[contract.Address()][address] = value
	}
	// Copy a snapstot of the current memory state to a new buffer
	var mem []byte
	if !l.cfg.DisableMemory {
		mem = make([]byte, len(memory.Data()))
		copy(mem, memory.Data())
	}
	// Copy a snapshot of the current stack state to a new buffer
	var stck []*big.Int
	if !l.cfg.DisableStack {
		stck = make([]*big.Int, len(stack.Data()))
		for i, item := range stack.Data() {
			stck[i] = new(big.Int).Set(item)
		}
	}
	// Copy a snapshot of the current storage to a new container
	var storage Storage
	if !l.cfg.DisableStorage {
		storage = l.changedValues[contract.Address()].Copy()
	}
	// create a new snaptshot of the EVM.
	log := StructLog{pc, op, gas, cost, mem, memory.Len(), stck, storage, depth, err}

	l.logs = append(l.logs, log)
	return nil
}

func (l *StructLogger) CaptureEnd(output []byte, gasUsed uint64, t time.Duration, err error) error {
	fmt.Printf("0x%x", output)
	if err != nil {
		fmt.Printf(" error: %v\n", err)
	}
	return nil
}

// StructLogs returns a list of captured log entries
func (l *StructLogger) StructLogs() []StructLog {
	return l.logs
}

// WriteTrace writes a formatted trace to the given writer
func WriteTrace(writer io.Writer, logs []StructLog) {
	for _, log := range logs {
		fmt.Fprintf(writer, "%-16spc=%08d gas=%v cost=%v", log.Op, log.Pc, log.Gas, log.GasCost)
		if log.Err != nil {
			fmt.Fprintf(writer, " ERROR: %v", log.Err)
		}
		fmt.Fprintln(writer)

		if len(log.Stack) > 0 {
			fmt.Fprintln(writer, "Stack:")
			for i := len(log.Stack) - 1; i >= 0; i-- {
				fmt.Fprintf(writer, "%08d  %x\n", len(log.Stack)-i-1, math.PaddedBigBytes(log.Stack[i], 32))
			}
		}
		if len(log.Memory) > 0 {
			fmt.Fprintln(writer, "Memory:")
			fmt.Fprint(writer, hex.Dump(log.Memory))
		}
		if len(log.Storage) > 0 {
			fmt.Fprintln(writer, "Storage:")
			for h, item := range log.Storage {
				fmt.Fprintf(writer, "%x: %x\n", h, item)
			}
		}
		fmt.Fprintln(writer)
	}
}

// WriteLogs writes vm logs in a readable format to the given writer
func WriteLogs(writer io.Writer, logs []*types.Log) {
	for _, log := range logs {
		fmt.Fprintf(writer, "LOG%d: %x bn=%d txi=%x\n", len(log.Topics), log.Address, log.BlockNumber, log.TxIndex)

		for i, topic := range log.Topics {
			fmt.Fprintf(writer, "%08d  %x\n", i, topic)
		}

		fmt.Fprint(writer, hex.Dump(log.Data))
		fmt.Fprintln(writer)
	}
}