package shadowcache

import (
    "fmt"
	"container/list"
)

type ShadowCache struct {
	size int64
    capacity int64
	evictList *list.List
	items map[string]*list.Element
    hits, reqs uint64
}

type entry struct {
	id string
	size int64
}

func NewCache(cap int64) *ShadowCache {
	c := &ShadowCache{
        size: 0,
		capacity: cap,
		evictList: list.New(),
		items:     make(map[string]*list.Element),
        hits: 0,
        reqs: 0,
	}
	return c
}

func (c *ShadowCache) GetCapacity() int64 {
    return c.capacity
}

func (c *ShadowCache) SetCapacity(cap int64) {
    c.capacity = cap
}

func (c *ShadowCache) EnforceCapacity() {
        // Evict until space
        curCap := c.capacity
        for c.size > curCap {
            c.removeOldest()
        }
}

func (c *ShadowCache) removeOldest() {
	lentry := c.evictList.Back()
    if lentry == nil {
        fmt.Println("error removeoldest")
    }
	c.evictList.Remove(lentry)
    centry := lentry.Value.(*entry)
    id := centry.id
    size := centry.size
    c.size -= size
	delete(c.items, id)
}

func (c *ShadowCache) Request(id string, size int64) {
    c.reqs++
	// Check for existing item
	if lentry, ok := c.items[id]; ok {
        // Cache hit
		c.evictList.MoveToFront(lentry)
		c.hits++
	} else {
        // Cache miss
        // Admit new item
        centry := &entry{id, size}
        lentry := c.evictList.PushFront(centry)
        c.items[id] = lentry
        c.size += size
        c.EnforceCapacity()
    }
}

func (c *ShadowCache) GetAndResetHitRatio() float64 {
    if c.reqs == 0 {
     	return 0
	}
    hr:= float64(c.hits) / float64(c.reqs)
    c.hits = 0
    c.reqs = 0
    return hr
}
