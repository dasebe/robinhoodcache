package main

import (
	st "statquery"
    "io"
    "io/ioutil"
	"net/http"
    "net"
	"flag"
	"fmt"
	"time"
	"log"
	"encoding/json"
	"sort"
	"strconv"
	"sync"
    "container/heap"
    "encoding/csv"
    "os"
)

var statServerPort *int = flag.Int("statServerPort", 80, "statServer port")
var verbose *bool = flag.Bool("verbose", false, "Enable detailed logging")


// error check alias
func Check(err error) {
    if err != nil {
        panic(err)
    }
}


// latency stats
var statStream chan st.Results
var csvStream chan st.Results
var latencyRing [10000000]st.Latency
var requestRate float64
var queryRate float64
var totalReqs map[string]int64
var totalReqsMtx = &sync.Mutex{}
var ringIdx int = 0
var heapWarmup uint64 = 0
var latencyHeapMtx = &sync.Mutex{}
var latencyHeap = &st.LatHeap{}
var total float64 = 0
var startTime time.Time

func PutStatsHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
    var res st.Results
	err := decoder.Decode(&res)
	Check(err)
    statStream <- res
    if *verbose {
        csvStream <- res
    }
    encoder := json.NewEncoder(w)
    copy := CopyMemStat()
    err = encoder.Encode(copy)
    Check(err)
}

func StoreStats() {
    lastT := time.Now()
    lastD := time.Since(lastT)
    var queryCount float64 = 0
    var requestCount float64 = 0
    for res := range statStream {
        lastD = time.Since(lastT)
        if lastD > 10*time.Second {
            requestRate = requestCount / lastD.Seconds()
            requestCount = 0
            queryRate = queryCount / lastD.Seconds()
            queryCount = 0
            lastT = time.Now()
        }
        for _, lat := range res.Data {
            latencyRing[ringIdx] = lat
            if ringIdx < len(latencyRing) -1 {
                ringIdx += 1
            } else {
                ringIdx = 0
            }
            if lat.Tp == "req" && heapWarmup <= 10000000{
                heapWarmup++
            }
            if lat.Tp == "req" {
                requestCount += 1
            }
            if heapWarmup > 5000000 && lat.Tp == "req" {
                latencyHeapMtx.Lock()
                total += 1
                if latencyHeap.Len() < 2000000 {
                    heap.Push(latencyHeap, lat.Lat)
                } else if lat.Lat > (*latencyHeap)[0] {
                    heap.Pop(latencyHeap)
                    heap.Push(latencyHeap, lat.Lat)
                }
                latencyHeapMtx.Unlock()
            }
            if lat.Tp != "req" {
                queryCount += 1
            }
            totalReqsMtx.Lock()
            totalReqs[lat.Tp] += 1
            totalReqsMtx.Unlock()
        }
    }
}

// output all measurements into a file
func CsvStats() {
    fileName := "/logs/" + os.Getenv("CONFIG") + "_" + time.Now().Format("2006-01-02_15:04:05")
    file, err := os.Create(fileName )
    Check(err)
    defer file.Close()
    writer := csv.NewWriter(file)
    writer.Comma = '\t'
    counter := 10000
    csvLine := make([]string,5)
    for res := range csvStream {
        // get timestamp in seconds since start
        curTime := int(time.Since(startTime).Seconds())
        for _, lat := range res.Data {
            csvLine[0] = strconv.Itoa(curTime)
            csvLine[1] = lat.Tp
            csvLine[2] = strconv.Itoa(int(lat.St))
            csvLine[3] = strconv.FormatInt(lat.Lat,10)
            csvLine[4] = lat.Cp
            err := writer.Write(csvLine)
            Check(err)
            counter--
            // flush every 10000 latencies
            if counter<1 {
                counter = 10000
                writer.Flush()
            }
        }
    }
}

// percentile calculation
type LatSlice []int64
func (a LatSlice) Len() int           { return len(a) }
func (a LatSlice) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a LatSlice) Less(i, j int) bool { return a[i] < a[j] }
func SortedPercentile(vals LatSlice, perctile float64) int64{
    pidx := perctile * float64(len(vals))
    var perc int64
    // empty check
    if len(vals)>1 {
     	i := int(pidx)
        if i<len(vals) {
            perc = vals[i]
        } else {
            perc = vals[0]
        }
    } else {
        perc = -1
    }
    return perc
}

