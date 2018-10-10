package main

import (
	"context"
	"fmt"
	"bytes"
	"net/http"
	"encoding/json"
	"time"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	//	"github.com/moby/moby/api/types/swarm"
)


type StatsJson struct {
    /*    Id       string `json:"id"`
    Read     string `json:"read"`
    Preread  string `json:"preread"` */
    CpuStats cpu `json:"cpu_stats"`
    PreCpuStats cpu `json:"precpu_stats"`
    Name string `json:"name"`
    MemStats mem `json:"memory_stats"`
    NetStats map[string]net `json:"networks"`
    BulkIoStats bulkio `json:"blkio_stats"`
}

type bulkio struct {
    Iops []io `json:"io_serviced_recursive"`
}

type io struct {
    Major int `json:"major"`
    Minor int `json:"minor"`
    Op string `json:"op"`
    Value uint64 `json:"value"`
}

type cpu struct {
    Usage cpuUsage `json:"cpu_usage"`
    SystemUsage float64 `json:"system_cpu_usage"`
}

type cpuUsage struct {
    Total float64 `json:"total_usage"`
    PerCPU []float64 `json:"percpu_usage"`
}

type mem struct {
    MaxUsage float64 `json:"max_usage"`
    Limit float64 `json:"limit"`
}

type net struct {
    RxBytes float64 `json:"rx_bytes"`
    RxDropped float64 `json:"rx_dropped"`
    TxBytes float64 `json:"tx_bytes"`
    TxDropped float64 `json:"tx_dropped"`
}

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
type ResultSet map[string]Result


// error check alias
func Check(err error) {
    if err != nil {
        fmt.Println(err)
    }
}

func main() {
	ctx := context.Background()
	cli, err := client.NewEnvClient()
	if err != nil {
		panic(err)
	}

	type totaliops struct {
        readiops uint64
        writeiops uint64
	}
	previo := make(map[string]totaliops)
    prevnet := make(map[string]net)

    var lastT time.Time = time.Now()
	for {
		containers, err := cli.ContainerList(context.Background(), types.ContainerListOptions{})
		if err != nil {
			panic(err)
		}
        curT := time.Now()
        curDur := curT.Sub(lastT).Seconds()
		rs := make(ResultSet)

		for _, container := range containers {
			//		fmt.Println(container.ID)
			//		fmt.Println(container.Names,container.Created,container.HostConfig)
			stats, err := cli.ContainerStats(ctx, container.ID, false)
			Check(err)
            /*			
			rr := stats.Body
			buf := new(bytes.Buffer)
			buf.ReadFrom(rr)
			s := buf.String() // Does a complete co	py of the bytes in the buffer.
			fmt.Println("stats\n",s,"\n")
*/

			decoder := json.NewDecoder(stats.Body)
			var containerStats StatsJson
			err = decoder.Decode(&containerStats)
			Check(err)
			
			//parse cpu
			system_delta := containerStats.CpuStats.SystemUsage - containerStats.PreCpuStats.SystemUsage
			var cpu_delta_sum float64 = 0
			var cpu_delta float64
			for i := 0; i < len(containerStats.CpuStats.Usage.PerCPU); i++ {
			    cpu_delta = containerStats.CpuStats.Usage.PerCPU[i] - containerStats.PreCpuStats.Usage.PerCPU[i]
			    cpu_delta_sum += cpu_delta
			}
			totalCPU := cpu_delta_sum / system_delta

			// parse mem
			maxMem := containerStats.MemStats.MaxUsage/containerStats.MemStats.Limit

			// parse net
			var sumRxB float64 = 0
			var sumRxD float64 = 0
			var sumTxB float64 = 0
			var sumTxD float64 = 0
			for _, netstat := range containerStats.NetStats {
			    sumRxB += netstat.RxBytes
			    sumRxD += netstat.RxDropped
			    sumTxB += netstat.TxBytes
			    sumTxD += netstat.TxDropped
			}
            
            // convert net bw into per second
			var rxB float64 = 0
			var rxD float64 = 0
			var txB float64 = 0
			var txD float64 = 0
			prevNetEntry, ok := prevnet[containerStats.Name]
			if ok {
                rxB = float64(sumRxB - prevNetEntry.RxBytes)/curDur
                rxD = float64(sumRxD - prevNetEntry.RxDropped)/curDur
                txB = float64(sumTxB - prevNetEntry.TxBytes)/curDur
                txD = float64(sumTxD - prevNetEntry.TxDropped)/curDur
			}
			prevnet[containerStats.Name] = net{sumRxB, sumRxD, sumTxB, sumTxD,}


			// parse iops
			var sumReadIops uint64
			var sumWriteIops uint64
			for _, ioentry := range containerStats.BulkIoStats.Iops {
			    switch ioentry.Op {
                case "Read":
                    sumReadIops += ioentry.Value
                case "Write":
                    sumWriteIops += ioentry.Value
                }
			}
            // convert iops into per second
			var readiops float64 = 0
			var writeiops float64 = 0
			preventry, ok := previo[containerStats.Name]
			if ok {
                readiops = float64(sumReadIops - preventry.readiops)/curDur
                writeiops = float64(sumWriteIops - preventry.writeiops)/curDur
			}
			previo[containerStats.Name] = totaliops{sumReadIops, sumWriteIops, }

			rs[containerStats.Name] = Result{
				Mem: maxMem,
				CPU: totalCPU,
				NetRx: rxB,
				NetRd: rxD,
				NetTx: txB,
				NetTd: txD,
				IoRead: readiops,
				IoWrite: writeiops,
			}
			stats.Body.Close()
		}
		//fmt.Println(rs)
		d, err := json.Marshal(rs)
		Check(err)

		url := "http://robinhood_stat_server/pututils"
		resp, err := http.Post(url, "application/json", bytes.NewBuffer(d))
		Check(err)
		if err == nil {
            resp.Body.Close()
        }

        // update timestamp and sleep 1 second
        lastT = curT
		time.Sleep(time.Second * 1)
	}

}
