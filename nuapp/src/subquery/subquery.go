package subquery

import (
    sq "shadowcache"
    st "statquery"
    "math/rand"
    "database/sql"
    _ "github.com/go-sql-driver/mysql"
    "github.com/bradfitz/gomemcache/memcache"
    //    "net"
    "fmt"
    "time"
    "io/ioutil"
    "encoding/json"
    "os"
    "net/http"
	"net/rpc"
    "bytes"
    "bufio"
    "flag"
    "math"	
    "strconv"
    "strings"
    "errors"
    "sync/atomic"
)
var cacheConfigPath *string = flag.String("cacheConfig", "/config/cache.json", "config file name")
var appConfigPath *string = flag.String("appConfig", "/config/appserver.json", "config file name")
// bypass caches
var BypassCaches bool

// semaphore alias
type Empty struct{}
type Semaphore chan Empty
func NewSemaphore(n int) Semaphore {
    return make(Semaphore, n)
}
func (s Semaphore) Acquire() bool {
    select {
    case s <- Empty{}:
        return true
    case <-time.After(10 * time.Second):
        return false
    }
}
func (s Semaphore) Release() {
    <- s
}

// subsystems
var Subs map[string]Subsystem

type appConfig struct {
    Cache struct {
        MaxConcurrentReqs int
    }
    Fb struct {
        MaxConcurrentReqs int
    }
    Db struct {
        User, Name, Passwd string
        MaxOpenConns, MaxIdleConns int
    }
}
// global config store
var DepConfigs []st.DepConfig


// subquery dep layer
type DepQuery struct {
    S []int64
    C []int8
    U []string
}
type JsonRequest struct {
    T int64 `json:"t"`
    D QueryLayerSlice `json:"d"`
}
type QueryLayer map[string]DepQuery
type QueryLayerSlice []QueryLayer

// subsystem types: caches and backend connections
type Subsystem struct {
    CacheClient *memcache.Client
    CacheSema Semaphore
    BackClient BackConn
    BackSema Semaphore
    Shadow1, Shadow2 *sq.ShadowCache
}

type BackConn interface {
    Request([]string) (map[string][]byte, error)
    Ping() bool
}

type SizeGen struct{
    R * rand.Rand
    Cutoffs map[float32]int64
    Ordered []float32
}

func NewSizeGen (buckets map[string]float32) (sg SizeGen){
    sg.R = rand.New(rand.NewSource(time.Now().UTC().UnixNano()))
    sg.Cutoffs = make(map[float32]int64)
    var cutoff float32
    for k, v := range buckets{
        cutoff += v
        sg.Cutoffs[cutoff], _ = strconv.ParseInt(k, 10, 64)
        sg.Ordered = append(sg.Ordered, cutoff)
    }
    return sg
}

func (sg SizeGen) Size () (size int64){
    val := sg.R.Float32()
    for _, k := range sg.Ordered{
        if val < k {
            return sg.Cutoffs[k]
        }
    }
    return sg.Cutoffs[sg.Ordered[len(sg.Ordered) - 1]]
}

// fb-type backend connection
type FbBackConn struct {
    Conn chan *rpc.Client
    Pars st.FbackParameters
}
func (fbcon FbBackConn) Request(ids []string) (map[string][]byte, error){
    var err error
    res := make(map[string][]byte)
    // check empty ids
    if len(ids) < 1 {
        fmt.Println("empty mysql ids",len(ids))
        err = errors.New("empty ids")
        return res, err
    }
    var reply []uint32
    pars := fbcon.Pars
    pars.IdCount = len(ids)
    rpcc := <- fbcon.Conn
    err = rpcc.Call("Datastore.GetIds", pars, &reply)
    fbcon.Conn <- rpcc
    if err == nil {
        if len(ids) == len(reply) {
            for i, id := range ids {
                res[id] = make([]byte,reply[i],reply[i])
            }
        } else {
            err = errors.New("incorrect reply count")
        }
    }
    return res, err
}

func (fbcon FbBackConn) Ping() (works bool){
    for i:=0; i<100; i++ {
        pars := fbcon.Pars
        pars.IdCount = 10
        var reply []uint32
        rpcc := <- fbcon.Conn
        err := rpcc.Call("Datastore.GetIds", pars, &reply)
        fbcon.Conn <- rpcc
        if err != nil {
            return false
        }
    }
    return true
}


type MySqlBackConn struct {
    Conn * sql.DB
    R * rand.Rand
    WriteProb float32
}