type LatEntry struct {
    Latency int64
    Name string
}
type LatTail []LatEntry
func (a LatTail) Len() int           { return len(a) }
func (a LatTail) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a LatTail) Less(i, j int) bool { return a[i].Latency < a[j].Latency }
func CountCritPath(vals LatTail, startPerctile float64, endPerctile float64) map[string]int64 {
    startIdx := int(startPerctile * float64(len(vals)))
    endIdx := int(endPerctile * float64(len(vals)))
    cp := make(map[string]int64)
    // empty check
    if len(vals)>1 {
        for j := startIdx; j < endIdx; j++ {
            cp[vals[j].Name]++
        }
    } 
   return cp
}


var statustypes = []int8{-1, 0, 1, 2}
var lastStats map[string]interface{}
func GetStatsHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
    io.Copy(ioutil.Discard, r.Body)
    encoder := json.NewEncoder(w)
    encoder.SetIndent("", " ")
    err := encoder.Encode(lastStats)
    Check(err)
}


func RecomputeStats(sleepPeriod time.Duration) {
    for {
        // get timestamp in seconds since start
        curTime := int(time.Since(startTime).Seconds())
        // store results here
        res := make(map[string]map[int8]LatSlice)
        // calculate crit path
        lt := make(LatTail, 0, len(latencyRing)) // for P99 crit path
        critsOverall := make(map[string]int64)
        for _, lat := range latencyRing {
            if res[lat.Tp] == nil {
                res[lat.Tp] = make(map[int8]LatSlice)
            }
            res[lat.Tp][lat.St] = append(res[lat.Tp][lat.St], lat.Lat)
            res[lat.Tp][2] = append(res[lat.Tp][2], lat.Lat)
            if lat.Tp == "req" {
                critsOverall[lat.Cp] += 1
                lt = append(lt, LatEntry{Latency: lat.Lat, Name: lat.Cp})
            }
        }
        // sort in place
        sort.Sort(lt)
        crits := CountCritPath(lt, 0.99,1.0)
        crits2 := CountCritPath(lt, 0.985,0.995)
        crits3 := CountCritPath(lt, 0.99,0.995)
        copiedMemStat := CopyMemStat()
        // gsr is the object being sent back
        // calculate percentiles and create json
        gsr := make(map[string]interface{})
        for tp, vals := range res {
            gsrtmp := make(map[string]interface{})
            for _, st := range statustypes {
                lats, ok := vals[st]
                if !ok {
                    lats = []int64{}
                }
                perctmp := make(map[string]interface{})
                // sort in place
                sort.Sort(lats)
                perctmp["count"] = int64(len(lats))
                perctmp["p99.99"] = SortedPercentile(lats,0.9999)
                perctmp["p99.9"] = SortedPercentile(lats,0.999)
                perctmp["p99.5"] = SortedPercentile(lats,0.995)
                perctmp["p99"] = SortedPercentile(lats,0.99)
                perctmp["p98.5"] = SortedPercentile(lats,0.985)
                perctmp["p95"] = SortedPercentile(lats,0.95)
                perctmp["p90"] = SortedPercentile(lats,0.90)
                perctmp["p75"] = SortedPercentile(lats,0.75)
                perctmp["p50"] = SortedPercentile(lats,0.50)
                perctmp["p25"] = SortedPercentile(lats,0.25)
                if st == 2 {
                    if _, ok := copiedMemStat[tp]; ok {
                        perctmp["memlimit"] = copiedMemStat[tp].Limits
                        perctmp["malloced"] = copiedMemStat[tp].Mallocs
                    } else {
                        perctmp["memlimit"] = []int64{-1}
                        perctmp["malloced"] = []int64{-1}
                    }
                    var hr_delta float64 = 0
                    HitStatsMtx.Lock()
                    appservercount := 0
                    for _, hitstat := range HitStats {
                        hr_delta += hitstat[tp]
                        appservercount++
                    }
                    HitStatsMtx.Unlock()
                    perctmp["hr_delta"] = hr_delta
                    totalReqsMtx.Lock()
                    perctmp["totalreqs"] = totalReqs[tp]
                    totalReqsMtx.Unlock()
                }
                gsrtmp[strconv.Itoa(int(st))] = perctmp
            }
            gsrtmp["crit_count_overall"] = critsOverall[tp]
            gsrtmp["crit_count"] = crits[tp]
            gsrtmp["crit_count2"] = crits2[tp]
            gsrtmp["crit_count3"] = crits3[tp]
            gsr[tp] = gsrtmp
        }
        gsr["request rate"] = requestRate
        gsr["query rate"] = queryRate
        gsr["config"] = os.Getenv("CONFIG")
        gsr["seconds"] = curTime
        lastStats = gsr
        time.Sleep(sleepPeriod)
    }
}



