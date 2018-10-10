package statquery

import ()

// config
type FbackParameters struct {
    Hardness int
    IdCount int
    SizeLower int
    SizeUpper int
}
type DepConfig struct {
    Name string
    MaxOpenConns int
    CacheAddr []string
    CachePort int
    CacheSize int64
    BackendType string
    BackendUrl string
    WriteProb float32
    FbackPars *FbackParameters
}

// types for latency reporting
type Latency struct {
    Lat int64 // latency
    St int8 // result: 0 cache miss, 1 cache hit, -1 error (later manually add type 2 - overall)
    Tp string // request type
    Cp string // Critpath: only set if Tp=="req", then slowest request type
}
type Results struct{
    Data []Latency
}

type MemLimit struct {
    Limits []int64
    Mallocs []int64
}
type MemLimits map[string]MemLimit
type PerControllerMemLimits map[string]MemLimits

type HitStatistic map[string]float64
type HitStatEntry struct {
    RAddr string
    HitStat HitStatistic
}

type GetStatResult map[string]interface{}

type LatHeap []int64

func (h LatHeap) Len() int           { return len(h) }
func (h LatHeap) Less(i, j int) bool { return h[i] < h[j] }
func (h LatHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *LatHeap) Push(x interface{}) {
    // Push and Pop use pointer receivers because they modify the slice's length,
    // not just its contents.
    *h = append(*h, x.(int64))
}

func (h *LatHeap) Pop() interface{} {
    old := *h
    n := len(old)
    x := old[n-1]
    *h = old[0 : n-1]
    return x
}
