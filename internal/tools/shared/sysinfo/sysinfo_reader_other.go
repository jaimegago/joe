//go:build !linux && !darwin

package sysinfo

import (
	"fmt"
	"runtime"
)

func newRealSystemReader() SystemReader {
	return &stubReader{}
}

type stubReader struct{}

func (r *stubReader) DiskInfo(_ string) (*DiskInfo, error) {
	return nil, fmt.Errorf("disk stats not supported on this platform (%s)", platformName())
}

func (r *stubReader) MemoryInfo() (*MemoryInfo, error) {
	return nil, fmt.Errorf("memory stats not supported on this platform (%s)", platformName())
}

func (r *stubReader) LoadInfo() (*LoadInfo, error) {
	return nil, fmt.Errorf("load stats not supported on this platform (%s)", platformName())
}

func (r *stubReader) OSInfo() (*OSInfo, error) {
	return defaultOSInfo()
}

func platformName() string {
	return runtime.GOOS
}
