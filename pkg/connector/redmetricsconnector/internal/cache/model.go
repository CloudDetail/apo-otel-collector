package cache

import (
	"bytes"

	"go.opentelemetry.io/collector/pdata/pcommon"
)

const (
	metricKeySeparator = string(byte(0))
)

type ReusedKeyValue struct {
	maxAttribute int
	keys         []string
	vals         []string
	size         int
	dest         *bytes.Buffer
}

func NewReusedKeyValue(maxAttribute int) *ReusedKeyValue {
	return &ReusedKeyValue{
		maxAttribute: maxAttribute,
		keys:         make([]string, maxAttribute),
		vals:         make([]string, maxAttribute),
		dest:         bytes.NewBuffer(make([]byte, 0, 1024)),
	}
}

func (kv *ReusedKeyValue) Reset() {
	kv.size = 0
}

func (kv *ReusedKeyValue) Add(key string, value string) *ReusedKeyValue {
	if kv.size < kv.maxAttribute {
		kv.keys[kv.size] = key
		kv.vals[kv.size] = value

		kv.size++
	}

	return kv
}

func (kv *ReusedKeyValue) GetValue() string {
	kv.dest.Reset()
	for i := 0; i < kv.size; i++ {
		if i > 0 {
			kv.dest.WriteString(metricKeySeparator)
		}
		kv.dest.WriteString(kv.vals[i])
	}
	return kv.dest.String()
}

func (kv *ReusedKeyValue) GetMap() pcommon.Map {
	dims := pcommon.NewMap()
	for i := 0; i < kv.size; i++ {
		dims.PutStr(kv.keys[i], kv.vals[i])
	}
	return dims
}
