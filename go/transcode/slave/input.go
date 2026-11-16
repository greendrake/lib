package slave

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// source is a job's bytes, as the loopback input gives them to ffmpeg.
type source interface {
	// open reads n bytes from off, for one of ffmpeg's requests, until ctx ends; closing it gives up what it has
	// asked for ahead of its reader.
	open(ctx context.Context, off, n int64) io.ReadCloser
	// taken is how many bytes the source has given.
	taken() int64
}

// input serves a job's source to ffmpeg over HTTP, on a loopback port of its own, at a path holding a random token
// (128 bits) of the job's, so that no other process on the machine reads the source by guessing it: HEAD and GET,
// with ranges, as ffmpeg seeks by asking a new one.
type input struct {
	url    string
	path   string
	size   int64
	source source
	ctx    context.Context
	fail   context.CancelCauseFunc // ends the job for a source error
	srv    *http.Server
	mu     sync.Mutex
	closed bool
	served sync.WaitGroup // the requests being answered
}

// serveInput serves src, of size bytes, until close; a source error ends the job, under ctx, through fail.
func serveInput(ctx context.Context, fail context.CancelCauseFunc, src source, size int64, logger *slog.Logger) (*input, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	in := &input{path: "/" + rand.Text(), size: size, source: src, ctx: ctx, fail: fail}
	in.url = "http://" + ln.Addr().String() + in.path
	in.srv = &http.Server{Handler: in, ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelWarn)}
	go in.srv.Serve(ln) // ends with close, answering http.ErrServerClosed
	return in, nil
}

// close stops the input, once the requests being answered have been.
func (in *input) close() {
	in.mu.Lock()
	in.closed = true
	in.mu.Unlock()
	in.srv.Close()
	in.served.Wait()
}

func (in *input) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	in.mu.Lock()
	if in.closed {
		in.mu.Unlock()
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}
	in.served.Add(1)
	in.mu.Unlock()
	defer in.served.Done()

	if r.URL.Path != in.path {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	h := w.Header()
	off, n, status := int64(0), in.size, http.StatusOK
	if spec := r.Header.Get("Range"); spec != "" {
		var ok bool
		if off, n, ok = parseRange(spec, in.size); !ok {
			h.Set("Content-Range", fmt.Sprintf("bytes */%d", in.size))
			http.Error(w, http.StatusText(http.StatusRequestedRangeNotSatisfiable), http.StatusRequestedRangeNotSatisfiable)
			return
		}
		status = http.StatusPartialContent
		h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", off, off+n-1, in.size))
	}
	h.Set("Accept-Ranges", "bytes")
	h.Set("Content-Length", strconv.FormatInt(n, 10))
	h.Set("Content-Type", "application/octet-stream")
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	body := in.source.open(in.ctx, off, n)
	defer body.Close()
	for buf := make([]byte, 64<<10); ; {
		k, err := body.Read(buf)
		if _, werr := w.Write(buf[:k]); werr != nil {
			return // ffmpeg has hung up: it seeks elsewhere, or is done
		}
		switch {
		case err == io.EOF:
			return
		case err != nil:
			in.fail(fmt.Errorf("transcode: reading the source: %w", err))
			return
		}
	}
}

// parseRange reads a Range header of one range of bytes, as ffmpeg asks for one — from an offset to the end or to a
// last byte, or the last so many — answering its offset and length within size; or !ok for anything else, or a
// range none of which is within size.
func parseRange(spec string, size int64) (off, n int64, ok bool) {
	spec, isBytes := strings.CutPrefix(spec, "bytes=")
	first, last, cut := strings.Cut(spec, "-")
	if !isBytes || !cut {
		return 0, 0, false
	}
	if first == "" {
		n, err := strconv.ParseInt(last, 10, 64)
		if err != nil || n <= 0 || size == 0 {
			return 0, 0, false
		}
		n = min(n, size)
		return size - n, n, true
	}
	off, err := strconv.ParseInt(first, 10, 64)
	if err != nil || off < 0 || off >= size {
		return 0, 0, false
	}
	end := size - 1
	if last != "" {
		if end, err = strconv.ParseInt(last, 10, 64); err != nil || end < off {
			return 0, 0, false
		}
		end = min(end, size-1)
	}
	return off, end - off + 1, true
}

// local is the source of an in-process job: the caller's.
type local struct {
	src io.ReaderAt
	n   atomic.Int64 // bytes given
}

func (l *local) open(_ context.Context, off, n int64) io.ReadCloser {
	return io.NopCloser(&localRange{local: l, off: off, left: n})
}

func (l *local) taken() int64 { return l.n.Load() }

// localRange reads a range of a local source.
type localRange struct {
	*local
	off, left int64
}

func (r *localRange) Read(p []byte) (int, error) {
	if r.left == 0 {
		return 0, io.EOF
	}
	k, err := r.src.ReadAt(p[:min(int64(len(p)), r.left)], r.off)
	r.off, r.left = r.off+int64(k), r.left-int64(k)
	r.n.Add(int64(k))
	switch {
	case err == io.EOF && r.left > 0:
		return k, fmt.Errorf("the source ends at %d bytes, short of its size", r.off)
	case err == io.EOF:
		return k, nil
	}
	return k, err
}
