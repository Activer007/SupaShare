package uploadreceive

import (
	"errors"
	"io"
	"net"
	"net/http"
	"time"
)

// progressReader uses the HTTP transport's deadline, not a detached goroutine
// around Body.Read. Small trickles cannot renew the budget: each window requires
// a full quantum of wire bytes, including multipart headers and epilogues.
// This is a reception-only limit; disk/processing work has its own boundaries.
type progressReader struct {
	body               io.ReadCloser
	controller         *http.ResponseController
	window             time.Duration
	quantum, progress  int64
	deadline, absolute time.Time
	supported          bool
	timedOut           bool
}

func newProgressReader(w http.ResponseWriter, r *http.Request, window time.Duration, quantum int64, maximum time.Duration) (*progressReader, error) {
	if window <= 0 || quantum <= 0 || maximum <= 0 {
		return nil, ErrInvalid
	}
	now := time.Now()
	p := &progressReader{body: r.Body, controller: http.NewResponseController(w), window: window, quantum: quantum, absolute: now.Add(maximum)}
	if d, ok := r.Context().Deadline(); ok && d.Before(p.absolute) {
		p.absolute = d
	}
	p.deadline = minTime(now.Add(window), p.absolute)
	err := p.controller.SetReadDeadline(p.deadline)
	// Like requestbudget.Management, finite non-network recorders can be used in
	// unit tests. Production HTTP/1 and HTTP/2 writers support read deadlines;
	// response wrappers must expose Unwrap so ResponseController can reach them.
	if err != nil && !errors.Is(err, http.ErrNotSupported) {
		return nil, ErrInvalid
	}
	p.supported = err == nil
	return p, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (p *progressReader) Read(b []byte) (int, error) {
	if !time.Now().Before(p.deadline) {
		p.timedOut = true
		return 0, ErrTimeout
	}
	n, err := p.body.Read(b)
	now := time.Now()
	var timeout net.Error
	if !now.Before(p.deadline) || (errors.As(err, &timeout) && timeout.Timeout()) {
		p.timedOut = true
		return n, ErrTimeout
	}
	if n > 0 {
		p.progress += int64(n)
		if p.progress >= p.quantum {
			p.progress = 0
			p.deadline = minTime(now.Add(p.window), p.absolute)
			if p.supported {
				if deadlineErr := p.controller.SetReadDeadline(p.deadline); deadlineErr != nil {
					return n, ErrInvalid
				}
			}
		}
	}
	return n, err
}
func (p *progressReader) Close() error { return p.body.Close() }
func (p *progressReader) finish(success bool) {
	if !p.supported {
		return
	}
	if success {
		_ = p.controller.SetReadDeadline(time.Time{})
	} else {
		// Do not let net/http drain a rejected incomplete body while holding the
		// connection. The caller also sets Connection: close for HTTP/1.
		_ = p.controller.SetReadDeadline(time.Now())
	}
}
