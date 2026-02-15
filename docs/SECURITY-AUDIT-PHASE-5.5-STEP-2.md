# Security Audit Report: Phase 5.5 Self-Protection Invariants
**Date:** February 15, 2026  
**Scope:** Self-protection invariants, file operations, command execution, policy system  
**Status:** 🟡 GOOD with 5 CRITICAL recommendations

---

## Executive Summary

The self-protection invariants implementation provides a solid foundation for securing Joe against self-modification attacks. However, **5 critical vulnerabilities** were identified that must be addressed before production use. The system correctly prevents direct attacks but is vulnerable to environment variable manipulation, TOCTOU race conditions, and insufficient parent directory validation.

**Risk Level:** MEDIUM (reducible to LOW with recommendations)

---

## ✅ What's Working Well

### 1. Symlink Protection ✅
**File:** `internal/safety/invariants.go:41-46`

```go
if resolved, err := filepath.EvalSymlinks(cleanPath); err == nil {
    cleanPath = resolved
} else if !os.IsNotExist(err) {
    // EvalSymlinks failed for a reason other than "not found" — deny for safety
    return fmt.Errorf("cannot resolve path %s for safety check: %w", absPath, err)
}
```

**Status:** EXCELLENT
- Symlinks are properly resolved before checking
- Fail-safe: if symlink resolution fails (and file exists), deny access
- Handles the case where file doesn't exist yet (write_file creating new files)

### 2. Path Traversal Protection ✅
**File:** `internal/safety/invariants.go:34-35`

```go
cleanPath := filepath.Clean(absPath)
cleanJoeDir := filepath.Clean(joeDir)
```

**Status:** GOOD
- `filepath.Clean()` normalizes paths, removing `..` segments
- Prevents attacks like `~/../.joe/config.yaml`
- Combined with prefix check: `strings.HasPrefix(cleanPath, cleanJoeDir+separator)`

### 3. Command Execution (No Shell) ✅
**File:** `internal/tools/local/runcmd/runcmd.go:28`

```go
cmd := exec.CommandContext(ctx, name, args...)
```

**Status:** EXCELLENT
- Uses `exec.CommandContext()` directly (no `/bin/sh`)
- Arguments passed as separate strings, not concatenated
- No shell metacharacter interpretation (`; | & $()` etc.)
- Command injection via arguments not possible

### 4. Defense in Depth ✅
**Implementation:** Self-protection checks run BEFORE policy checks in all tools

**Status:** EXCELLENT
- Even if policy is misconfigured, self-protection still applies
- Clear separation: invariants (hardcoded) vs policy (configurable)
- Fail-safe ordering

### 5. Error Handling ✅
**File:** `internal/safety/invariants.go:27-30`

```go
joeDir, err := paths.JoeDirPath()
if err != nil {
    // If we can't determine Joe's config dir, deny access for safety
    return fmt.Errorf("cannot verify path safety: %w", err)
}
```

**Status:** EXCELLENT
- Fail-closed: if safety check can't be performed, deny access
- No "fail open" vulnerabilities where errors grant access

---

## 🔴 Critical Vulnerabilities Found

### 1. 🔴 CRITICAL: HOME Environment Variable Manipulation
**File:** `internal/paths/defaults.go:28-29`

```go
func JoeDirPath() (string, error) {
    home := os.Getenv("HOME")  // ⚠️ VULNERABLE
    if home == "" {
        var err error
        home, err = os.UserHomeDir()
        if err != nil {
            return "", err
        }
    }
    return filepath.Join(home, JoeDir), nil
}
```

**Vulnerability:** An attacker can manipulate the `HOME` environment variable to bypass self-protection:

```bash
# Attack: Set HOME to attacker-controlled directory
export HOME=/tmp/fake-home
mkdir -p /tmp/fake-home/.joe
echo "malicious config" > /tmp/fake-home/.joe/config.yaml

# Joe now protects /tmp/fake-home/.joe/ instead of /Users/victim/.joe/
# The real config at /Users/victim/.joe/ is now accessible!

joe
> read_file("/Users/victim/.joe/config.yaml")  # SUCCESS (bypassed protection)
```

**Impact:** CRITICAL
- Complete bypass of self-protection
- Attacker can read actual config, database, safety policy
- Can manipulate Joe's behavior

