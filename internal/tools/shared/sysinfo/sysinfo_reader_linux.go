//go:build linux

package sysinfo

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

func newRealSystemReader() SystemReader {
	return &linuxReader{}
}

type linuxReader struct{}

func (r *linuxReader) DiskInfo(path string) (*DiskInfo, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return nil, fmt.Errorf("statfs %s: %w", path, err)
	}
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

func (r *linuxReader) MemoryInfo() (*MemoryInfo, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return nil, fmt.Errorf("open /proc/meminfo: %w", err)
	}
	defer f.Close()

	vals := map[string]uint64{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSuffix(parts[0], ":")
		val, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			continue
		}
		vals[key] = val * 1024 // kB → bytes
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read /proc/meminfo: %w", err)
	}

	total := vals["MemTotal"]
	available := vals["MemAvailable"]
	used := total - available
	var usedPct float64
	if total > 0 {
		usedPct = float64(used) / float64(total) * 100
	}
	return &MemoryInfo{
		TotalBytes:     total,
		AvailableBytes: available,
		UsedBytes:      used,
		UsedPct:        usedPct,
	}, nil
}

func (r *linuxReader) LoadInfo() (*LoadInfo, error) {
	f, err := os.Open("/proc/loadavg")
	if err != nil {
		return nil, fmt.Errorf("open /proc/loadavg: %w", err)
	}
	defer f.Close()

	var load1, load5, load15 float64
	_, err = fmt.Fscanf(f, "%f %f %f", &load1, &load5, &load15)
	if err != nil {
		return nil, fmt.Errorf("parse /proc/loadavg: %w", err)
	}
	return &LoadInfo{Load1: load1, Load5: load5, Load15: load15}, nil
}

func (r *linuxReader) OSInfo() (*OSInfo, error) {
	return defaultOSInfo()
}
