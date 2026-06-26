package session

import (
	"context"
	"io"
	"os"
	"time"
)

// tailReader streams a file's bytes, optionally following appends (tail -f).
// Closing it (or cancelling ctx) ends the stream.
type tailReader struct {
	f      *os.File
	follow bool
	ctx    context.Context
	cancel context.CancelFunc
}

func newTailReader(ctx context.Context, path string, follow bool) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithCancel(ctx)
	return &tailReader{f: f, follow: follow, ctx: cctx, cancel: cancel}, nil
}

func (t *tailReader) Read(p []byte) (int, error) {
	for {
		n, err := t.f.Read(p)
		if n > 0 {
			return n, nil
		}
		if err == io.EOF {
			if !t.follow {
				return 0, io.EOF
			}
			select {
			case <-t.ctx.Done():
				return 0, io.EOF
			case <-time.After(250 * time.Millisecond):
				continue
			}
		}
		if err != nil {
			return 0, err
		}
	}
}

func (t *tailReader) Close() error {
	t.cancel()
	return t.f.Close()
}
