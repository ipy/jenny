package utils

// ReadDirBatchSize is the batch size for os.ReadDir to prevent OOM on large directories.
const ReadDirBatchSize = 128

// ContainsPathTraversal checks if a string contains path traversal components.
func ContainsPathTraversal(s string) bool {
	if s == "" {
		return false
	}
	if s[0] == '/' || s[0] == '\\' {
		return true
	}
	if len(s) >= 2 && s[0] == '.' && (s[1] == '/' || s[1] == '\\' || (s[1] == '.' && (len(s) < 3 || s[2] == '/' || s[2] == '\\'))) {
		return true
	}
	for i := range len(s) - 1 {
		if s[i] == '.' && s[i+1] == '.' && (i+2 < len(s) && (s[i+2] == '/' || s[i+2] == '\\')) {
			return true
		}
	}
	return false
}
