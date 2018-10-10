package main

import (
	"bytes"
	"flag"
	"io/ioutil"
	"fmt"
	"bufio"
	"os"
	"math/rand"
	"time"
	"sync"
	"net/http"
    "encoding/json"
    "sync/atomic"
)
var config *string = flag.String("requestorConfig", "/config/requestor.json", "http params for requestor")

type Empty struct{}
type Semaphore chan Empty
func NewSemaphore(n int) Semaphore {
    return make(Semaphore, n)
}
func (s Semaphore) Acquire() {
    s <- Empty{}
}
func (s Semaphore) Release() {
    <- s
}

// error check alias
func Check(err error) {
    if err != nil {
        panic(err)
    }
}

var schedCount uint64 = 0
var sentCount uint64 = 0
var printCount int = 0

type requestorConfig struct {
    HttpMaxConcurrentReqs int
    HttpMaxIdleConns int
    TraceFilename string
    WarmupLambda float64
    WarmupRequests int64
    ExperimentLambda float64
    ExperimentRequests int64
}

// interarrival distribution
type Distribution interface {
       Rand() float64
}

type Exponential struct {
	lambda float64
}

type Deterministic struct {
       value float64
}

func (deterministic *Deterministic) Rand() float64 {
	return deterministic.value
}

func (exponential *Exponential) Rand() float64 {
	return rand.ExpFloat64() / exponential.lambda
}

func NextRequestTime(reqStartTime time.Time, dist Distribution) time.Time {
    return reqStartTime.Add(ToSeconds(dist.Rand()))
}

func ToSeconds(s float64) time.Duration {
	return time.Duration(float64(time.Second) * s)
}

// client pools in channel to use them in FIFO order (instead of LIFO, as defined in go's transport pool)
type ClientCon struct {
    Client *http.Client
    Transport *http.Transport
}
var clientPools chan ClientCon

func openConn(RequestorConfig requestorConfig) (ClientCon) {
    tr := &http.Transport{
        MaxIdleConns:       RequestorConfig.HttpMaxIdleConns,
        MaxIdleConnsPerHost:       RequestorConfig.HttpMaxIdleConns,
        IdleConnTimeout:    30 * time.Second,
        DisableCompression: true,
    }
    return ClientCon{&http.Client{Transport: tr}, tr}
}

func InitHttp(RequestorConfig requestorConfig) {
    // global client pool (for fifo order)
    couponmap := make(map[string]int)
    clientPools = make(chan ClientCon,RequestorConfig.HttpMaxConcurrentReqs)
    bound := RequestorConfig.HttpMaxConcurrentReqs/10
	for i := 0; i<RequestorConfig.HttpMaxConcurrentReqs; i++{
        var newcon ClientCon
        for j := 0; j<100; {
            newcon = openConn(RequestorConfig)
            resp, err := newcon.Client.Get("http://nuapp/id")
            if err == nil {
                val, err := ioutil.ReadAll(resp.Body)
                if err == nil {
                    if couponmap[string(val)] < bound {
                        // we want to save this connection
                        couponmap[string(val)]++
                        fmt.Println("ad",string(val),couponmap[string(val)])
                        resp.Body.Close()
                        break
                    } else {
                        // greater than bound (we don't a new connection)
                        newcon.Transport.CloseIdleConnections()
                        j++
                    }
                }
                resp.Body.Close()
            }
            // break goes here
            fmt.Println("retrying http connection")
            time.Sleep(time.Millisecond*20)
        }
        clientPools <- newcon
        fmt.Println("created pool",i)
    }
}

// send json request to app server
func SendRequest(s string) ([]byte, int) {
    buf := bytes.NewBuffer([]byte(s))
	if buf == nil{
		fmt.Println("error allocating buffer",buf)
        return nil, 0
	}
    // execute query
    clientPool := <- clientPools
	resp, err := clientPool.Client.Post("http://nuapp/json", "application/json", buf)
    atomic.AddUint64(&sentCount, 1)
	if err != nil{
		fmt.Println("error issueing post")
        return nil, 0
	}
	defer resp.Body.Close()
	val, err := ioutil.ReadAll(resp.Body)
	if err != nil{
		fmt.Println("error reading resp")
	}
    clientPools <- clientPool
	return val, 1
}

func LoadBalance(RequestorConfig requestorConfig) {
    // global client pool (for fifo order)
    for{
        clientPool := <- clientPools
        time.Sleep(time.Millisecond*200)
        // decommission this connection
        clientPool.Transport.CloseIdleConnections()
        // replace with new one
        var newcon ClientCon
        for {
            newcon = openConn(RequestorConfig)
            resp, err := newcon.Client.Get("http://nuapp/id")
            if err == nil {
                _, err := ioutil.ReadAll(resp.Body)
                if err == nil {
                        resp.Body.Close()
                        break
                }
                resp.Body.Close()
            }
            time.Sleep(time.Millisecond*50)
        }            
        clientPools <- newcon
    }
}


// read json trace file and encode requests
func LoadGenerator(RequestorConfig requestorConfig) {

	var wg sync.WaitGroup

	f, err := os.Open(RequestorConfig.TraceFilename)
	Check(err)
    scanner := bufio.NewScanner(f)
    
	// Main loop
	warmupDist := &Exponential{RequestorConfig.WarmupLambda}
    expDist := &Exponential{RequestorConfig.ExperimentLambda}
    dist := warmupDist

	reqStartTime := NextRequestTime(time.Now(), dist)
    printCounter := 0
    var i int64
	for i = 0; scanner.Scan() && i < RequestorConfig.WarmupRequests + RequestorConfig.ExperimentRequests; i++ {
        if i==RequestorConfig.WarmupRequests {
            dist = expDist
        }
		time.Sleep(time.Until(reqStartTime))
        for schedCount - sentCount > uint64(RequestorConfig.HttpMaxConcurrentReqs)*1000 {
            printCount++
            if printCount>100 {
                fmt.Println("Closed loop (100x)",time.Now())
                printCount = 0
            }
            time.Sleep(time.Millisecond*10)
        }

		// issue request
		wg.Add(1)
        atomic.AddUint64(&schedCount, 1)
		go func(reqStartTime time.Time, body string) {
			if len(body) > 0{
			SendRequest(body)
            }
			wg.Done()
		}(reqStartTime, scanner.Text())
		reqStartTime = NextRequestTime(reqStartTime, dist)
        if printCounter>100000 {
            printCounter = 0
            fmt.Println("Status",schedCount, sentCount)
        }
        printCounter++
	}

	// Wait for completion
    fmt.Println("done sending", schedCount, sentCount)
	wg.Wait()
    fmt.Println("done waiting", schedCount, sentCount)
}

func main() {
    flag.Parse()
    f, err := os.Open(*config)
    Check(err)
    r := bufio.NewReader(f)
    dec := json.NewDecoder(r)
    var RequestorConfig requestorConfig
    err = dec.Decode(&RequestorConfig)
    Check(err)
	if ((RequestorConfig.ExperimentRequests + RequestorConfig.WarmupRequests <= 0) || (RequestorConfig.ExperimentLambda <= 0)) {
        fmt.Println(RequestorConfig)
		flag.Usage()
		return
	}
	InitHttp(RequestorConfig)
    go LoadBalance(RequestorConfig)
	LoadGenerator(RequestorConfig)
    fmt.Println("done")
}
