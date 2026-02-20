//go:build darwin

package sysinfo

import (
	"encoding/binary"
	"fmt"
	"syscall"

	"golang.org/x/sys/unix"
)

func newRealSystemReader() SystemReader {
	return &darwinReader{}
}

type darwinReader struct{}

func (r *darwinReader) DiskInfo(path string) (*DiskInfo, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return nil, fmt.Errorf("statfs %s: %w", path, err)
	}
	// On Darwin, Bsize is int32; safe to convert to uint64 for positive values.
	bsize := uint64(stat.Bsize)
	total := stat.Blocks * bsize
	free := stat.Bfree * bsize
	used := total - free
	var usedPct float64
	if total > 0 {
		usedPct = float64(used) / float64(total) * 100
	}
	return &DiskInfo{
		Path:       path,
		TotalBytes: total,
		FreeBytes:  free,
		UsedBytes:  used,
		UsedPct:    usedPct,
	}, nil
}

func (r *darwinReader) MemoryInfo() (*MemoryInfo, error) {
	// hw.memsize is a uint64 sysctl on macOS.
	raw, err := unix.SysctlRaw("hw.memsize")
	if err != nil {
		return nil, fmt.Errorf("sysctl hw.memsize: %w", err)
	}
	if len(raw) < 8 {
		return nil, fmt.Errorf("hw.memsize: unexpected byte length %d", len(raw))
	}
	total := binary.LittleEndian.Uint64(raw[:8])
	// Getting available memory on Darwin requires vm_stat or Mach host_statistics,
	// which is not exposed via pure Go stdlib. Return total only.
	return &MemoryInfo{
		TotalBytes:     total,
		AvailableBytes: 0,
		UsedBytes:      0,
		UsedPct:        0,
	}, nil
}

func (r *darwinReader) LoadInfo() (*LoadInfo, error) {
	// vm.loadavg returns struct { load[3] uint32; scale uint32 } on Darwin.
	raw, err := unix.SysctlRaw("vm.loadavg")
	if err != nil {
		return nil, fmt.Errorf("sysctl vm.loadavg: %w", err)
	}
	if len(raw) < 16 {
		return nil, fmt.Errorf("vm.loadavg: unexpected byte length %d", len(raw))
	}

	load0 := binary.LittleEndian.Uint32(raw[0:4])
	load1 := binary.LittleEndian.Uint32(raw[4:8])
	load2 := binary.LittleEndian.Uint32(raw[8:12])
	scale := binary.LittleEndian.Uint32(raw[12:16])

	if scale == 0 {
		return nil, fmt.Errorf("vm.loadavg: scale is zero")
	}
	return &LoadInfo{
		Load1:  float64(load0) / float64(scale),
		Load5:  float64(load1) / float64(scale),
		Load15: float64(load2) / float64(scale),
	}, nil
}

func (r *darwinReader) OSInfo() (*OSInfo, error) {
	return defaultOSInfo()
}
