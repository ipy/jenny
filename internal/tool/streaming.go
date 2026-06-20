// Package tool provides streaming I/O utilities shared across tools.
package tool

import (
	"bytes"
	"io"
	"unicode"
)

// LimitedBuffer writes to an internal buffer up to maxBytes bytes.
// After the limit is reached, subsequent writes are accepted but
// not stored; ReceivedBytes still accumulates to reflect the total.
type LimitedBuffer struct {
	MaxBytes      int
	buf           bytes.Buffer
	ReceivedBytes int
}

// Write implements io.Writer. Data beyond MaxBytes is accepted but not stored.
func (b *LimitedBuffer) Write(p []byte) (n int, err error) {
	b.ReceivedBytes += len(p)
	remaining := b.MaxBytes - b.buf.Len()
	if remaining <= 0 {
		return len(p), nil
	}
	if len(p) <= remaining {
		return b.buf.Write(p)
	}
	_, err = b.buf.Write(p[:remaining])
	return len(p), err
}

// AtLimit reports whether any write has exceeded MaxBytes.
func (b *LimitedBuffer) AtLimit() bool {
	return b.ReceivedBytes > b.MaxBytes
}

// String returns the buffered content.
func (b *LimitedBuffer) String() string {
	return b.buf.String()
}

// containsBinary reports whether data contains binary content.
// A file is considered binary if more than 10% of bytes are non-printable
// non-space characters, or if it contains a null byte.
func containsBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	if bytes.Contains(data, []byte{0}) {
		return true
	}
	nonPrintable := 0
	for _, r := range string(data) {
		if !unicode.IsPrint(r) && !unicode.IsSpace(r) {
			nonPrintable++
		}
	}
	return float64(nonPrintable)/float64(len(data)) > 0.10
}

// PeekBOM peeks at the first few bytes of r to detect UTF-16 LE/BE BOM.
// Returns the detected encoding, a reader that yields the BOM-free stream,
// and the raw peeked bytes (which the caller should discard).
//
// The returned reader is a io.MultiReader combining the remainder of the
// peek buffer and the original r. The caller must use the returned reader
// and discard the original r.
func PeekBOM(r io.Reader) (encoding string, bomFree io.Reader, peekBytes int, err error) {
	peek := make([]byte, 4)
	n, err := io.ReadAtLeast(r, peek, 1)
	if err != nil {
		return "utf-8", bytes.NewReader(peek[:n]), n, err
	}

	switch {
	case n >= 2 && peek[0] == 0xFE && peek[1] == 0xFF:
		// UTF-16 BE BOM
		return "utf-16-be", io.MultiReader(bytes.NewReader(peek[2:n]), r), n, nil
	case n >= 2 && peek[0] == 0xFF && peek[1] == 0xFE:
		// UTF-16 LE BOM
		return "utf-16-le", io.MultiReader(bytes.NewReader(peek[2:n]), r), n, nil
	default:
		// UTF-8 or no BOM
		return "utf-8", io.MultiReader(bytes.NewReader(peek[:n]), r), n, nil
	}
}
