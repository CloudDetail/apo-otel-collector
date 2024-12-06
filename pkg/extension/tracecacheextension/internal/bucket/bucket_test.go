package bucket

import (
	"encoding/binary"
	"testing"

	"go.opentelemetry.io/collector/pdata/pcommon"
)

func TestCopyAndGetBatch(t *testing.T) {
	var (
		batch_empty = Batch([]pcommon.TraceID{})
		batch_0     = Batch(genterateTraceIds(1, 5))
		batch_1     = Batch(genterateTraceIds(5, 10))
		batch_2     = Batch(genterateTraceIds(10, 13))
		batch_3     = Batch(genterateTraceIds(13, 18))
		batch_4     = Batch(genterateTraceIds(18, 30))
		batch_5     = Batch(genterateTraceIds(30, 32))
		batch_6     = Batch(genterateTraceIds(33, 40))
	)

	tests := []struct {
		data       Batch
		expireWant Batch
	}{
		{
			data:       batch_0,
			expireWant: batch_empty,
		},
		{
			data:       batch_1,
			expireWant: batch_empty,
		},
		{
			data:       batch_2,
			expireWant: batch_empty,
		},
		{
			data:       batch_3,
			expireWant: batch_empty,
		},
		{
			data:       batch_4,
			expireWant: batch_empty,
		},
		{
			data:       batch_5,
			expireWant: batch_0,
		},
		{
			data:       batch_6,
			expireWant: batch_1,
		},
	}

	bucket, _ := NewBucket(5, "expire_time")
	for i, test := range tests {
		expire := bucket.CopyAndGetBatch(test.data)
		if len(expire) != len(test.expireWant) {
			t.Errorf("Test[%d] expire size() = %v, want %v", i, len(expire), len(test.expireWant))
		}
		for j, expireGot := range expire {
			if expireGot != test.expireWant[j] {
				t.Errorf("Test[%d] expire[%d] = %v, want %v", i, j, expireGot.String(), test.expireWant[j].String())
			}
		}
	}
}

func TestCopyAndGetBatches(t *testing.T) {
	var (
		batch_empty = Batch([]pcommon.TraceID{})
		batch_0     = Batch(genterateTraceIds(1, 5))
		batch_1     = Batch(genterateTraceIds(5, 10))
		batch_2     = Batch(genterateTraceIds(10, 13))
		batch_3     = Batch(genterateTraceIds(13, 18))
		batch_4     = Batch(genterateTraceIds(18, 30))
		batch_5     = Batch(genterateTraceIds(30, 32))
		batch_6     = Batch(genterateTraceIds(33, 40))
	)

	tests := []struct {
		data       Batch
		sampleWant Batch
		expireWant Batch
	}{
		{
			data:       batch_0,
			sampleWant: batch_empty,
			expireWant: batch_empty,
		},
		{
			data:       batch_1,
			sampleWant: batch_empty,
			expireWant: batch_empty,
		},
		{
			data:       batch_2,
			sampleWant: batch_0,
			expireWant: batch_empty,
		},
		{
			data:       batch_3,
			sampleWant: batch_1,
			expireWant: batch_empty,
		},
		{
			data:       batch_4,
			sampleWant: batch_2,
			expireWant: batch_empty,
		},
		{
			data:       batch_5,
			sampleWant: batch_3,
			expireWant: batch_0,
		},
		{
			data:       batch_6,
			sampleWant: batch_4,
			expireWant: batch_1,
		},
	}

	bucket, _ := NewBucket(5, "expire_time")
	for i, test := range tests {
		sample, expire := bucket.CopyAndGetBatches(test.data, 2)
		if len(sample) != len(test.sampleWant) {
			t.Errorf("Test[%d] sample size() = %v, want %v", i, len(sample), len(test.sampleWant))
		}
		for j, sampleGot := range sample {
			if sampleGot != test.sampleWant[j] {
				t.Errorf("Test[%d] sample[%d] = %v, want %v", i, j, sampleGot.String(), test.sampleWant[j].String())
			}
		}
		if len(expire) != len(test.expireWant) {
			t.Errorf("Test[%d] expire size() = %v, want %v", i, len(expire), len(test.expireWant))
		}
		for k, expireGot := range expire {
			if expireGot != test.expireWant[k] {
				t.Errorf("Test[%d] expire[%d] = %v, want %v", i, k, expireGot.String(), test.expireWant[k].String())
			}
		}
	}
}

func genterateTraceIds(from int, to int) []pcommon.TraceID {
	ids := make([]pcommon.TraceID, to-from+1)
	for i := 0; i < to-from; i++ {
		traceID := [16]byte{}
		binary.BigEndian.PutUint64(traceID[:8], 0)
		binary.BigEndian.PutUint64(traceID[8:], uint64(i))
		ids[i] = pcommon.TraceID(traceID)
	}
	return ids
}
