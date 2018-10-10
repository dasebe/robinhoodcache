package main

import (
	sq "subquery"
	"net/http"
	"flag"
	"fmt"
    "net"
	"log"
    "os"
    "time"
	"encoding/json"
)

var appServerPort *int = flag.Int("appServerPort", 80, "appServer port")
var cacheip *string = flag.String("cacheip", "127.0.0.1", "memcache server ip")
var timeouts uint64

func JsonHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
    var req sq.JsonRequest
	err := decoder.Decode(&req)
	sq.Check(err)
    timeStart := time.Now()
	go sq.ParseRequest(req.D, timeStart)
    time.Sleep(time.Microsecond*10)
	fmt.Fprintln(w, 1)
}

func FlushHandler(w http.ResponseWriter, r *http.Request) {
	for _, conf := range sq.DepConfigs {
		sq.Subs[conf.Name].CacheSema.Acquire()
		sq.Subs[conf.Name].CacheClient.FlushAll()
		sq.Subs[conf.Name].CacheSema.Release()
		fmt.Fprintln(w, "Flushed", conf.Name)
	}
}

func IdHandler(w http.ResponseWriter, r *http.Request) {
	ifaces, err := net.Interfaces()
	if err != nil {
        fmt.Fprintln(w, "fail")
		return
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue // interface down
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue // loopback interface
		}
        if iface.Name != "eth2" {
            continue
        }
		addrs, err := iface.Addrs()
		if err != nil {
            fmt.Fprintln(w, "fail")
            return
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue // not an ipv4 address
			}
            //			return ip.String(), nil
            fmt.Fprintln(w, ip.String())
            return
		}
	}
    fmt.Fprintln(w, "fail")
    return
}

func main() {
	flag.Parse()

    
    value := os.Getenv("BYPASS")
    if len(value) == 0 {
        sq.BypassCaches = false
    } else {
        sq.BypassCaches = true
    }

	sq.InitSubSystems(*cacheip)

	http.HandleFunc("/json", JsonHandler)
	http.HandleFunc("/flush", FlushHandler)
    http.HandleFunc("/id", IdHandler)
	log.Fatal(
		http.ListenAndServe(
			fmt.Sprintf(":%d", *appServerPort),
			nil,
		),
	)
}
