package poc

import (
	"github.com/xdn/go-xdn/shabal"
	"encoding/binary"
	"io/ioutil"
	"fmt"
	"github.com/xdn/go-xdn/common"
	"math/big"
)

const (
	HASH_SIZE = 32
	CELL_SIZE = 8192
	PLOT_SIZE = 4096
)

func GenCell(nonce uint64, pub uint64) []byte {
	var cellBytes []byte
	nonceBytes := make([]byte, 8)
	pubBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(nonceBytes, nonce)
	binary.BigEndian.PutUint64(pubBytes, pub)

	comBytes := append(pubBytes, nonceBytes...)

	hasher := shabal.NewSha_bal256()

	for i := CELL_SIZE - 1; i >= 0; i-- {
		if i == CELL_SIZE - 1 {
			com := comBytes
			hasher.Write(com)
			cell := hasher.Sum(nil)
			cellBytes = cell
		} else {
			var com []byte
			if len(cellBytes) >= PLOT_SIZE {
				com = cellBytes[0: PLOT_SIZE]
			} else {
				com = append(cellBytes, comBytes...)
			}
			hasher.Reset()
			hasher.Write(com)
			cell := hasher.Sum(nil)
			cellBytes = append(cell, cellBytes...)
		}
	}

	com := append(cellBytes, comBytes...)
	hasher.Reset()
	hasher.Write(com)
	final := hasher.Sum(nil)

	for i := CELL_SIZE - 1; i >= 0; i-- {
		for j := 0; j < HASH_SIZE; j++ {
			cellBytes[i * HASH_SIZE + j] = cellBytes[i * HASH_SIZE + j] ^ final[j]
		}
	}

	cellBytes = rearrange(cellBytes)

	//fmt.Printf("cellBytes:%v\r\n", cellBytes[0: 300])

	return cellBytes
}

func GenCellForP(nonce uint64, pub uint64) []byte {
	//var cellBytes []byte
	cellBytes := make([]byte, CELL_SIZE * HASH_SIZE)
	nonceBytes := make([]byte, 8)
	pubBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(nonceBytes, nonce)
	binary.BigEndian.PutUint64(pubBytes, pub)

	comBytes := append(pubBytes, nonceBytes...)

	hasher := shabal.NewSha_bal256()

	for i := CELL_SIZE - 1; i >= 0; i-- {
		if i == CELL_SIZE - 1 {
			com := comBytes
			hasher.Write(com)
			cell := hasher.Sum(nil)
			for j := 0; j < HASH_SIZE; j++ {
				cellBytes[i * HASH_SIZE + j] = cell[j]
			}
		} else {
			var com []byte
			//if len(cellBytes) >= PLOT_SIZE {
			if i < CELL_SIZE - 128 {
				//com = cellBytes[0: PLOT_SIZE]
				com = cellBytes[(i + 1) * HASH_SIZE : (i + 1) * HASH_SIZE + 128 * HASH_SIZE]
			} else {
				com = append(cellBytes[(i + 1) * HASH_SIZE : CELL_SIZE * HASH_SIZE], comBytes...)
			}
			hasher.Reset()
			hasher.Write(com)
			cell := hasher.Sum(nil)
			//cellBytes = append(cell, cellBytes...)
			for j := 0; j < HASH_SIZE; j++ {
				cellBytes[i * HASH_SIZE + j] = cell[j]
			}
		}
	}

	com := append(cellBytes, comBytes...)
	hasher.Reset()
	hasher.Write(com)
	final := hasher.Sum(nil)

	for i := CELL_SIZE - 1; i >= 0; i-- {
		for j := 0; j < HASH_SIZE; j++ {
			cellBytes[i * HASH_SIZE + j] = cellBytes[i * HASH_SIZE + j] ^ final[j]
		}
	}

	return cellBytes
}

func GetCellFromPlot(dir string, nonce uint64, pub uint64) []byte {
	file := fmt.Sprintf("%v/%v_%v_%v", dir, pub, nonce, 72)
	data, err := ioutil.ReadFile(file)
	if err != nil {
		return nil
	} else {

		//fmt.Printf("cellFiles:%v\r\n", data[0: 300])

		return data
	}
}

func rearrange(src []byte) []byte {
	res := make([]byte, 0)
	for i := 0; i < CELL_SIZE; i++ {
		if i % 2 == 0 {
			res = append(res, src[HASH_SIZE * i: HASH_SIZE * (i + 1)]...)
		} else {
			res = append(res, src[HASH_SIZE * (CELL_SIZE - i): HASH_SIZE * (CELL_SIZE - i + 1)]...)
		}
	}
	return res
}

func GenSignature(lastGenSig common.Hash, lastPubID uint64) common.Hash {
	sigBytes := lastGenSig.Bytes()
	pubBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(pubBytes, lastPubID)
	comBytes := append(sigBytes, pubBytes...)
	hasher := shabal.NewSha_bal256()
	hasher.Write(comBytes)
	res := hasher.Sum(nil)
	return common.BytesToHash(res)
}

func GenHash(genSig common.Hash, number uint64) common.Hash {
	sigBytes := genSig.Bytes()
	numBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(numBytes, number)
	comBytes := append(sigBytes, numBytes...)
	hasher := shabal.NewSha_bal256()
	hasher.Write(comBytes)
	res := hasher.Sum(nil)
	return common.BytesToHash(res)
} 

func GetScoopID(genHash common.Hash) int {
	var ret int
	ret = int(genHash[30] << 8) | int(genHash[31])
	return ret % 4096
}

func CalcTarget(scoop_1 []byte, scoop_2 []byte, genSig common.Hash) common.Hash {
	comBytes := append(scoop_1, scoop_2...)
	comBytes = append(comBytes, genSig.Bytes()...)
	hasher := shabal.NewSha_bal256()
	hasher.Write(comBytes)
	res := hasher.Sum(nil)
	return common.BytesToHash(res)
}

func CalcDeadLine(target *big.Int, baseTarget *big.Int) *big.Int {
	return new(big.Int).Div(target, baseTarget)
}

func CalcPlotID(addr common.Address) uint64 {
	tmp := (addr.Bytes())[15:]
	return uint64(binary.BigEndian.Uint64(tmp))
}