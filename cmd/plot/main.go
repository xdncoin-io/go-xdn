package main

import (
	"fmt"
	"os"
	"strconv"
	"runtime"
	//"io/ioutil"
	"github.com/xdn/go-xdn/alecthomas/kingpin.v2"
	"github.com/xdn/go-xdn/alecthomas/units"
	"github.com/xdn/go-xdn/poc"
	"github.com/xdn/go-xdn/common"
	"encoding/json"
)

var (
	app = kingpin.New("dplot", "tool for wirte data for plot id")

	write = app.Command("write", "wirte plot data for plot id")
	dataPath = write.Flag("dataPath", "set the data path").Required().String()
	singSize = write.Flag("singSize", "set the singSize for sing file as MB GB TB").Required().String()
	size = write.Flag("size", "set the total size for plot as MB GB TB").Required().String()
	startNonce = write.Flag("startNonce", "set the start none").Default("314159").String()
	plotID = write.Flag("plotID", "set the plot ID").Required().String()

	calc = app.Command("calc", "get plotID for given address")
	addr = calc.Flag("addr", "given an address").Required().String()
)

type Param struct {
	DataPath string
	SingSize int64
	SingCount int64
	Count int64
	StartNonce uint64
	PlotID uint64
}

type Result struct {
	Code int `json:"code"`
	Msg string `json:"msg"`
	Current int64 `json:"current"`
	Total int64 `json:"total"`
	Name string `json:"name"`
}

func makeResult(err error, i int64, count int64, name string) string {
	res := &Result{
		Code: 0,
		Msg: "ok",
		Current: i,
		Total: count,
		Name: name,
	}
	if err != nil {
		res.Code = -1
		res.Msg = err.Error()
	}
	ret, _ := json.Marshal(res)
	return string(ret)
}

func parseParam() (*Param, error) {
	param := &Param{
		DataPath: *dataPath,
		SingCount: 0,
		Count: 0,
		StartNonce: uint64(0),
		PlotID: uint64(0),
	}
	cellSize, err := units.ParseBase2Bytes("256KB")
	if err != nil {
		return nil, err
	}
	sing, err := units.ParseBase2Bytes(*singSize)
	if err != nil {
		return nil, err
	}
	total, err := units.ParseBase2Bytes(*size)
	if err != nil {
		return nil, err
	}

	param.SingCount = int64(sing) / int64(cellSize)
	param.Count = int64(total) / int64(sing)
	param.SingSize = param.SingCount * int64(cellSize)

	param.StartNonce, err = strconv.ParseUint(*startNonce, 10, 64)
	if err != nil {
		return nil, err
	}
	param.PlotID, err = strconv.ParseUint(*plotID, 10, 64)
	if err != nil {
		return nil, err
	}

	return param, nil
}

func createFile(path string, name string, size int64) error {
	f, err := os.Create(path + "/" + name)
	if err != nil {
		return err
	}
	defer f.Close()

	battleSize, err := units.ParseBase2Bytes("20MB")
	if err != nil {
		return err
	}
	battle := size / int64(battleSize)
	b := make([]byte, int(battleSize))
	for i := int64(0); i < battle; i++ {
		_, err = f.Write(b)
		if err != nil {
			return err
		}
	}
	dual := size % int64(battleSize)
	if dual > int64(0) {
		b = make([]byte, dual)
		_, err = f.Write(b)
		if err != nil {
			return err
		}
	}
	return nil
}

func makeName(plotID uint64, nonce uint64, singCount int64) string {
	return fmt.Sprintf("%v_%v_%v", plotID, nonce, singCount)
}

type WaitWrite struct {
	Data []byte
	Index int64
}

type ChanWrite struct {
	F *os.File
	W []*WaitWrite
	Close bool
}

func loop(param *Param, ch chan<- int) error {
	//currentNonce := param.StartNonce
	threadCount := 2 * runtime.NumCPU() // 设置默认线程数10个
	writeCount := 100

	chp := make(chan int, threadCount)
	chw := make(chan *ChanWrite, writeCount)

	for m := 0; m < threadCount; m++ {
		go func(ki int) error {
			for i := int64(0); i < param.Count; i++ {
				if(int(i) % threadCount == ki) {
					//fmt.Printf("haha ki=%v, i=%v, threadCount=%v\r\n", ki, i, threadCount)
					currentNonce := param.StartNonce + uint64(i) * uint64(param.SingCount)

					name := makeName(param.PlotID, currentNonce, param.SingCount)
					err := createFile(param.DataPath, name, param.SingSize)
					if err != nil {
						fmt.Printf("%v\r\n", makeResult(err, i, param.Count, name))
						return err
					}
					f, err := os.OpenFile(param.DataPath + "/" + name, os.O_RDWR, 0666)
					if err != nil {
						return err
					}
					onceHandle := 400    // 一次性写入400个Nonce的数据
					for j := int64(0); j < param.SingCount / int64(onceHandle) + 1; j++ {
						jf := j
						once := onceHandle
						closed := false
						if j >= param.SingCount / int64(onceHandle) {
							once = int(param.SingCount) % onceHandle
							closed = true
						}
						if once == 0 {
							break
						}

						cells := make([][]byte, once)
						for p := 0 ; p < once; p++ {
							cs := poc.GenCellForP(currentNonce, param.PlotID)
							cells[p] = cs
							currentNonce++
						}

						waits := make([]*WaitWrite, 4096)

						//fmt.Printf("jf=%v,once=%v,onceHandle=%v,currentNonce=%v\r\n", jf, once, onceHandle, currentNonce)

						for k := 0; k < 4096; k++ {
							data := make([]byte, 64 * once)
							index := int64(64) * param.SingCount * int64(k) + int64(64) * jf * int64(onceHandle)

							for q := 0; q < once; q++ {
								for r := 0; r < 32; r++ {
									data[q * 64 + r] = cells[q][32 * (2 * k) + r]
									data[q * 64 + 32 + r] = cells[q][(8192 - (2 * k + 1)) * 32 + r]
								}
							}

							waits[k] = &WaitWrite{
								Data: data,
								Index: index,
							}

							// chw <- &ChanWrite{
							// 	F: f,
							// 	Data: data,
							// 	Index: index,
							// 	Close: closed,
							// }
						}
						chw <- &ChanWrite{
							F: f,
							W: waits,
							Close: closed,
						}
					}

					fmt.Printf("%v\r\n", makeResult(err, i, param.Count, name))
				}
			}
			chp <- 1
			return nil
		}(m)
	}

	complete := 0
	for {
		select {
		case <-chp:
			complete++
			if(complete == threadCount) {
				goto WAITFOR
			}
			//break
		case w := <-chw:
			for i := 0; i < len(w.W); i++ {
				w.F.WriteAt(w.W[i].Data, w.W[i].Index)
			}
			if w.Close {
				w.F.Close()
			}
		}
	}
WAITFOR:
	return nil
}

func main() {
	kingpin.Version("1.0.0")
	switch kingpin.MustParse(app.Parse(os.Args[1:])) {
	case write.FullCommand():
		param, err := parseParam()
		if err != nil {
			fmt.Printf("err:%v\r\n", err)
			return
		}
		ch := make(chan int)
		loop(param, ch)

	case calc.FullCommand():
		address := common.HexToAddress(*addr)
		plotID := poc.CalcPlotID(address)
		fmt.Printf("plotID: %v\r\n", plotID)
	}
}