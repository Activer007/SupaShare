package cliapi

import (
	"errors"
	"io"
)

// Probe EOF without writing an over-limit byte to disk. Counts encrypted wire
// bytes separately from plaintext bytes and never performs overflow-prone +1.
type downloadReader struct {
	r         io.Reader
	remaining int64
}

func (r *downloadReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.remaining == 0 {
		var probe [1]byte
		n, err := r.r.Read(probe[:])
		if n > 0 {
			return 0, errors.New("download exceeded its byte budget")
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.r.Read(p)
	r.remaining -= int64(n)
	return n, err
}
