package adaptive

import "runtime"

const (
	mibBytes = 1024 * 1024
)

type MemoryCheck struct {
	memoryLimit uint64
}

func NewMemoryCheck(memoryLimit uint64) *MemoryCheck {
	return &MemoryCheck{
		memoryLimit: memoryLimit,
	}
}

func (check *MemoryCheck) GetCurrentMemory() uint64 {
	ms := &runtime.MemStats{}
	runtime.ReadMemStats(ms)

	return ms.Alloc / mibBytes
}

func (check *MemoryCheck) GetMemoryLimit() uint64 {
	return check.memoryLimit
}