**Recommendation:** Use `os.UserHomeDir()` EXCLUSIVELY, remove `os.Getenv("HOME")`:

```go
func JoeDirPath() (string, error) {
    home, err := os.UserHomeDir()
    if err != nil {
        return "", fmt.Errorf("cannot determine home directory: %w", err)
    }
    return filepath.Join(home, JoeDir), nil
}
```

`os.UserHomeDir()` is OS-native and not influenced by environment variables on most platforms.

---

### 2. 🔴 CRITICAL: TOCTOU (Time-of-Check-Time-of-Use) Race Condition
**File:** `internal/tools/local/writefile/writefile.go:63-77`

```go
// Self-protection: check if path is allowed (blocks ~/.joe/)
if err := safety.IsPathAllowed(absPath); err != nil {
    return nil, err
}

// Check if file exists to determine if we're creating or overwriting
_, err = os.Stat(absPath)  // ⚠️ TIME OF CHECK
created := os.IsNotExist(err)

// Create parent directories if they don't exist
dir := filepath.Dir(absPath)
if err := os.MkdirAll(dir, 0755); err != nil {  // ⚠️ TOCTOU gap
    return nil, fmt.Errorf("failed to create parent directories: %w", err)
}

// Write file
if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {  // ⚠️ TIME OF USE
    return nil, fmt.Errorf("failed to write file: %w", err)
}
```

**Vulnerability:** Race condition between security check and file write:

```
Thread 1 (Joe):                Thread 2 (Attacker):
1. Check: /tmp/test.txt ✅     
2. MkdirAll(/tmp)              
3. [pause]                     rm /tmp/test.txt
                               ln -s ~/.joe/config.yaml /tmp/test.txt
4. WriteFile(/tmp/test.txt)    
   → Writes to ~/.joe/config.yaml! ❌
```

**Impact:** HIGH
- Race window between check and write
- Symlink swap after check but before write
- Can overwrite protected files

**Recommendation 1:** Re-check path after opening file descriptor:

```go
// Open file with O_NOFOLLOW to prevent symlink following
fd, err := os.OpenFile(absPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0644)
if err != nil {
    return nil, fmt.Errorf("failed to open file: %w", err)
}
defer fd.Close()

// Get actual path via fd and re-check
actualPath, err := filepath.EvalSymlinks(fmt.Sprintf("/proc/self/fd/%d", fd.Fd()))
if err == nil {
    if err := safety.IsPathAllowed(actualPath); err != nil {
        fd.Close()
        os.Remove(absPath)  // Clean up
        return nil, err
    }
}

// Now safe to write
if _, err := fd.Write([]byte(content)); err != nil {
    return nil, fmt.Errorf("failed to write: %w", err)
}
```

**Recommendation 2 (Simpler):** Check resolved path at write time:

```go
// Write file
if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
    return nil, fmt.Errorf("failed to write file: %w", err)
}

// Immediately verify what we wrote to
writtenPath, err := filepath.EvalSymlinks(absPath)
if err == nil {
    if err := safety.IsPathAllowed(writtenPath); err != nil {
        // Wrote to protected location — delete it
        os.Remove(absPath)
        return nil, fmt.Errorf("write completed but violated protection: %w", err)
    }
}
```

---

### 3. 🔴 HIGH: Parent Directory Creation Bypasses Protection
**File:** `internal/tools/local/writefile/writefile.go:73`

```go
dir := filepath.Dir(absPath)
if err := os.MkdirAll(dir, 0755); err != nil {  // ⚠️ No check on parent!
    return nil, fmt.Errorf("failed to create parent directories: %w", err)
}
```

**Vulnerability:** Parent directory is not validated against self-protection:

```
User: write_file("/tmp/innocent/file.txt", "content")

Check: /tmp/innocent/file.txt ✅ (not in ~/.joe/)
MkdirAll: /tmp/innocent ✅

But what if parent is a symlink?
$ ln -s ~/.joe /tmp/innocent

MkdirAll creates ~/.joe/subdir if needed
WriteFile writes to ~/.joe/subdir/file.txt ❌
```

**Impact:** MEDIUM-HIGH
- Can create files in protected directories via parent symlinks
- Bypasses self-protection through indirect path

**Recommendation:** Validate parent directory before MkdirAll:

