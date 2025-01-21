package cache

import (
	"log"
	"sync"
	"sync/atomic"
)

type WriteableCache[T any] struct {
	lock  sync.Mutex
	cache map[string][]T
}

func NewWriteableCache[T any]() *WriteableCache[T] {
	return &WriteableCache[T]{
		cache: make(map[string][]T),
	}
}

func (c *WriteableCache[T]) Write(key string, data T) {
	c.lock.Lock()
	defer c.lock.Unlock()

	datas, exist := c.cache[key]
	if exist {
		datas = append(datas, data)
	} else {
		datas = []T{data}
	}
	c.cache[key] = datas
}

func (c *WriteableCache[T]) GetAndReset() map[string][]T {
	c.lock.Lock()
	defer c.lock.Unlock()

	result := c.cache
	c.cache = make(map[string][]T)
	return result
}

type Batch []string

type ReadableCache[T any] struct {
	name       string
	cache      map[string]*CacheData[T]
	buckets    []Batch
	readIndex  int
	writeIndex int
	size       int

	activeCount *atomic.Int64
}

func NewReadableCache[T any](name string, size int) *ReadableCache[T] {
	buckets := make([]Batch, size+1)
	for i := 0; i <= size; i++ {
		buckets[i] = make(Batch, 0)
	}

	return &ReadableCache[T]{
		name:        name,
		cache:       make(map[string]*CacheData[T]),
		buckets:     buckets,
		readIndex:   0,
		writeIndex:  size,
		size:        size + 1,
		activeCount: &atomic.Int64{},
	}
}

func (c *ReadableCache[T]) Copy(datas map[string][]T) {
	batch := make(Batch, 0)

	for key, value := range datas {
		if cacheData, ok := c.cache[key]; ok {
			cacheData.merge(value, c.writeIndex)
		} else {
			c.cache[key] = newCacheData(value, c.writeIndex)
			c.activeCount.Add(1)
		}
		batch = append(batch, key)
	}
	c.buckets[c.writeIndex] = batch

	c.writeIndex++
	if c.writeIndex >= c.size {
		c.writeIndex = 0
	}
}

func (c *ReadableCache[T]) PickMatchData(key string) []T {
	if cacheData, ok := c.cache[key]; ok {
		delete(c.cache, key)
		return cacheData.datas
	}
	return nil
}

func (c *ReadableCache[T]) PickMatchDatas(keys []string) []T {
	results := make([]T, 0)
	for _, key := range keys {
		if cacheData, ok := c.cache[key]; ok {
			delete(c.cache, key)
			results = append(results, cacheData.datas...)
		}
	}
	return results
}

func (c *ReadableCache[T]) CleanExpireDatas(logEnable bool) {
	var cleanCount int64 = 0
	bucket := c.buckets[c.readIndex]
	for _, key := range bucket {
		if cacheData, ok := c.cache[key]; ok {
			if cacheData.index == c.readIndex {
				delete(c.cache, key)
				cleanCount += 1
			}
		}
	}

	c.readIndex++
	if c.readIndex >= c.size {
		c.readIndex = 0
	}

	if logEnable && cleanCount > 0 {
		currentCount := c.activeCount.Add(-cleanCount)
		log.Printf("[Clean %s] active: %d, cleaned: %d", c.name, currentCount, cleanCount)
	}
}

type CacheData[T any] struct {
	datas []T
	index int
}

func newCacheData[T any](datas []T, index int) *CacheData[T] {
	return &CacheData[T]{
		datas: datas,
		index: index,
	}
}

func (cache *CacheData[T]) merge(datas []T, index int) {
	cache.datas = append(cache.datas, datas...)
	cache.index = index
}