func (mybc MySqlBackConn) Request(ids []string) (map[string][]byte, error){
    res := make(map[string][]byte)
    // check empty ids
    if len(ids) < 1 {
        fmt.Println("empty mysql ids",len(ids))
        err := errors.New("empty ids")
        return res, err
    }
    // execute multi-id select
    var query string
    if len(ids) > 1 {
        query = "SELECT oid, value FROM blobs WHERE oid in (?" + strings.Repeat(",?", len(ids)-1) + ") LIMIT "+strconv.Itoa(len(ids))
    } else {
        query = "SELECT oid, value FROM blobs WHERE oid=? LIMIT 1"
    }
    // go's Query function requires a []interface{} and there is no explicit conversion from []string (except to copy every element)
    idsConverted := make([]interface{}, len(ids))
    for i, id := range ids {
        idsConverted[i] = id
    }
    rows, err := mybc.Conn.Query(query, idsConverted...)
    if err != nil {
        return res, err
    }
    defer rows.Close()
    for rows.Next() {
        var oid string
        var val []byte
        err = rows.Scan(&oid, &val)
        if err != nil {
            return res, err
        }
        if len(val)>0 {
            res[oid] = val
            if mybc.R.Float32() < mybc.WriteProb{
                mybc.Conn.Exec("UPDATE blobs SET value=? WHERE oid=?;", val, oid)
            }
        } else {
            fmt.Println("empty backend result",oid,val)
        }
    }
    return res, err
}

func (mybc MySqlBackConn) Ping() (works bool){
    err := mybc.Conn.Ping()
    if err == nil {
        return true
    }
    return false
}


// latency reporting
var latencyMeasurements chan st.Latency;
var measCountSinceHReport int
func reportLatency() {
    for {
        measCount := len(latencyMeasurements)
        measCountSinceHReport += measCount
        if measCount > 0 {
            lats := make([]st.Latency, measCount)
            for i := 0; i < measCount; i++ {
                t:= <- latencyMeasurements
                lats[i] = t
            }
            res := st.Results{Data: lats}
            d, err := json.Marshal(res)
            Check(err)
            url := "http://robinhood_stat_server/putstats"
            req, err := http.NewRequest("POST", url, bytes.NewBuffer(d))
            Check(err)
            req.Header.Set("Content-Type", "application/json")
            client := &http.Client{}
            resp, err := client.Do(req)
            if err != nil {
                continue
            }

            // get mem limits
            decoder := json.NewDecoder(resp.Body)
            memlims := make(st.MemLimits)
            err = decoder.Decode(&memlims)
            Check(err)
            // set memlims for shadow caches
            var totalCapacity int64 = 0
            for subName, mll := range memlims {
                var capsum int64 = 0
                var cacheCount = 0
                for _, lim := range mll.Limits {
                    capsum += lim
                    cacheCount++
                }
                Subs[subName].Shadow1.SetCapacity(int64(float64(capsum)/float64(cacheCount)))
                totalCapacity += capsum
            }
            for subName, mll := range memlims {
                var capsum int64 = 0
                var cacheCount = 0
                for _, ml := range mll.Limits {
                    capsum += ml
                    cacheCount++
                }
                // 2nd shadow caches gets 1% more cache space as would the controller do
                Subs[subName].Shadow2.SetCapacity(int64(float64(capsum)/float64(cacheCount) + float64(totalCapacity)/float64(cacheCount)/100.0))
            }
            // clean up
            resp.Body.Close()
            lats = nil
        }
        rc := atomic.LoadUint64(&reqCount)
        atomic.StoreUint64(&reqCount,0)
        reqCountSum += rc
        fmt.Println("reqc",rc,reqCountSum)
        time.Sleep(1 * time.Second)
    }
}

