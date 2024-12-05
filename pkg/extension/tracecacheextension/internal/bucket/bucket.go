package bucket

import (
	"fmt"
	"sync"
)

type WriteableBatch[T any] struct {
	lock  sync.Mutex
	batch []T
}

func NewWriteableBatch[T any]() *WriteableBatch[T] {
	return &WriteableBatch[T]{
		batch: make([]T, 0),
	}
}

func (batch *WriteableBatch[T]) AddToBatch(data T) {
	batch.lock.Lock()
	defer batch.lock.Unlock()

	batch.batch = append(batch.batch, data)
}

func (batch *WriteableBatch[T]) GetAndReset() []T {
	batch.lock.Lock()
	defer batch.lock.Unlock()

	result := batch.batch
	batch.batch = make([]T, 0)
	return result
}

type Batch[T any] []T

// 0      samplePeriod  cleanPeriod
// +———————————+————————————+
// r                        w
// r     - smapleBatch 缓存samplePeriod 秒后推送数据
// w     - cleanBatch  缓存cleanPeriod  秒后清理
type Bucket[T any] struct {
	buckets []Batch[T]
	size    int
	// 当前写索引
	writeIndex int
}

func NewBucket[T any](cleanPeriod int, name string) (*Bucket[T], error) {
	if cleanPeriod <= 0 {
		return nil, fmt.Errorf("invalid number of %s, it must be greater than zero", name)
	}

	buckets := make([]Batch[T], cleanPeriod+1)
	for i := 0; i <= cleanPeriod; i++ {
		buckets[i] = make(Batch[T], 0)
	}
	return &Bucket[T]{
		buckets:    buckets,
		size:       cleanPeriod + 1,
		writeIndex: 0, // 从cleanPeriod 开始写入
	}, nil
}

func (bucket *Bucket[T]) GetCleanPeriod() int {
	return bucket.size - 1
}

// 同一线程执行，无需加锁
func (bucket *Bucket[T]) CopyAndGetBatch(batch Batch[T]) (toCleanBatch Batch[T]) {
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
func (bucket *Bucket[T]) CopyAndGetBatches(batch Batch[T], sampleTime int, delayTime int) (closeBatch Batch[T], sampleBatch Batch[T], toCleanBatch Batch[T]) {
	sampleIndex := bucket.writeIndex + sampleTime
	if sampleIndex >= bucket.size {
		sampleIndex -= bucket.size
	}
	sampleBatch = bucket.buckets[sampleIndex]

	closeIndex := sampleIndex + delayTime
	if closeIndex >= bucket.size {
		closeIndex -= bucket.size
	}
	closeBatch = bucket.buckets[closeIndex]

	toCleanBatch = bucket.CopyAndGetBatch(batch)
	return
}
