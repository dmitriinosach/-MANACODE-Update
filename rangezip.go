package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const rangeChunk = 128 << 10

type rangeReader struct {
	url    string
	size   int64
	client *http.Client
	off    int64
	buf    []byte
}

func newRangeReader(url string, size int64) *rangeReader {
	return &rangeReader{url: url, size: size, client: &http.Client{Timeout: 30 * time.Second}}
}

func (r *rangeReader) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= r.size {
		return 0, io.EOF
	}
	n := 0
	for n < len(p) && off+int64(n) < r.size {
		at := off + int64(n)
		if at < r.off || at >= r.off+int64(len(r.buf)) {
			if err := r.fill(at, int64(len(p)-n)); err != nil {
				return n, err
			}
		}
		n += copy(p[n:], r.buf[at-r.off:])
	}
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (r *rangeReader) fill(at, want int64) error {
	end := min(at+max(want, rangeChunk), r.size)
	if tail := r.size - rangeChunk; end == r.size && tail < at && tail >= 0 {
		at = tail
	}
	req, err := http.NewRequest(http.MethodGet, r.url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", at, end-1))
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return errors.New("сервер не отдаёт архив по частям")
	}
	buf, err := io.ReadAll(io.LimitReader(resp.Body, end-at))
	if err != nil {
		return err
	}
	if int64(len(buf)) != end-at {
		return io.ErrUnexpectedEOF
	}
	r.off, r.buf = at, buf
	return nil
}
