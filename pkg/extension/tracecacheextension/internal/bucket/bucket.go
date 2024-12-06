package bucket

import (
	"fmt"
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

func (batch *WriteableBatch) GetAndReset() Batch {
	batch.lock.Lock()
	defer batch.lock.Unlock()

	result := batch.batch
	batch.batch = make(Batch, 0)
	return result
}

// 0      samplePeriod  cleanPeriod
// +———————————+————————————+
// r                        w
// r     - smapleBatch 缓存samplePeriod 秒后推送数据
// w     - cleanBatch  缓存cleanPeriod  秒后清理
type Bucket struct {
	buckets []Batch
	size    int
	// 当前写索引
	writeIndex int
}

func NewBucket(cleanPeriod int, name string) (*Bucket, error) {
	if cleanPeriod <= 0 {
		return nil, fmt.Errorf("invalid number of %s, it must be greater than zero", name)
	}

	buckets := make([]Batch, cleanPeriod+1)
	for i := 0; i <= cleanPeriod; i++ {
		buckets[i] = make(Batch, 0)
	}
	return &Bucket{
		buckets:    buckets,
		size:       cleanPeriod + 1,
		writeIndex: 0, // 从cleanPeriod 开始写入
	}, nil
}

func (bucket *Bucket) GetCleanPeriod() int {
	return bucket.size - 1
}

// 同一线程执行，无需加锁
func (bucket *Bucket) CopyAndGetBatch(batch Batch) (toCleanBatch Batch) {
	bucket.buckets[bucket.writeIndex] = batch
	bucket.writeIndex++
	if bucket.writeIndex >= bucket.size {
		bucket.writeIndex = 0
	}

	// 将下一个待写入的清空
	toCleanBatch = bucket.buckets[bucket.writeIndex]
	bucket.buckets[bucket.writeIndex] = nil
	return
}

// 同一线程执行，无需加锁
func (bucket *Bucket) CopyAndGetBatches(batch Batch, sampleTime int) (sampleBatch Batch, toCleanBatch Batch) {
	sampleIndex := bucket.writeIndex - sampleTime
	if sampleIndex < 0 {
		sampleIndex += bucket.size
	}
	sampleBatch = bucket.buckets[sampleIndex]

	toCleanBatch = bucket.CopyAndGetBatch(batch)
	return
}
