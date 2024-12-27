package cache

import (
	"sync"

	"go.opentelemetry.io/collector/pdata/pcommon"
)

// Batch is the type of batches held by the Batcher.
type Batch []pcommon.TraceID

type WriteableBatch struct {
	lock  sync.Mutex
	batch Batch
}

func NewWriteableBatch() *WriteableBatch {
	return &WriteableBatch{
		batch: make(Batch, 0),
	}
}

func (batch *WriteableBatch) AddToBatch(data pcommon.TraceID) {
	batch.lock.Lock()
	defer batch.lock.Unlock()

	batch.batch = append(batch.batch, data)
}

func (batch *WriteableBatch) PickBatch() Batch {
	batch.lock.Lock()
	defer batch.lock.Unlock()

	result := batch.batch
	batch.batch = make(Batch, 0)
	return result
}

type Buckets struct {
	buckets    []Batch
	writeIndex int
}

func NewBuckets(size int) *Buckets {
	buckets := make([]Batch, size+1)
	for i := 0; i <= size; i++ {
		buckets[i] = make(Batch, 0)
	}
	return &Buckets{
		buckets:    buckets,
		writeIndex: size,
	}
}

// 同一线程执行，无需加锁
func (bucket *Buckets) CopyAndGetBatch(batch Batch) (result Batch) {
	bucket.buckets[bucket.writeIndex] = batch
	bucket.writeIndex++
	if bucket.writeIndex >= len(bucket.buckets) {
		bucket.writeIndex = 0
	}

	result = bucket.buckets[bucket.writeIndex]
	// Clean Data in buckets.
	bucket.buckets[bucket.writeIndex] = nil
	return
}
