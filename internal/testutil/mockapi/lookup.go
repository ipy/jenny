package mockapi

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Lookup resolves a cassette ID to an absolute filesystem path.
//
// Behavior:
//   - Resolves cassetteID against the testdata/ directory co-located with this
//     source file (determined via runtime.Caller at package init).
//   - Checks for cassetteID+".sse" first, then cassetteID+".json".
//   - Returns the absolute path of the existing file.
//   - Returns an error if neither extension exists.
//
// This function is safe to call from any working directory because
// runtime.Caller(0) locates lookup.go's source directory at runtime.
func Lookup(cassetteID string) (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("Lookup: cannot determine package path")
	}
	testdataDir := filepath.Join(filepath.Dir(filename), "testdata")

	// Check .sse first, then .json
	ssePath := filepath.Join(testdataDir, cassetteID+".sse")
	if _, err := os.Stat(ssePath); err == nil {
		return ssePath, nil
	}
	jsonPath := filepath.Join(testdataDir, cassetteID+".json")
	if _, err := os.Stat(jsonPath); err == nil {
		return jsonPath, nil
	}
	return "", fmt.Errorf("cassette %q not found (tried .sse and .json in %s)", cassetteID, testdataDir)
}