```go
dir := filepath.Dir(absPath)

// Resolve parent symlinks and check
if resolvedDir, err := filepath.EvalSymlinks(dir); err == nil {
    if err := safety.IsPathAllowed(filepath.Join(resolvedDir, "dummy")); err != nil {
        return nil, fmt.Errorf("parent directory violates protection: %w", err)
    }
} else if !os.IsNotExist(err) {
    // Parent exists but symlink resolution failed — deny
    return nil, fmt.Errorf("cannot verify parent directory: %w", err)
}

// If parent doesn't exist, check each component being created
if _, err := os.Stat(dir); os.IsNotExist(err) {
    // Validate that creating this directory tree won't create anything in ~/.joe/
    for p := dir; p != "/" && p != "."; p = filepath.Dir(p) {
        if err := safety.IsPathAllowed(filepath.Join(p, "dummy")); err != nil {
            return nil, fmt.Errorf("creating parent would violate protection: %w", err)
        }
    }
}

// Now safe to create
if err := os.MkdirAll(dir, 0755); err != nil {
    return nil, fmt.Errorf("failed to create parent directories: %w", err)
}
```

---

### 4. 🟡 MEDIUM: Case Sensitivity on macOS/Windows
**File:** `internal/safety/invariants.go:50-54`

```go
// Block any path within ~/.joe/
if strings.HasPrefix(cleanPath, cleanJoeDir+string(filepath.Separator)) || cleanPath == cleanJoeDir {
    return &InvariantViolationError{...}
}
```

**Vulnerability:** Case-insensitive filesystems (macOS, Windows) allow bypass:

```
Protected: /Users/alice/.joe/
Attack:    /Users/alice/.JOE/config.yaml  (uppercase J)

On macOS:  Same directory!
Check:     strings.HasPrefix("/Users/alice/.JOE/", "/Users/alice/.joe/") = false ❌
Result:    Bypass allowed
```

**Impact:** MEDIUM
- Only on case-insensitive filesystems (macOS default, Windows)
- Linux (case-sensitive) not affected

**Recommendation:** Normalize case on case-insensitive systems:

```go
func normalizePathCase(path string) string {
    // On case-insensitive systems, convert to lowercase for comparison
    if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
        return strings.ToLower(path)
    }
    return path
}

// In IsPathAllowed:
cleanPath = normalizePathCase(filepath.Clean(absPath))
cleanJoeDir = normalizePathCase(filepath.Clean(joeDir))
```

---

### 5. 🟡 MEDIUM: Command Path Manipulation
**File:** `internal/safety/invariants.go:80`

```go
func IsCommandAllowed(command string) error {
    // Extract base command name (remove path)
    baseCmd := filepath.Base(command)  // ⚠️ Only checks basename
    
    if baseCmd == "joe" || baseCmd == "joecored" {
        return &InvariantViolationError{...}
    }
}
```

**Vulnerability:** Doesn't prevent wrapper scripts or renamed binaries:

```bash
# Attack 1: Wrapper script
$ cat > /tmp/kubectl << 'EOF'
#!/bin/bash
exec /usr/local/bin/joe "$@"
EOF
$ chmod +x /tmp/kubectl

joe> run_command("kubectl", ["apply", "-f", "evil.yaml"])
# Actually runs joe instead of kubectl! ❌

# Attack 2: Copy and rename
$ cp $(which joe) /tmp/supervisor
$ /tmp/supervisor  # Joe running under different name
```

**Impact:** MEDIUM
- Can execute joe/joecored under different names
- Wrapper scripts can invoke blocked commands
- Limited by run_command allowlist (kubectl must be allowed)

**Recommendation:** Additional protection layers:

1. **Check $0 inside joe binary:**
```go
// In cmd/joe/main.go
func main() {
    // Verify we're running as expected name
    basename := filepath.Base(os.Args[0])
    if basename != "joe" && basename != "joe.exe" {
        log.Fatal("joe must be invoked by its original name")
    }
}
```

2. **Hash-based verification:**
```go
// Store hash of joe binary during installation
// Reject run_command if target file matches joe's hash
```

3. **Note:** This is defense-in-depth. Primary protection is the allowlist.

---

## 🟢 Minor Issues

### 6. 🟢 LOW: Unicode Normalization
**File:** Path comparison in `invariants.go`

