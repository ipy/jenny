# Log Package Technical Debt Fixes

**Iteration:** 12  
**Status:** Implemented

## Overview

Three technical debt items in `internal/log/` were addressed:

1. **Dead code removal** - Remove unused `newRingBuffer()` function
2. **Deep-copy slices** - `GetLastRequest()` returns deep-copied slices to prevent external mutation
3. **Parallel-safe tests** - Tests use `ResetForTest()` + `t.Cleanup()` instead of direct global mutation

## AC1: Dead `newRingBuffer` Removed

The function `newRingBuffer(capacity int) *ringBuffer` was deleted. The composite literal `ringBuffer{capacity: 100}` at line 105 already works correctly.

**Verification:**
```bash
go build ./internal/log/
go test ./internal/log/ -count=1
```

## AC2: `GetLastRequest` Returns Deep-Copied Slices

The `GetLastRequest()` function now deep-copies the `Tools` and `Messages` slices to prevent callers from mutating the store's internal state.

**Implementation:**
```go
func GetLastRequest() *LastRequest {
    lastRequestMu.RLock()
    defer lastRequestMu.RUnlock()
    if lastRequestStore == nil {
        return nil
    }
    result := *lastRequestStore
    if result.Tools != nil {
        result.Tools = append([]any(nil), result.Tools...)
    }
    if result.Messages != nil {
        result.Messages = append([]any(nil), result.Messages...)
    }
    return &result
}
```

**Test:** `TestLastRequest_DeepCopySlices` verifies that mutations to returned slice elements do not affect the store.

## AC3: Tests Use `ResetForTest()` + `t.Cleanup()`

A package-level `ResetForTest()` function was added to safely reset global state:

```go
func ResetForTest() {
    errorRing.mu.Lock()
    errorRing.entries = nil
    errorRing.capacity = 100
    errorRing.mu.Unlock()
    lastRequestMu.Lock()
    lastRequestStore = nil
    lastRequestMu.Unlock()
}
```

All tests now use `ResetForTest()` with `t.Cleanup(ResetForTest)` instead of directly assigning to `errorRing` or `lastRequestStore`.

**Verification:**
```bash
grep -E 'errorRing =|lastRequestStore =' internal/log/log_test.go
# Returns: (no matches)
```

## AC4: All Tests Pass

All existing tests pass:
- `TestRingBuffer_CapsAt100Entries`
- `TestRingBuffer_FIFOEviction`
- `TestRingBuffer_EmptyBuffer`
- `TestLastRequest_SetAndGet`
- `TestLastRequest_MessagesNilByDefault`
- `TestLastRequest_OverwriteEachTurn`
- `TestLastRequest_NilWhenEmpty`
- `TestLastRequest_ImmutableFromGetter`
- `TestLastRequest_DeepCopySlices` (new)

```bash
go test ./internal/log/ -count=1 -v
# Result: ok
```
