package sysinfo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/tools/shared/sysinfo"
)

// mockSystemReader returns pre-canned responses for all sections.
type mockSystemReader struct {
	disk    *sysinfo.DiskInfo
	diskErr error
	mem     *sysinfo.MemoryInfo
	memErr  error
	load    *sysinfo.LoadInfo
	loadErr error
	osInfo  *sysinfo.OSInfo
	osErr   error
}

func (m *mockSystemReader) DiskInfo(_ string) (*sysinfo.DiskInfo, error) {
	return m.disk, m.diskErr
}

func (m *mockSystemReader) MemoryInfo() (*sysinfo.MemoryInfo, error) {
	return m.mem, m.memErr
}

func (m *mockSystemReader) LoadInfo() (*sysinfo.LoadInfo, error) {
	return m.load, m.loadErr
}

func (m *mockSystemReader) OSInfo() (*sysinfo.OSInfo, error) {
	return m.osInfo, m.osErr
}

func fullMock() *mockSystemReader {
	return &mockSystemReader{
		disk: &sysinfo.DiskInfo{
			Path:       "/",
			TotalBytes: 100 * 1024 * 1024 * 1024,
			FreeBytes:  40 * 1024 * 1024 * 1024,
			UsedBytes:  60 * 1024 * 1024 * 1024,
			UsedPct:    60.0,
		},
		mem: &sysinfo.MemoryInfo{
			TotalBytes:     16 * 1024 * 1024 * 1024,
			AvailableBytes: 8 * 1024 * 1024 * 1024,
			UsedBytes:      8 * 1024 * 1024 * 1024,
			UsedPct:        50.0,
		},
		load:   &sysinfo.LoadInfo{Load1: 0.5, Load5: 0.8, Load15: 1.2},
		osInfo: &sysinfo.OSInfo{Hostname: "myhost", OS: "linux", Arch: "amd64"},
	}
}

func TestSystemInfoTool_Name(t *testing.T) {
	tool := sysinfo.NewSystemInfoTool()
	if tool.Name() != "system_info" {
		t.Errorf("Name() = %q, want system_info", tool.Name())
	}
}

func TestSystemInfoTool_Description(t *testing.T) {
	tool := sysinfo.NewSystemInfoTool()
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestSystemInfoTool_Parameters(t *testing.T) {
	tool := sysinfo.NewSystemInfoTool()
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", params.Type)
	}
	if _, ok := params.Properties["sections"]; !ok {
		t.Error("Parameters() missing 'sections'")
	}
}

func TestSystemInfoTool_Execute_All(t *testing.T) {
	tool := &sysinfo.SystemInfoTool{Reader: fullMock()}

	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(sysinfo.SystemInfoResult)
	if r.Disk == nil {
		t.Error("Disk should not be nil for 'all'")
	}
	if r.Memory == nil {
		t.Error("Memory should not be nil for 'all'")
	}
	if r.Load == nil {
		t.Error("Load should not be nil for 'all'")
	}
	if r.OS == nil {
		t.Error("OS should not be nil for 'all'")
	}
}