type Query struct {
    Id string
    Size int64
    Dep string
}
var querySequence chan Query;
func processQueries() {
    for {
        q := <- querySequence
        Subs[q.Dep].Shadow1.Request(q.Id, q.Size)
        Subs[q.Dep].Shadow2.Request(q.Id, q.Size)
    }
}
func reportHitratio() {
    for {
        // wait for 100 queries
        if measCountSinceHReport < 100000 {
            time.Sleep(time.Second * 2)
        } else {
            measCountSinceHReport = 0
            // fmt.Println("shadow's queue length",len(querySequence))
            // report statistics every 5 seconds
            hitstats := make(st.HitStatistic)
            for subName, subConf := range Subs {
                hr1 := subConf.Shadow1.GetAndResetHitRatio()
                hr2 := subConf.Shadow2.GetAndResetHitRatio()
                c1 := float64(subConf.Shadow1.GetCapacity())
                c2 := float64(subConf.Shadow2.GetCapacity())
                if c2==c1 {
                    hitstats[subName] = 0
                } else if hr2<hr1 {
                    fmt.Println("shadow error",hr1,hr2,c1,c2)
                    hitstats[subName] = 0
                } else {
                    hitstats[subName] = (hr2-hr1)/(c2-c1)
                    fmt.Println("hr",subName,hr1,hr2,c1,c2,"delta",hitstats[subName])
                }
            }
            d, err := json.Marshal(hitstats)
            Check(err)
            url := "http://robinhood_stat_server/puthrs"
            req, err := http.NewRequest("POST", url, bytes.NewBuffer(d))
            Check(err)
            req.Header.Set("Content-Type", "application/json")
            client := &http.Client{}
            resp, err := client.Do(req)
            if err != nil {
                fmt.Println("puthrs error", hitstats, "error:", err)
            } else {
                resp.Body.Close()
            }
        }
    }
}

// ping cache and record latency
func PingTest(cacheip string) {
    // read subsystems from cache config
    f, err := os.Open(*cacheConfigPath)
    Check(err)
    r := bufio.NewReader(f)
    dec := json.NewDecoder(r)
    myconfig := make([]st.DepConfig,64)
    err = dec.Decode(&myconfig)
    Check(err)
    testurl := "http://"+cacheip+":30005/"
    var startTime time.Time
    var endTime time.Time
    for {
        // cache test
        startTime = time.Now()
        resp, err := http.Get(testurl)
        endTime = time.Now()
        if err != nil {
            fmt.Println("latency get error",err,cacheip)
        } else {
            _, err = ioutil.ReadAll(resp.Body)
            if err != nil {
                fmt.Println("latency get error2",err,cacheip)
            } else {
                resp.Body.Close()
                respTime := endTime.Sub(startTime).Nanoseconds()
                if len(latencyMeasurements)<9000000 {
                    latencyMeasurements <- st.Latency{
                        Lat: respTime,
                        St: 1,
                        Tp: "ping",
                        Cp: "",
                    }
                } else {
                    fmt.Println("lf")
                }
            }
        }
        // backend test
        for _, conf := range DepConfigs {
            startTime = time.Now()
            backendPing := Subs[conf.Name].BackClient.Ping()
            endTime = time.Now()
            if !backendPing {
                fmt.Println("backend ping error",conf.Name)
            } else {
                respTime := endTime.Sub(startTime).Nanoseconds()
                if len(latencyMeasurements)<1000000 {
                    latencyMeasurements <- st.Latency{
                        Lat: respTime,
                        St: 0,
                        Tp: "ping",
                        Cp: "",
                    }
                }
            }
            // sleep
            time.Sleep(time.Millisecond*1)
        }
    }
}


// error check alias
func Check(err error) {
    if err != nil {
        panic(err)
    }
}