func GetTailHandler(w http.ResponseWriter, r *http.Request) {
    fmt.Println("gettail")
	defer r.Body.Close()
    io.Copy(ioutil.Discard, r.Body)
    // get timestamp in seconds since start
    curTime := int(time.Since(startTime).Seconds())
    var p9999number int = 0
    var p999number int = 0
    var p99number int = 0
    var p95number int = 0
    var p9999 int64 = 0
    var p999 int64 = 0
    var p99 int64 = 0
    var p95 int64 = 0
    var curTotal float64 = 0
    latencyHeapMtx.Lock()
    fmt.Println("gettail acquired")
    var heapOverFlow int = 0
    if latencyHeap.Len() > 10 {
        curTotal = total
        p9999number = int(.0001 * curTotal)
        p999number = int(.001 * curTotal)
        p99number = int(.01 * curTotal)
        p95number = int(.05 * curTotal)
        sort.Sort(latencyHeap)
        if p9999number >= latencyHeap.Len() {
            p9999 = (*latencyHeap)[0]
            heapOverFlow = 1
        } else{
            p9999 = (*latencyHeap)[latencyHeap.Len()-p9999number]
        }
        if p999number >= latencyHeap.Len() {
            p999 = (*latencyHeap)[0]
            heapOverFlow = 1
        } else{
            p999 = (*latencyHeap)[latencyHeap.Len()-p999number]
        }
        if p99number >= latencyHeap.Len() {
            p99 = (*latencyHeap)[0]
            heapOverFlow = 1
        } else{
            p99 = (*latencyHeap)[latencyHeap.Len()-p99number]
        }
        if p95number >= latencyHeap.Len() {
            p95 = (*latencyHeap)[0]
            heapOverFlow = 1
        } else{
            p95 = (*latencyHeap)[latencyHeap.Len()-p95number]
        }
    }
    latencyHeapMtx.Unlock()
    gsr := st.GetStatResult{"p99.99": p9999, "p99.9": p999, "p99": p99, "p95": p95, "p99.99number": p9999number, "p99.9number": p999number, "p99number": p99number, "p95number": p95number, "total": curTotal, "HeapOverFlow" : heapOverFlow, "config" : os.Getenv("CONFIG"), "seconds" : curTime}
    encoder := json.NewEncoder(w)
    err := encoder.Encode(gsr)
    Check(err)
}

// cpu utilization etc
type Result struct {
    Mem float64 `json:"mem_pct"`
    CPU float64 `json:"total_cpu_pct"`
    NetRx float64 `json:"rx_bytes"`
    NetRd float64 `json:"rx_dropped"`
    NetTx float64 `json:"tx_bytes"`
    NetTd float64 `json:"tx_dropped"`
    IoRead float64 `json:"iops_read"`
    IoWrite float64 `json:"iops_write"`
}
type UtilResultSet map[string]interface{}
var UtilResults UtilResultSet
var UtilMtx = &sync.Mutex{}
func PutUtilsHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
    decoder := json.NewDecoder(r.Body)
    TmpUtilRes := make(UtilResultSet)
    err := decoder.Decode(&TmpUtilRes)
    Check(err)
    ip, _, err := net.SplitHostPort(r.RemoteAddr)
    Check(err)
    UtilMtx.Lock()
    for name, stats := range TmpUtilRes {
    	UtilResults[ip+"--"+name] = stats
    }
    UtilMtx.Unlock()
    fmt.Fprintf(w, "ok")
}

func GetUtilsHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
    io.Copy(ioutil.Discard, r.Body)
    encoder := json.NewEncoder(w)
    encoder.SetIndent("", " ")
    // get timestamp in seconds since start
    curTime := int(time.Since(startTime).Seconds())
    // get and encode results
    UtilMtx.Lock()
    UtilResults["seconds"] = curTime
    err := encoder.Encode(UtilResults)
    UtilMtx.Unlock()
    Check(err)
}