func TestSystemInfoTool_Execute_DiskOnly(t *testing.T) {
	tool := &sysinfo.SystemInfoTool{Reader: fullMock()}

	result, err := tool.Execute(context.Background(), map[string]any{
		"sections": "disk",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(sysinfo.SystemInfoResult)
	if r.Disk == nil {
		t.Error("Disk should not be nil")
	}
	if r.Memory != nil {
		t.Error("Memory should be nil for disk-only request")
	}
	if r.Load != nil {
		t.Error("Load should be nil for disk-only request")
	}
	if r.OS != nil {
		t.Error("OS should be nil for disk-only request")
	}
}

func TestSystemInfoTool_Execute_MemoryOnly(t *testing.T) {
	tool := &sysinfo.SystemInfoTool{Reader: fullMock()}

	result, err := tool.Execute(context.Background(), map[string]any{
		"sections": "memory",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(sysinfo.SystemInfoResult)
	if r.Memory == nil {
		t.Error("Memory should not be nil")
	}
	if r.Disk != nil {
		t.Error("Disk should be nil for memory-only request")
	}
}

func TestSystemInfoTool_Execute_LoadOnly(t *testing.T) {
	tool := &sysinfo.SystemInfoTool{Reader: fullMock()}

	result, err := tool.Execute(context.Background(), map[string]any{
		"sections": "load",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(sysinfo.SystemInfoResult)
	if r.Load == nil {
		t.Error("Load should not be nil")
	}
	if r.Load.Load1 != 0.5 {
		t.Errorf("Load1 = %f, want 0.5", r.Load.Load1)
	}
}

func TestSystemInfoTool_Execute_OSOnly(t *testing.T) {
	tool := &sysinfo.SystemInfoTool{Reader: fullMock()}

	result, err := tool.Execute(context.Background(), map[string]any{
		"sections": "os",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(sysinfo.SystemInfoResult)
	if r.OS == nil {
		t.Error("OS should not be nil")
	}
	if r.OS.Hostname != "myhost" {
		t.Errorf("Hostname = %q, want myhost", r.OS.Hostname)
	}
}

func TestSystemInfoTool_Execute_DiskError(t *testing.T) {
	mock := fullMock()
	mock.diskErr = errors.New("permission denied")
	tool := &sysinfo.SystemInfoTool{Reader: mock}

	result, err := tool.Execute(context.Background(), map[string]any{
		"sections": "disk",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v (errors should be in result)", err)
	}

	r := result.(sysinfo.SystemInfoResult)
	if len(r.Errors) == 0 {
		t.Error("Errors should not be empty when disk read fails")
	}
	if r.Disk != nil {
		t.Error("Disk should be nil when read fails")
	}
}

func TestSystemInfoTool_Execute_PartialErrors(t *testing.T) {
	mock := fullMock()
	mock.loadErr = errors.New("unsupported")
	mock.memErr = errors.New("not available")
	tool := &sysinfo.SystemInfoTool{Reader: mock}

	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(sysinfo.SystemInfoResult)
	// Disk and OS should still succeed.
	if r.Disk == nil {
		t.Error("Disk should succeed even when load/memory fail")
	}
	if r.OS == nil {
		t.Error("OS should succeed even when load/memory fail")
	}
	if len(r.Errors) < 2 {
		t.Errorf("Errors = %d, want >= 2", len(r.Errors))
	}
}

func TestSystemInfoTool_Execute_CustomDiskPath(t *testing.T) {
	mock := fullMock()
	mock.disk = &sysinfo.DiskInfo{Path: "/data", TotalBytes: 500e9, UsedPct: 30.0}
	tool := &sysinfo.SystemInfoTool{Reader: mock}

	result, err := tool.Execute(context.Background(), map[string]any{
		"sections":  "disk",
		"disk_path": "/data",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(sysinfo.SystemInfoResult)
	if r.Disk.Path != "/data" {
		t.Errorf("Disk.Path = %q, want /data", r.Disk.Path)
	}
}

// TestSystemInfoTool_RealReader_OS uses the real OS reader to cover defaultOSInfo
// and the platform-specific reader. Only the "os" section is requested since
// disk/memory/load may not be relevant in all CI environments.
func TestSystemInfoTool_RealReader_OS(t *testing.T) {
	tool := sysinfo.NewSystemInfoTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"sections": "os",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	r := result.(sysinfo.SystemInfoResult)
	if r.OS == nil {
		t.Fatal("OS should not be nil for real reader")
	}
	if r.OS.OS == "" {
		t.Error("OS.OS should not be empty")
	}
	if r.OS.Arch == "" {
		t.Error("OS.Arch should not be empty")
	}
}

// TestSystemInfoTool_RealReader_Disk uses the real OS reader for disk stats
// on the root filesystem, which should always succeed on Linux/Darwin.
func TestSystemInfoTool_RealReader_Disk(t *testing.T) {
	tool := sysinfo.NewSystemInfoTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"sections":  "disk",
		"disk_path": "/",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	r := result.(sysinfo.SystemInfoResult)
	if r.Disk == nil {
		// On unsupported platforms the error goes into r.Errors; that's fine.
		if len(r.Errors) > 0 {
			t.Logf("disk not available on this platform: %v", r.Errors)
			return
		}
		t.Fatal("Disk should not be nil on supported platforms")
	}
	if r.Disk.TotalBytes == 0 {
		t.Error("TotalBytes should be > 0 for root filesystem")
	}
}

// TestSystemInfoTool_RealReader_Memory exercises the platform-specific
// MemoryInfo implementation (sysctl on Darwin, /proc/meminfo on Linux).
func TestSystemInfoTool_RealReader_Memory(t *testing.T) {
	tool := sysinfo.NewSystemInfoTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"sections": "memory",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	r := result.(sysinfo.SystemInfoResult)
	if r.Memory == nil {
		if len(r.Errors) > 0 {
			t.Logf("memory not available on this platform: %v", r.Errors)
			return
		}
		t.Fatal("Memory should not be nil on supported platforms")
	}
	if r.Memory.TotalBytes == 0 {
		t.Error("TotalBytes should be > 0")
	}
}

// TestSystemInfoTool_RealReader_Load exercises the platform-specific
// LoadInfo implementation (sysctl on Darwin, /proc/loadavg on Linux).
func TestSystemInfoTool_RealReader_Load(t *testing.T) {
	tool := sysinfo.NewSystemInfoTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"sections": "load",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	r := result.(sysinfo.SystemInfoResult)
	if r.Load == nil {
		if len(r.Errors) > 0 {
			t.Logf("load not available on this platform: %v", r.Errors)
			return
		}
		t.Fatal("Load should not be nil on supported platforms")
	}
	// Load averages should be non-negative.
	if r.Load.Load1 < 0 {
		t.Errorf("Load1 = %f, should be >= 0", r.Load.Load1)
	}
}

// TestSystemInfoTool_Execute_SectionsWithSpaces verifies that section names
// with surrounding whitespace are correctly parsed (covers trimSpace branches).
func TestSystemInfoTool_Execute_SectionsWithSpaces(t *testing.T) {
	tool := &sysinfo.SystemInfoTool{Reader: fullMock()}

	result, err := tool.Execute(context.Background(), map[string]any{
		"sections": " disk , os ",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	r := result.(sysinfo.SystemInfoResult)
	if r.Disk == nil {
		t.Error("Disk should not be nil — space-padded section name should be trimmed")
	}
	if r.OS == nil {
		t.Error("OS should not be nil — space-padded section name should be trimmed")
	}
}

func TestSystemInfoTool_Execute_MultipleSections(t *testing.T) {
	tool := &sysinfo.SystemInfoTool{Reader: fullMock()}

	result, err := tool.Execute(context.Background(), map[string]any{
		"sections": "disk,os",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(sysinfo.SystemInfoResult)
	if r.Disk == nil {
		t.Error("Disk should not be nil")
	}
	if r.OS == nil {
		t.Error("OS should not be nil")
	}
	if r.Memory != nil {
		t.Error("Memory should be nil for disk,os request")
	}
}
