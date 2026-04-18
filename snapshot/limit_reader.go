package snapshot

import (
	"errors"
	"io"
)

// limitReader wraps an io.Reader with a byte-count cap. When the cap is
// reached, Read returns (0, errHeaderLimitExceeded) and sets exceeded to
// true. The sentinel error distinguishes "input exceeded the cap" (the
// malformed-or-malicious case HeaderOnlyRead needs to surface) from "input
// legitimately ended" (io.EOF from the underlying reader), which standard
// io.LimitReader cannot express.
type limitReader struct {
	r        io.Reader
	n        int64
	exceeded bool
}

// newLimitReader constructs a limitReader that will return at most n bytes
// from r before returning errHeaderLimitExceeded.
func newLimitReader(r io.Reader, n int64) *limitReader {
	return &limitReader{r: r, n: n}
}

// Read implements io.Reader. Returns (0, errHeaderLimitExceeded) once the
// byte cap is reached; natural reader errors (io.EOF, io.ErrUnexpectedEOF,
// arbitrary I/O errors) propagate unchanged and do NOT set exceeded.
func (lr *limitReader) Read(p []byte) (int, error) {
	if lr.n <= 0 {
		lr.exceeded = true
		return 0, errHeaderLimitExceeded
	}
	if int64(len(p)) > lr.n {
		p = p[:lr.n]
	}
	n, err := lr.r.Read(p)
	lr.n -= int64(n)
	return n, err //nolint:wrapcheck // io.Reader pass-through: callers depend on unwrapped io.EOF and io.ErrUnexpectedEOF sentinels.
}

var errHeaderLimitExceeded = errors.New("header size exceeded MaxHeaderSize")
