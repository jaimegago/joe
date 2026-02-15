# Phase 5.5 Step 2: Self-Protection Invariants - COMPLETE

**Status:** ✅ Complete  
**Date:** 2026-02-15

## Summary

Implemented hardcoded self-protection invariants that prevent Joe from modifying its own configuration, killing itself, or bypassing safety checks. These protections are enforced at compile-time and cannot be overridden by policy files or LLM reasoning.

## What Was Implemented

### 1. Self-Protection Invariants Module (`internal/safety/invariants.go`)

Created a new module that enforces two key protection categories:

#### Path Protection
- **Blocks:** Any file operation within `~/.joe/` directory
- **Includes:** config.yaml, joe.db, safety-policy.yaml, and any other files in that directory
- **Enforcement:** Called before all file read/write operations
- **Function:** `IsPathAllowed(absPath string) error`

#### Command Protection
- **Blocks:** 
  - `joe`, `joecored` (prevent self-modification/restart)
  - `kill`, `pkill`, `killall` (prevent process termination)
- **Enforcement:** Called before all command executions
- **Function:** `IsCommandAllowed(command string) error`

### 2. Tool Integration

Updated all local tools that access files or execute commands:

#### File Tools
- **read_file** (`internal/tools/local/readfile/readfile.go`)
  - Added `safety.IsPathAllowed()` check after path expansion
  - Returns error before attempting to read if path is blocked
  
- **write_file** (`internal/tools/local/writefile/writefile.go`)
  - Added `safety.IsPathAllowed()` check after path expansion
  - Returns error before creating directories or writing if path is blocked

#### Command Tool
- **run_command** (`internal/tools/local/runcmd/runcmd.go`)
  - Added `safety.IsCommandAllowed()` check before command allowlist verification
  - Self-protection check runs first, then allowlist check
  - Double-gated: both must pass for command to execute

#### Git Tools (Defense in Depth)
- **local_git_status** (`internal/tools/local/gitstatus/gitstatus.go`)
  - Added path check when repository path is specified
  - Prevents inspecting git repos in ~/.joe/

- **local_git_diff** (`internal/tools/local/gitdiff/gitdiff.go`)
  - Added path check when specific file path is provided
  - Prevents diffing files in ~/.joe/

### 3. Error Types

Created `InvariantViolationError` type:
- Distinguishable from policy-based `AccessDeniedError`
- Contains violation type, target, and human-readable reason
- Helper function `IsInvariantViolation(err error) bool`

### 4. Tests

Comprehensive test coverage:
- `internal/safety/invariants_test.go` (15 test cases)
- Updated tool tests to verify self-protection:
  - `readfile_test.go`: BlocksJoeDirectory, BlocksSafetyPolicy
  - `writefile_test.go`: BlocksJoeDirectory, BlocksSafetyPolicy
  - `runcmd_test.go`: BlocksJoeCommand, BlocksJoecoredCommand, BlocksKillCommand, BlocksPkillCommand, BlocksKillallCommand

All tests passing ✅

### 5. Documentation

- Created `safety-policy.example.yaml` with comprehensive comments
- Documents all self-protection invariants at the bottom of the file
- Explains that these are hardcoded and cannot be overridden

## Key Design Decisions

### 1. Defense in Depth
Self-protection checks are the **first** check in each tool's Execute() method:
```
1. Parse arguments
2. Expand/normalize paths
3. ✅ Self-protection check (IsPathAllowed / IsCommandAllowed)
4. Policy allowlist check
5. Business logic
```

This ordering ensures that even if policy checks are bypassed (bug, misconfiguration), self-protection invariants still apply.

### 2. Fail-Safe Defaults
- If `~/.joe/` path cannot be determined, **deny all operations** for safety
- Base command extracted from full paths (e.g., `/usr/bin/joe` → `joe`)
- Path comparisons use cleaned, absolute paths to prevent bypass via `..`, symlinks (future work)

### 3. Clear Error Messages
Errors explicitly mention "self-protection" to make it clear why the operation was blocked:
```
safety invariant violation (path_protection): /Users/user/.joe/config.yaml 
  - Joe cannot read or write files within ~/.joe/ (self-protection)
```

### 4. Not Configurable
Self-protection invariants are **hardcoded** in the binary. No configuration option can disable them. The safety policy file itself cannot be read or modified by Joe.

## What's NOT Included (Future Work)

These are mentioned in the security doc but not implemented in this step:

1. **Symlink Resolution** - Currently doesn't follow symlinks. A symlink pointing to ~/.joe/ would be blocked by path check, but a symlink FROM ~/.joe/ pointing out might not be caught.

2. **Path Traversal via `..`** - Need to add check for `..` segments in paths after normalization.

3. **Subcommand Validation** - `kubectl`, `helm`, `argocd` subcommands (apply, delete, etc.) are not yet filtered. This is Step 4 of Phase 5.5.

4. **T3 Notification Contract** - Self-protection blocks immediately without notifying the user first. The notification system is Step 6.

## Files Changed

### New Files
- `internal/safety/invariants.go` (109 lines)
- `internal/safety/invariants_test.go` (176 lines)
- `safety-policy.example.yaml` (55 lines)

### Modified Files
- `internal/tools/local/readfile/readfile.go` (+7 lines)
- `internal/tools/local/readfile/readfile_test.go` (+36 lines)
- `internal/tools/local/writefile/writefile.go` (+7 lines)
- `internal/tools/local/writefile/writefile_test.go` (+45 lines)
- `internal/tools/local/runcmd/runcmd.go` (+7 lines)
- `internal/tools/local/runcmd/runcmd_test.go` (+84 lines)
- `internal/tools/local/gitstatus/gitstatus.go` (+7 lines)
- `internal/tools/local/gitdiff/gitdiff.go` (+7 lines)

**Total:** 3 new files, 8 modified files, ~540 lines of new/changed code

## Testing

All tests pass:
```bash
go test ./internal/safety/...
ok      github.com/jaimegago/joe/internal/safety        0.204s

go test ./internal/tools/local/...
ok      github.com/jaimegago/joe/internal/tools/local/readfile  0.576s
ok      github.com/jaimegago/joe/internal/tools/local/writefile 0.883s
ok      github.com/jaimegago/joe/internal/tools/local/runcmd    0.729s
```

Build successful:
```bash
go build ./...
# Success (no errors)
```

## Next Steps

**Phase 5.5 Step 3:** Path sandboxing for read_file/write_file
- Add `allowed_directories` enforcement from safety policy
- Reject paths containing `..` after normalization
- Reject symlinks that resolve outside allowed directories
- Builds on self-protection foundation laid in Step 2

**Phase 5.5 Step 4:** run_command subcommand validation
- Split allowlist into read-only vs mutation-capable commands
- Add subcommand allowlist for kubectl, helm, argocd
- Default: only read-only subcommands permitted

## References

- `docs/security-in-layers.md` - Full security architecture
- `internal/safety/policy.go` - Policy types and loading (Step 1)
- `internal/safety/tier.go` - Action tier classification (Step 1)