// initialize from depConfigs
func InitSubSystems(cacheip string) {
    // read app config
    f, err := os.Open(*appConfigPath)
    Check(err)
    r := bufio.NewReader(f)
    dec := json.NewDecoder(r)
    var appConf appConfig
    err = dec.Decode(&appConf)
    Check(err)
    fmt.Println(appConf)
    // read subsystems from cache config
    f, err = os.Open(*cacheConfigPath)
    Check(err)
    r = bufio.NewReader(f)
    dec = json.NewDecoder(r)
    DepConfigs = make([]st.DepConfig,64)
    err = dec.Decode(&DepConfigs)
    fmt.Println("using cacheip ",cacheip)
    for _,conf := range DepConfigs {
        for i, _ := range conf.CacheAddr{
            conf.CacheAddr[i] = fmt.Sprintf("%s:%d", cacheip, conf.CachePort)
        }
    }
    Check(err)
    // create subsystems
    Subs = make(map[string]Subsystem)
    var totalCapacity int64 = 0
    for _, conf := range DepConfigs {
        switch conf.BackendType {
        case "mysql":
            var dbServer = fmt.Sprintf("%s:%s@tcp(%s)/%s?timeout=10s&writeTimeout=10s&readTimeout=10s", appConf.Db.User, appConf.Db.Passwd, conf.BackendUrl, appConf.Db.Name)
            
            for {
                var err error
                var Db * sql.DB
                Db, err = sql.Open("mysql", dbServer)
                Check(err)
                Subs[conf.Name] = 
                    Subsystem {
                    CacheClient: memcache.New(conf.CacheAddr...),
                    CacheSema: NewSemaphore(appConf.Cache.MaxConcurrentReqs),
                    BackClient: MySqlBackConn{Conn: Db, R: rand.New(rand.NewSource(123)), WriteProb: conf.WriteProb,},
                    BackSema: NewSemaphore(conf.MaxOpenConns),
                    Shadow1: sq.NewCache(conf.CacheSize*1024),
                    Shadow2: sq.NewCache(conf.CacheSize*1024),
                }
                totalCapacity += conf.CacheSize*1024
                Subs[conf.Name].CacheClient.Timeout = time.Second
                Subs[conf.Name].CacheClient.MaxIdleConns = appConf.Cache.MaxConcurrentReqs
                fmt.Println("sub",conf.Name,dbServer,conf.CachePort)
                dbConnection := false
                cacheConnection := false
                dbConnection = Subs[conf.Name].BackClient.Ping()
                err = Subs[conf.Name].CacheClient.Set(&memcache.Item{Key: "works", Value: []byte("1")})
                if err == nil {
                    cacheConnection = true
                }
                if dbConnection && cacheConnection {
                    Db.SetMaxOpenConns(appConf.Db.MaxOpenConns)
                    Db.SetMaxIdleConns(appConf.Db.MaxIdleConns)
                    break
                }
                time.Sleep(time.Millisecond*300)
                fmt.Println("retrying. db:",dbConnection,"cache:",cacheConnection)
            }
        case "fb":
        loopretry:
            for {
                var err error
                clients := make(chan *rpc.Client,appConf.Fb.MaxConcurrentReqs)
                for i:=0; i<appConf.Fb.MaxConcurrentReqs; i++ {
                    rpcc, err := rpc.Dial("tcp", conf.BackendUrl+":37001")
                    if err != nil {
                        fmt.Println("rpc tcp dial failed")
                        time.Sleep(time.Millisecond*300)
                        continue loopretry
                    }
                    clients <- rpcc
                }
                if err == nil {
                    // fback backend parameters
                    var fpars st.FbackParameters
                    if conf.FbackPars == nil {
                        fpars = st.FbackParameters{
                            100,
                            1,
                            8,
                            17,
                        }
                    } else {
                        fpars = *conf.FbackPars
                    }
                    // make subsystem
                    Subs[conf.Name] = 
                        Subsystem {
                        CacheClient: memcache.New(conf.CacheAddr...),
                        CacheSema: NewSemaphore(appConf.Cache.MaxConcurrentReqs),
                        BackClient: FbBackConn{Conn: clients,Pars: fpars},
                        BackSema: NewSemaphore(conf.MaxOpenConns),
                        Shadow1: sq.NewCache(conf.CacheSize*1024),
                        Shadow2: sq.NewCache(conf.CacheSize*1024),
                    }
                    totalCapacity += conf.CacheSize*1024
                    Subs[conf.Name].CacheClient.Timeout = time.Second
                    Subs[conf.Name].CacheClient.MaxIdleConns = appConf.Cache.MaxConcurrentReqs
                    fmt.Println("sub_fb",conf.Name)
                    dbConnection := false
                    cacheConnection := false
                    dbConnection = Subs[conf.Name].BackClient.Ping()
                    err = Subs[conf.Name].CacheClient.Set(&memcache.Item{Key: "works", Value: []byte("1")})
                    if err == nil {
                        cacheConnection = true
                    }
                    if dbConnection && cacheConnection {
                        break
                    }
                    time.Sleep(time.Millisecond*300)
                    fmt.Println("retrying. fb:",dbConnection,"cache:",cacheConnection)
                } else {
                    time.Sleep(time.Millisecond*300)
                    fmt.Println("retrying. fb con failed")
                }
            }
        }
    }
    // update shadow caches
    for _, subConf := range Subs {
        subConf.Shadow2.SetCapacity(subConf.Shadow2.GetCapacity() + totalCapacity / 100)
    }

    // make measurement channel and start reporting
    latencyMeasurements = make(chan st.Latency, 10000000)
    go reportLatency()
    querySequence = make(chan Query, 100000)
    go processQueries()
    go reportHitratio()
    go PingTest(cacheip)
}