// memory allocation statistics
var CurMemLimits st.PerControllerMemLimits
var CurMemLimitOrder []string
var CurMemMtx = &sync.Mutex{}
func PutMemHandler(w http.ResponseWriter, r *http.Request) {
    defer r.Body.Close()
    decoder := json.NewDecoder(r.Body)
    TmpCurMemLimits := make(st.MemLimits)
    err := decoder.Decode(&TmpCurMemLimits)
    ip, _, err := net.SplitHostPort(r.RemoteAddr)
    Check(err)
    CurMemMtx.Lock()
    // check if we've seen this controller ip before
    _, ok := CurMemLimits[ip]
    if !ok {
        CurMemLimits[ip] = make(st.MemLimits)
        CurMemLimitOrder = append(CurMemLimitOrder,ip)
    }
    // iterate over backends and update if not null entry
    for dn, memlim := range TmpCurMemLimits {
    	if len(memlim.Limits)>0 && len(memlim.Mallocs)>0 {
            CurMemLimits[ip][dn] = memlim
        }
    }
    CurMemMtx.Unlock()
    Check(err)
    fmt.Fprintf(w, "ok")
}

func GetMemHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
    io.Copy(ioutil.Discard, r.Body)
    encoder := json.NewEncoder(w)
    copy := CopyMemStat()
    err := encoder.Encode(copy)
    Check(err)
}

func CopyMemStat() (st.MemLimits) {
    tmp := make(map[string]*st.MemLimit)
    CurMemMtx.Lock()
    for _, ip := range CurMemLimitOrder {
        for dn, memlim := range CurMemLimits[ip] {
            _, ok := tmp[dn]
            if !ok {
                tmp[dn] = &st.MemLimit{memlim.Limits, memlim.Mallocs}
            } else {
                tmp[dn].Limits = append(tmp[dn].Limits, memlim.Limits...)
                tmp[dn].Mallocs = append(tmp[dn].Mallocs, memlim.Mallocs...)
            }
        }
    }
    CurMemMtx.Unlock()
    tmp2 := make(st.MemLimits)
    for dn, memlim := range tmp {
        tmp2[dn] = *memlim
    }
    return tmp2
}

// hit ratio statistics
var HitStats map[string]st.HitStatistic
var HitStatsMtx = &sync.Mutex{}
var HitStatStream chan st.HitStatEntry
func PutHitRatiosHandler(w http.ResponseWriter, r *http.Request) {
    defer r.Body.Close()
    decoder := json.NewDecoder(r.Body)
    hs := make(st.HitStatistic)
    err := decoder.Decode(&hs)
    Check(err)
    ip, _, err := net.SplitHostPort(r.RemoteAddr)
    Check(err)
    HitStatStream <- st.HitStatEntry{RAddr: ip, HitStat: hs}
    fmt.Fprintf(w, "ok")
}


func StoreHitRatios() {
    for hs := range HitStatStream {
        HitStatsMtx.Lock()
        _, ok := (HitStats[hs.RAddr])
        if !ok {
            // haven't seen this ip before
            HitStats[hs.RAddr] = make(st.HitStatistic)
        }
        for tt, delta := range (hs.HitStat) {
            _, ok = (HitStats[hs.RAddr])[tt]
            if !ok {
                // haven't seen this type before
                (HitStats[hs.RAddr])[tt] = delta
            } else {
                (HitStats[hs.RAddr])[tt] = (HitStats[hs.RAddr])[tt] * .8 + delta * .2
            }
        }
        HitStatsMtx.Unlock()
    }
}

// initialize channels
func main() {
	flag.Parse()

	// init
	statStream = make(chan st.Results, 100)
    totalReqs = make (map[string]int64)
    heap.Init(latencyHeap)
    go StoreStats()
    if *verbose {
        csvStream = make(chan st.Results, 1000)
        go CsvStats()
    }
    go RecomputeStats(3 * time.Second)
    HitStatStream = make(chan st.HitStatEntry, 100)
	HitStats = make(map[string]st.HitStatistic)
    go StoreHitRatios()
    UtilResults = make(UtilResultSet)
    CurMemLimits = make(st.PerControllerMemLimits)
    startTime = time.Now()

    // register http api
	http.HandleFunc("/putstats", PutStatsHandler)
    http.HandleFunc("/getstats", GetStatsHandler)
    http.HandleFunc("/gettail", GetTailHandler)
	http.HandleFunc("/pututils", PutUtilsHandler)
    http.HandleFunc("/getutils", GetUtilsHandler)
	http.HandleFunc("/putmalloc", PutMemHandler)
    http.HandleFunc("/getmalloc", GetMemHandler)
    http.HandleFunc("/puthrs", PutHitRatiosHandler)

    // start http server
	log.Fatal(
		http.ListenAndServe(
			fmt.Sprintf(":%d", *statServerPort),
			nil,
		),
	)
}
