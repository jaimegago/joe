// Package sysinfo provides a Go-native system diagnostics tool.
// No external CLI dependencies — uses syscall and runtime from the standard library.
// Replaces df, free, uptime, and uname for structured system info.
package sysinfo

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/jaimegago/joe/internal/llm"
)

// SystemReader abstracts OS-level calls for testability.
type SystemReader interface {
	DiskInfo(path string) (*DiskInfo, error)
	MemoryInfo() (*MemoryInfo, error)
	LoadInfo() (*LoadInfo, error)
	OSInfo() (*OSInfo, error)
}

// DiskInfo holds filesystem usage for a single path.
type DiskInfo struct {
	Path       string  `json:"path"`
	TotalBytes uint64  `json:"total_bytes"`
	FreeBytes  uint64  `json:"free_bytes"`
	UsedBytes  uint64  `json:"used_bytes"`
	UsedPct    float64 `json:"used_pct"`
}

// MemoryInfo holds system memory statistics.
type MemoryInfo struct {
	TotalBytes     uint64  `json:"total_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	UsedPct        float64 `json:"used_pct"`
}

// LoadInfo holds system load averages.
type LoadInfo struct {
	Load1  float64 `json:"load_1m"`
	Load5  float64 `json:"load_5m"`
	Load15 float64 `json:"load_15m"`
}

// OSInfo holds basic OS identification.
type OSInfo struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
}

// SystemInfoResult is the structured result of a system_info query.
type SystemInfoResult struct {
	Disk   *DiskInfo   `json:"disk,omitempty"`
	Memory *MemoryInfo `json:"memory,omitempty"`
	Load   *LoadInfo   `json:"load,omitempty"`
	OS     *OSInfo     `json:"os,omitempty"`
	Errors []string    `json:"errors,omitempty"`
}

// SystemInfoTool returns structured system diagnostics.
// Replaces df, free, uptime, and uname.
type SystemInfoTool struct {
	Reader SystemReader
}

// NewSystemInfoTool creates a SystemInfoTool using the real OS-level reader.
func NewSystemInfoTool() *SystemInfoTool {
	return &SystemInfoTool{Reader: newRealSystemReader()}
}

func (t *SystemInfoTool) Name() string { return "system_info" }

func (t *SystemInfoTool) Description() string {
	return "Return structured system statistics: disk usage, memory, load averages, and OS info. Replaces df/free/uptime/uname. Use sections to limit output. Disk stats use the root filesystem by default."
}

func (t *SystemInfoTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"sections": {
				Type:        "string",
				Description: "Comma-separated list of sections to return: disk, memory, load, os, all. Default: all.",
			},
			"disk_path": {
				Type:        "string",
				Description: "Filesystem path for disk stats. Default: /",
			},
		},
	}
}

func (t *SystemInfoTool) Execute(_ context.Context, args map[string]any) (any, error) {
	sections, _ := args["sections"].(string)
	if sections == "" {
		sections = "all"
	}

	diskPath := "/"
	if p, ok := args["disk_path"].(string); ok && p != "" {
		diskPath = p
	}

	want := parseSections(sections)
	result := SystemInfoResult{}

	if want["disk"] || want["all"] {
		info, err := t.Reader.DiskInfo(diskPath)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("disk: %v", err))
		} else {
			result.Disk = info
		}
	}

	if want["memory"] || want["all"] {
		info, err := t.Reader.MemoryInfo()
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("memory: %v", err))
		} else {
			result.Memory = info
		}
	}

	if want["load"] || want["all"] {
		info, err := t.Reader.LoadInfo()
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("load: %v", err))
		} else {
			result.Load = info
		}
	}

	if want["os"] || want["all"] {
		info, err := t.Reader.OSInfo()
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("os: %v", err))
		} else {
			result.OS = info
		}
	}

	return result, nil
}

// parseSections converts a comma-separated sections string into a lookup map.
func parseSections(s string) map[string]bool {
	m := map[string]bool{}
	for _, part := range splitComma(s) {
		m[part] = true
	}
	return m
}

func splitComma(s string) []string {
	var parts []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			part := trimSpace(s[start:i])
			if part != "" {
				parts = append(parts, part)
			}
			start = i + 1
		}
	}
	return parts
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// defaultOSInfo returns basic OS info using Go's runtime package.
// Used by all platform readers as a shared helper.
func defaultOSInfo() (*OSInfo, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	return &OSInfo{
		Hostname: hostname,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
	}, nil
}