// execute a subquery and handle cache hits
func ExecuteSubquery(doneDeps chan<- st.Latency, dep string, url []string, cacheable []int8, startTime time.Time) {
    // if cache Enabled, query memcached
    hasError := false
    realSize := make(map[string]int64)
    //    url = url[:1]
    for _, key := range url {
        realSize[strings.ToLower(key)] = -1
    }
    cacheQueries := make([]string,len(realSize))
    cacheHits := make(map[string]bool)
    i := 0
    for key, _ := range realSize {
        cacheQueries[i] = dep+":"+key
        i++
    }
    fulfilled := 0
    backQueries := make([]string,0)

    /* 
       request from cache
                            */
    var timeHit time.Time

    if !BypassCaches {
	    queryCount := len(cacheQueries)
	    parallelQueries := int(queryCount/10) // truncate
        if parallelQueries < 1 {
            parallelQueries = 1
        }
        type CacheGetRes struct {
            itemMap map[string]*memcache.Item
            cacheErr error
        }
	    cacheGetChan := make(chan CacheGetRes,parallelQueries)
        if Subs[dep].CacheSema.Acquire() {
            // request from cache
            for i:=0; i<parallelQueries; i++ {
                var myids []string
                if i==parallelQueries-1 {
                    myids = cacheQueries[(i*10):]
                } else {
                    myids = cacheQueries[(i*10):((i+1)*10)]
                }
                go func(ids []string, cacheGetChan chan CacheGetRes) {
                    itemMap, err := Subs[dep].CacheClient.GetMulti(ids)
                    cacheGetChan <- CacheGetRes{itemMap, err}
                }(myids, cacheGetChan)
            }
            //timeout waiting
            itemMap := make(map[string]*memcache.Item)
            var tmpRes CacheGetRes
            retrieved := 0
            for retrieved < parallelQueries {
                select {
                case tmpRes = <-cacheGetChan:
                    if tmpRes.cacheErr == nil {
                        added:=0
                        for key, item := range tmpRes.itemMap {
                            itemMap[key] = item
                            added++
                        }
                    }
                    if tmpRes.cacheErr == nil || tmpRes.cacheErr == memcache.ErrCacheMiss {
                        retrieved++
                    } else {
                        fmt.Println("cache error",tmpRes.cacheErr,dep)
                        break
                    }
                case <-time.After(1 * time.Second):
                    fmt.Println("cache timeout",dep)
                    break
                }
//                fmt.Println("waiting",retrieved,parallelQueries,len(itemMap),queryCount,dep)
            }
            Subs[dep].CacheSema.Release()
            timeHit = time.Now()
            // parse results
            for _, key := range cacheQueries {
                realkey := strings.Replace(key, dep+":", "", 1)
                // check if in cache results
                _, ok := itemMap[key]
                if ok {
                    // cached
                    lsize := len(itemMap[key].Value)
                    if lsize < 1 {
                        fmt.Println("empty cache result",dep,realkey,lsize)
                        backQueries = append(backQueries,realkey)
                    } else {
                        realSize[realkey] = int64(lsize)
                        cacheHits[realkey] = true
                        fulfilled++
                    }
                } else {
                    backQueries = append(backQueries,realkey)
                }
                
            }
            //fmt.Println("ht",dep,len(cacheQueries),parallelQueries,len(backQueries),int(timeHit.Sub(startTime).Nanoseconds()/1e6))
        } else {
            // error
            fmt.Println("Cache get timed out", dep)
            hasError = true
        }
    } else {
        // Bypass
        // -> append all queries to backendQueries
        for _, key := range cacheQueries {
            realkey := strings.Replace(key, dep+":", "", 1)
            backQueries = append(backQueries,realkey)
        }
    }

    /* 
       request from backend
                            */
    var timeMiss time.Time = timeHit

    // check cache which items 
    if !hasError && len(backQueries)>0 {
        // retrieve all not yet fetched
        vals := make(map[string][]byte)
        var err error
        if Subs[dep].BackSema.Acquire() {
            vals, err = Subs[dep].BackClient.Request(backQueries)
            Subs[dep].BackSema.Release()
            timeMiss = time.Now()
            // only continue if no error
            if err==nil {
                //fmt.Println("Backend success", dep, key, len(bytes))
                for key, item := range vals {
                    realSize[key] = int64(len(item))
                    fulfilled++
                    if !BypassCaches {
                        // store in cache
                        if Subs[dep].CacheSema.Acquire() {
                            err = Subs[dep].CacheClient.Set(&memcache.Item{Key: dep+":"+key, Value: item}) 
                            Subs[dep].CacheSema.Release()
                            if err!=nil {
                                fmt.Println("Cache set error", dep, err)
                                hasError = true;
                            }
                        } else {
                            fmt.Println("Cache set timed out", dep)
                            hasError = true
                        }
                    }
                }
            } else {
                hasError = true;
                fmt.Println("Backend error", dep, err)
            }
        } else {
            fmt.Println("Backend Semaphore timed out", dep)
            hasError = true
        }
    }

    // sanity check that we got all queries
    if !hasError && len(realSize) != fulfilled {
        hasError = true
    }
    
    // calculate query latency
    var hitLatency int64
    var missLatency int64
    var status int8 = 0
    if hasError {
        hitLatency = int64(math.Pow(10,10)) // 10 seconds is the timeout below
        missLatency = hitLatency
        status = -1
    } else {
        hitLatency = timeHit.Sub(startTime).Nanoseconds()
        missLatency = timeMiss.Sub(startTime).Nanoseconds()
        // mark as hit if all queries hit
        if len(backQueries)==0 {
            status = 1
        }
    }

    
    // report subquery result to dependency tracking
    doneDeps <- st.Latency{
        Lat: missLatency,
        St: status,
        Tp: dep,
        Cp: "",
    }

    // latency reporting and cache simulation
    for key, itemSize := range realSize {
        curLatency := missLatency
        curStatus := status
        // check cache hit
        _, ok := cacheHits[key]
        if ok && !hasError {
            curLatency = hitLatency
            curStatus = 1 // mark as hit
        }
        latencyMeasurements <- st.Latency{
            Lat: curLatency,
            St: curStatus,
            Tp: dep,
            Cp: "",
        }
        // send to cache channel if there's still space
        if len(querySequence)<90000 {
            querySequence <- Query{Id: key, Size: itemSize, Dep: dep}
        }
    }
}

