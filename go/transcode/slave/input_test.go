package slave

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

func TestParseRange(t *testing.T) {
	type answer struct {
		off, n int64
		ok     bool
	}
	for spec, want := range map[string]answer{
		"bytes=0-":       {0, 100, true},
		"bytes=40-":      {40, 60, true},
		"bytes=40-49":    {40, 10, true},
		"bytes=40-400":   {40, 60, true},
		"bytes=99-":      {99, 1, true},
		"bytes=-10":      {90, 10, true},
		"bytes=-1000":    {0, 100, true},
		"bytes=100-":     {},
		"bytes=50-40":    {},
		"bytes=-0":       {},
		"bytes=0-1,5-6":  {},
		"bytes=x-":       {},
		"bytes=-x":       {},
		"bytes=-5-":      {},
		"items=0-":       {},
		"bytes 0-":       {},
		"bytes=10":       {},
		"bytes=0-x":      {},
		"bytes=-":        {},
		"bytes=00000-09": {0, 10, true},
	} {
		var got answer
		got.off, got.n, got.ok = parseRange(spec, 100)
		if got != want {
			t.Errorf("%s: %+v, want %+v", spec, got, want)
		}
	}
	if _, _, ok := parseRange("bytes=-5", 0); ok {
		t.Error("a suffix of an empty source")
	}
}

func TestInput(t *testing.T) {
	data := []byte(strings.Repeat("0123456789", 10))
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	in, err := serveInput(ctx, cancel, &local{src: bytes.NewReader(data)}, int64(len(data)), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer in.close()
	base := strings.TrimSuffix(in.url, in.path)
	if !strings.HasPrefix(in.url, "http://127.0.0.1:") || len(in.path) < 1+26 {
		t.Errorf("serving at %s", in.url)
	}
	for _, c := range []struct {
		method, url, rng string
		status           int
		body             string
		headers          map[string]string
	}{
		{"GET", base + "/", "", http.StatusNotFound, "", nil},
		{"GET", base + in.path + "x", "", http.StatusNotFound, "", nil},
		{"GET", in.url, "", http.StatusOK, string(data), map[string]string{"Content-Length": "100", "Accept-Ranges": "bytes"}},
		{"HEAD", in.url, "", http.StatusOK, "", map[string]string{"Content-Length": "100", "Accept-Ranges": "bytes"}},
		{"GET", in.url, "bytes=95-", http.StatusPartialContent, "56789", map[string]string{"Content-Length": "5", "Content-Range": "bytes 95-99/100"}},
		{"HEAD", in.url, "bytes=10-19", http.StatusPartialContent, "", map[string]string{"Content-Length": "10", "Content-Range": "bytes 10-19/100"}},
		{"GET", in.url, "bytes=100-", http.StatusRequestedRangeNotSatisfiable, "", map[string]string{"Content-Range": "bytes */100"}},
		{"POST", in.url, "", http.StatusMethodNotAllowed, "", map[string]string{"Allow": "GET, HEAD"}},
	} {
		req, err := http.NewRequest(c.method, c.url, nil)
		if err != nil {
			t.Fatal(err)
		}
		if c.rng != "" {
			req.Header.Set("Range", c.rng)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != c.status || (c.status < 300 && string(body) != c.body) {
			t.Errorf("%s %s %s: %d %q, want %d %q", c.method, c.url, c.rng, res.StatusCode, body, c.status, c.body)
		}
		for name, want := range c.headers {
			if got := res.Header.Get(name); got != want {
				t.Errorf("%s %s %s: %s %q, want %q", c.method, c.url, c.rng, name, got, want)
			}
		}
	}
	if ctx.Err() != nil {
		t.Errorf("the job ended: %v", context.Cause(ctx))
	}

	in.close()
	if _, err := http.Get(in.url); err == nil {
		t.Error("served once closed")
	}
}

// failing is a source whose reads from at on fail.
type failing struct {
	io.ReaderAt
	at int64
}

var errFailing = errors.New("the disk is on fire")

func (f failing) ReadAt(p []byte, off int64) (int, error) {
	if off+int64(len(p)) > f.at {
		return 0, errFailing
	}
	return f.ReaderAt.ReadAt(p, off)
}

func TestInputSourceErrors(t *testing.T) {
	data := bytes.Repeat([]byte{7}, 1000)
	for name, c := range map[string]struct {
		src  io.ReaderAt
		size int64
		want string
	}{
		"failing": {failing{bytes.NewReader(data), 500}, 1000, errFailing.Error()},
		"short":   {bytes.NewReader(data), 2000, "the source ends at 1000 bytes, short of its size"},
	} {
		ctx, cancel := context.WithCancelCause(context.Background())
		in, err := serveInput(ctx, cancel, &local{src: c.src}, c.size, slog.Default())
		if err != nil {
			t.Fatal(err)
		}
		res, err := http.Get(in.url)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.ReadAll(res.Body)
		res.Body.Close()
		in.close()
		if err == nil {
			t.Errorf("%s: the whole body came", name)
		}
		if cause := context.Cause(ctx); cause == nil || !strings.Contains(cause.Error(), c.want) {
			t.Errorf("%s: the job ended for %v, want %q", name, cause, c.want)
		}
		cancel(nil)
	}
}

func TestTail(t *testing.T) {
	var short tail
	short.Write([]byte("Invalid data\n"))
	if got := short.String(); got != "Invalid data" {
		t.Errorf("%q", got)
	}
	var long tail
	for range 1000 {
		long.Write([]byte("error while decoding\n"))
	}
	long.Write([]byte("the last word\n"))
	got := long.String()
	if len(got) > tailSize || !strings.HasPrefix(got, "error while decoding\n") || !strings.HasSuffix(got, "\nthe last word") {
		t.Errorf("%d bytes: %q…%q", len(got), got[:30], got[len(got)-30:])
	}
}
