package idbucket

import (
	"errors"
	"sync"

	"go.opentelemetry.io/collector/pdata/pcommon"
)

var (
	ErrInvalidNumCleanPeriod = errors.New("invalid number of clean_time, it must be greater than zero")
	ErrInvalidNumBatches     = errors.New("invalid number of sample_time, it must be less than clean_time")
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

func (batch *WriteableBatch) AddToBatch(id pcommon.TraceID) {
	batch.lock.Lock()
	defer batch.lock.Unlock()

	batch.batch = append(batch.batch, id)
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
// r           w w+1
// r     - smapleBatch 缓存samplePeriod 秒后推送数据
// w     - newBatch
// w + 1 - cleanBatch  缓存cleanPeriod  秒后清理
type Bucket struct {
	buckets []Batch
	// 清理间隔
	size int
	// 当前读索引
	readIndex int
	// 当前写索引
	writeIndex int
}

func NewBucket(samplePeriod int, cleanPeriod int) (*Bucket, error) {
	if cleanPeriod <= 0 {
		return nil, ErrInvalidNumCleanPeriod
	}
	if samplePeriod > cleanPeriod {
		return nil, ErrInvalidNumBatches
	}
	if samplePeriod == 0 {
		samplePeriod = cleanPeriod
	}

	buckets := make([]Batch, cleanPeriod+1)
	for i := 0; i <= cleanPeriod; i++ {
		buckets[i] = make(Batch, 0)
	}
	return &Bucket{
		buckets:    buckets,
		size:       cleanPeriod,
		readIndex:  0,
		writeIndex: samplePeriod, // 从SamplePeriod 开始写入
	}, nil
}

// 同一线程执行，无需加锁
func (bucket *Bucket) CopyAndGetBatches(batch Batch) (sampleBatch Batch, toCleanBatch Batch) {
	bucket.buckets[bucket.writeIndex] = batch
	bucket.writeIndex++
	if bucket.writeIndex > bucket.size {
		bucket.writeIndex = 0
	}

	sampleBatch = bucket.buckets[bucket.readIndex]
	toCleanBatch = bucket.buckets[bucket.writeIndex]
	bucket.buckets[bucket.writeIndex] = nil
	bucket.readIndex++
	if bucket.readIndex > bucket.size {
		bucket.readIndex = 0
	}
	return
}