var reqCount uint64 = 0
var reqCountSum uint64 = 0
// parse a request struct and issue requests following dependency graph
func ParseRequest(layers QueryLayerSlice, startTime time.Time) {
    atomic.AddUint64(&reqCount, 1)
    // dependency tracking meta data
    doneDeps := make(chan st.Latency, 100) // channel that holds dependency nodes that have been reached
    waiting := make(map[string]bool) // store index of subquery of each depnode
    var reqState int8 = 1
	var slowestDepName string
    var slowestLatency int64 = 0
    var responseTime int64

    // process all subqueries
    var layer QueryLayer
loopLayers:
    for i := 0; i < len(layers); i++ {
        //fmt.Println("layer",i)
        layer = layers[i]
        // send all queries in this layer
        for dep, queries := range layer {
            // if dep in subs: execute
            _, ok := Subs[dep]
            if ok {
                waiting[dep] = true
                go ExecuteSubquery(doneDeps, dep, queries.U, queries.C, startTime)
            }
            // else: skip
        }
        // wait for all queries in this layer to finish
        if len(waiting) == 0 {
            // skip this layer
            continue
        }
    loopWaiting:
        for {
            select {
            case queryResult := <-doneDeps:
                // queryResult stores Latency (L), Request Status (R), Dependency Name (T)
                if queryResult.Lat > slowestLatency {
                    slowestDepName = queryResult.Tp
                    slowestLatency = queryResult.Lat
                }
                // if cache miss and not overall req state error, set req state to indicate cache miss
                if queryResult.St == -1 {
                    reqState = -1
                } else if queryResult.St == 0 && reqState != -1 {
                    reqState = 0
                }
                // advance state machine 
                delete(waiting,queryResult.Tp)
                if len(waiting) == 0 {
                    break loopWaiting
                }
            case <-time.After(time.Second * 10):
                // record as error
                reqState = -1
                //if len(slowestDepName) == 0 {
                // blame random outstanding request
                for dp, _ := range waiting {
                    slowestDepName = dp
                    break
                }
                //}
                // stop processing this request
                break loopLayers
            }
        }
    }

    if reqState != -1 {
        responseTime = time.Since(startTime).Nanoseconds()
    } else {
        responseTime = int64(math.Pow(10,10))
    }

    latencyMeasurements <- st.Latency{
        Lat: responseTime,
        St: reqState,
        Tp: "req",
        Cp: slowestDepName,
    }
}
