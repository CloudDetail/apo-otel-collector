package idbucket

import (
	"encoding/binary"
	"testing"

	"go.opentelemetry.io/collector/pdata/pcommon"
)

func TestBucket(t *testing.T) {
	var (
		batch_empty = Batch([]pcommon.TraceID{})
		batch_0     = Batch(genterateTraceIds(1, 5))
		batch_1     = Batch(genterateTraceIds(5, 10))
		batch_2     = Batch(genterateTraceIds(10, 13))
		batch_3     = Batch(genterateTraceIds(13, 18))
		batch_4     = Batch(genterateTraceIds(18, 30))
		batch_5     = Batch(genterateTraceIds(30, 32))
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
	}

	bucket, _ := NewBucket(2, 5)
	for _, test := range tests {
		sample, expire := bucket.CopyAndGetBatches(test.data)
		if len(sample) != len(test.sampleWant) {
			t.Errorf("sample size() = %v, want %v", len(sample), len(test.sampleWant))
		}
		for i, sampleGot := range sample {
			if sampleGot != test.sampleWant[i] {
				t.Errorf("sample[%d] = %v, want %v", i, sampleGot.String(), test.sampleWant[i].String())
			}
		}
		if len(expire) != len(test.expireWant) {
			t.Errorf("expire size() = %v, want %v", len(expire), len(test.expireWant))
		}
		for i, expireGot := range expire {
			if expireGot != test.expireWant[i] {
				t.Errorf("expire[%d] = %v, want %v", i, expireGot.String(), test.expireWant[i].String())
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
