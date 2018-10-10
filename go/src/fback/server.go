package main

import (
    st "statquery"
	"fmt"
    "errors"
	"log"
	"net"
	"net/rpc"
    "math/rand"
    "sync/atomic"
    "time"
)

type Datastore [][]byte
// stats
var sentQ uint64 = 0
var hardness uint64 = 0
var widthS uint64 = 0
var concReqs int64 = 0

func (ds *Datastore) GetIds(pars st.FbackParameters, data *[]uint32) error {
    atomic.AddInt64(&concReqs, 1)
    calculate(pars.Hardness)
    rlen := pars.IdCount
//    time.Sleep(time.Millisecond*time.Duration(rand.Intn(1)))
    //	fmt.Println(ids)
    *data = make([]uint32,rlen,rlen)
    var width uint64 = 0
    for i := 0; i < rlen; i++ {
        var blocksize uint32 = 1 << uint32(pars.SizeLower+rand.Intn(pars.SizeUpper-pars.SizeLower)) 
        (*data)[i] = blocksize+4
        width += 1
    }
    atomic.AddUint64(&sentQ, 1)
    atomic.AddUint64(&hardness, uint64(pars.Hardness))
    atomic.AddUint64(&widthS, width)
    atomic.AddInt64(&concReqs, -1)
	return nil
}

func Report() {
    for {
        sentqlocal := atomic.LoadUint64(&sentQ)
        if sentqlocal > 0 {
            fmt.Println(sentqlocal,atomic.LoadUint64(&hardness),atomic.LoadUint64(&widthS)/sentqlocal,atomic.LoadInt64(&concReqs))
        }
        //reset
        atomic.StoreUint64(&sentQ,0)
        atomic.StoreUint64(&hardness,0)
        atomic.StoreUint64(&widthS,0)
        time.Sleep(time.Millisecond*1000)
    }
}

func main() {
    var USIZE int = 17

    fmt.Println("starting")
    // create some data blocks
    ds := make(Datastore,USIZE,USIZE)
    var idx uint32 = 0
	for ; idx < uint32(USIZE); idx++ {
        var blocksize uint32 = 1 << idx
        ds[idx] = make([]byte,blocksize,blocksize)
    }
    fmt.Println("created data")

    // start report
    go Report()

    // connection
    ltuple, err := net.ResolveTCPAddr("tcp", "0.0.0.0:37001")
	if err != nil {
		log.Fatal(err)
	}
	lsocket, err := net.ListenTCP("tcp", ltuple)
	if err != nil {
		log.Fatal(err)
	}
	rpc.Register(&ds)
    fmt.Println("server started")
	rpc.Accept(lsocket)
}

// matric product
func dot(x, y [][]float32) ([][]float32, error) {
	if len(x[0]) != len(y) {
		return nil, errors.New("wrong matrix format")
	}
	
	out := make([][]float32, len(x))
	for i := 0; i < len(x); i += 1 {
		for j := 0; j < len(y); j += 1 {
			if len(out[i]) < 1 {
				out[i] = make([]float32, len(y))
			}
			out[i][j] += x[i][j] * y[j][i]
		}
	}
	return out, nil
}

func calculate(hardness int) {
	X := make([][]float32,hardness,hardness)
	for i:=0; i<hardness; i++ {
		X[i] = make([]float32,hardness,hardness)
	}
	Y := make([][]float32,hardness,hardness)
	for i:=0; i<hardness; i++ {
		Y[i] = make([]float32,hardness,hardness)
	}
	dot(X, Y)
}