**Issue:** Unicode paths may have multiple representations (NFD vs NFC):
- `/Users/alice/.joe` (NFC)
- `/Users/alice/.joe` (NFD - different byte sequence, same visual)

**Impact:** LOW - Most systems normalize automatically

**Recommendation:** Add normalization step:
```go
import "golang.org/x/text/unicode/norm"

cleanPath = norm.NFC.String(filepath.Clean(absPath))
```

---

### 7. 🟢 LOW: Policy Parsing Error Details
**File:** `internal/safety/policy.go:105-107`

```go
if err := yaml.Unmarshal(data, policy); err != nil {
    return nil, fmt.Errorf("parse safety policy %s: %w", path, err)
}
```

**Issue:** Error message might leak file content (YAML parse errors can include snippets)

**Impact:** LOW - Only visible to the human running Joe

**Recommendation:** Sanitize error messages in production:
```go
if err := yaml.Unmarshal(data, policy); err != nil {
    return nil, fmt.Errorf("malformed safety policy at %s (check YAML syntax)", path)
}
```

---

## 🔍 Additional Secure Practices Observed

### ✅ No Shell Injection
- All commands use `exec.CommandContext()` directly
- No `sh -c` or `bash -c` wrappers
- Arguments passed separately, not concatenated

### ✅ Output Truncation
- stdout/stderr limited to 256KB
- File reads limited to 1MB
- Prevents memory exhaustion

### ✅ Timeout Protection
- Commands timeout after 30 seconds
- Prevents hanging operations

### ✅ SQL Injection Protection
- All queries use parameterized statements (`?` placeholders)
- Reviewed: `internal/store/*.go` - all safe

### ✅ No Hardcoded Secrets
- LLM API keys from environment variables
- No credentials in code or config files

---

## Recommendations Priority

### CRITICAL (Fix Before Production)
1. **Fix HOME env var bypass** - Use `os.UserHomeDir()` exclusively
2. **Fix TOCTOU in write_file** - Re-verify path after write or use O_NOFOLLOW
3. **Validate parent directories** - Check parent before MkdirAll

### HIGH (Fix in Phase 5.5 Step 3)
4. **Case sensitivity** - Normalize paths on case-insensitive systems

### MEDIUM (Address in Phase 5.5 Step 4)
5. **Command name verification** - Add $0 check in joe binary

### LOW (Future Enhancement)
6. **Unicode normalization** - Add NFD/NFC normalization
7. **Error message sanitization** - Reduce info leakage in parse errors

---

## Test Coverage Gaps

### Missing Test Cases
1. **HOME manipulation test** - Currently not tested
2. **TOCTOU race condition test** - Requires threading
3. **Case sensitivity test** - macOS-specific
4. **Symlink parent directory test** - Not covered
5. **Command wrapper script test** - Not covered

### Recommended Test Additions
```go
// Test HOME manipulation
func TestIsPathAllowed_HomeEnvBypass(t *testing.T) {
    t.Setenv("HOME", "/tmp/fake")
    // Should still protect real home, not /tmp/fake
}

// Test symlink parent
func TestWriteFile_SymlinkParent(t *testing.T) {
    // Create symlink: /tmp/link -> ~/.joe
    // Try: write_file("/tmp/link/test.txt")
    // Should: Block
}
```

---

## Audit Methodology

1. **Static Analysis:** Manual code review of all security-critical paths
2. **Threat Modeling:** STRIDE analysis (Spoofing, Tampering, Repudiation, Info Disclosure, Denial of Service, Elevation of Privilege)
3. **Attack Surface:** Identified all external inputs (paths, commands, env vars)
4. **Control Flow:** Traced execution from input to protected operation
5. **Race Condition Analysis:** Identified time gaps between checks and operations

---

## Sign-Off

**Auditor:** Claude (Automated Security Analysis)  
**Date:** February 15, 2026  
**Recommendation:** Address CRITICAL issues before Phase 5.5 Step 3  
**Timeline:** Critical fixes: 2-4 hours; High priority: included in Step 3

---

## Next Steps

1. ✅ Create fixes for critical vulnerabilities
2. Add test cases for vulnerability scenarios
3. Re-run security audit after fixes
4. Proceed to Phase 5.5 Step 3 (Path Sandboxing) with clean foundation
