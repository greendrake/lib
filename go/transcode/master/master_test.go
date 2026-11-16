package master_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/coder/websocket"
	"greendrake.nz/go/transcode"
	"greendrake.nz/go/transcode/internal/transcodetest"
	"greendrake.nz/go/transcode/internal/wire"
	"greendrake.nz/go/transcode/master"
	"greendrake.nz/go/transcode/slave"
)

// served is a slave of one job at a time and a master of it.
func served(t *testing.T) (*transcodetest.Slave, master.Master) {
	s := transcodetest.NewSlave(t, slave.Config{Jobs: 1})
	return s, master.Master{URL: s.Serve(t)}
}

func TestSameOutput(t *testing.T) {
	s, m := served(t)
	src := transcodetest.Clip(t, 160, 120)
	local, err := transcodetest.Run(t, s, src, transcodetest.AVC)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := transcodetest.Run(t, m, src, transcodetest.AVC)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(remote, local) {
		t.Errorf("the remote output (%d bytes) differs from the in-process one (%d bytes)", len(remote), len(local))
	}
	if outcomes := []string{s.Outcome(), s.Outcome()}; !slices.Equal(outcomes, []string{"ok", "ok"}) {
		t.Errorf("logged %q", outcomes)
	}
}

// recording is a source keeping the reads made of it, in order.
type recording struct {
	io.ReaderAt
	mu    sync.Mutex
	reads [][2]int64 // offset, length
}

func (r *recording) ReadAt(p []byte, off int64) (int, error) {
	r.mu.Lock()
	r.reads = append(r.reads, [2]int64{off, int64(len(p))})
	r.mu.Unlock()
	return r.ReaderAt.ReadAt(p, off)
}

// A MOV with its index after the media is read by seeking: the index, near the end, before the media is read
// through, and the source read in full once, plus a few windows given up at the seeks.
func TestSeeking(t *testing.T) {
	_, m := served(t)
	src := transcodetest.Make(t, "late-index.mov", slices.Concat(transcodetest.Pattern(320, 240, 5), []string{"-c:v", "rawvideo", "-pix_fmt", "uyvy422"})...)
	size := int64(len(src))
	if boxes := transcodetest.Boxes(t, src); slices.Index(boxes, "moov") < slices.Index(boxes, "mdat") {
		t.Fatalf("the fixture's boxes are %v: the index before the media", boxes)
	}
	const window = 4 << 20 // the slave's
	if size < 4*window {
		t.Fatalf("a fixture of %d bytes is no more than a few windows", size)
	}
	rec := &recording{ReaderAt: bytes.NewReader(src)}
	o := transcode.Output{Format: "mp4", Video: &transcode.Video{Codec: "h264", MaxShortSide: 120}}
	if _, err := m.Transcode(context.Background(), transcode.Job{Label: "late", Source: rec, Size: size, Output: o}, io.Discard); err != nil {
		t.Fatal(err)
	}
	index := slices.IndexFunc(rec.reads, func(rd [2]int64) bool { return rd[0]+rd[1] > size-transcode.MaxChunk })
	through := slices.IndexFunc(rec.reads, func(rd [2]int64) bool { return rd[0] > 2*window && rd[0]+rd[1] <= size-window })
	if index < 0 || through < 0 || index > through {
		t.Errorf("the index read at %d, the media's middle at %d, of %d reads", index, through, len(rec.reads))
	}
	var read int64
	for _, rd := range rec.reads {
		read += rd[1]
	}
	if read < size || read >= 2*size {
		t.Errorf("%d bytes read of a source of %d", read, size)
	}
}

func TestProgress(t *testing.T) {
	s, m := served(t)
	src := transcodetest.Clip(t, 160, 120)
	first, gate := transcodetest.Gated(transcodetest.Job(src))
	a := transcodetest.Go(context.Background(), s, first)
	<-gate.Reading()
	b := transcodetest.Go(context.Background(), m, transcodetest.Job(src))
	b.Await(t, transcode.Progress{Queued: 0})
	gate.Open()
	a.Rest(t)

	rest := b.Rest(t)
	if len(rest) < 2 || rest[0] != (transcode.Progress{Running: true}) {
		t.Fatalf("over the wire: %+v", rest)
	}
	if last := rest[len(rest)-1]; !last.Running || last.Seconds < 1.9 || last.Seconds > 2.1 {
		t.Errorf("the last progress over the wire: %+v", last)
	}
}

// Cancelling the master's context ends the job on the slave at once, even while a read of the source holds the
// master up.
func TestCancel(t *testing.T) {
	s, m := served(t)
	j, gate := transcodetest.Gated(transcodetest.Job(transcodetest.Clip(t, 160, 120)))
	ctx, cancel := context.WithCancel(context.Background())
	b := transcodetest.Go(ctx, m, j)
	<-gate.Reading()
	cancel()
	if outcome := s.Outcome(); outcome != "cancelled" {
		t.Errorf("the slave logged %q", outcome)
	}
	if jobs := s.Jobs(t); len(jobs) != 0 {
		t.Errorf("the slave left %v", jobs)
	}
	gate.Open()
	if err := <-b.Done; !errors.Is(err, context.Canceled) {
		t.Errorf("the master: %v", err)
	}
}

// A slave closing ends its masters' jobs, the connection closed as going away.
func TestSlaveClosing(t *testing.T) {
	s, m := served(t)
	src := transcodetest.Clip(t, 160, 120)
	first, gate := transcodetest.Gated(transcodetest.Job(src))
	a := transcodetest.Go(context.Background(), s, first)
	<-gate.Reading()
	b := transcodetest.Go(context.Background(), m, transcodetest.Job(src))
	b.Await(t, transcode.Progress{Queued: 0})

	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	if err := <-b.Done; websocket.CloseStatus(err) != websocket.StatusGoingAway {
		t.Errorf("the master: %v", err)
	}
	gate.Open()
	<-a.Done
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if jobs := s.Jobs(t); len(jobs) != 0 {
		t.Errorf("the slave left %v", jobs)
	}
}

func TestFailure(t *testing.T) {
	s, m := served(t)
	var failure *transcode.Failure
	if _, err := transcodetest.Run(t, m, []byte("not media at all"), transcodetest.AVC); !errors.As(err, &failure) || failure.Message == "" || failure.Detail == "" {
		t.Errorf("an unreadable source: %v", err)
	}
	o := transcode.Output{Format: "mp4", Video: &transcode.Video{Codec: "nonesuch"}}
	if _, err := transcodetest.Run(t, m, transcodetest.Clip(t, 160, 120), o); !errors.As(err, &failure) || !strings.Contains(failure.Message, `"nonesuch" is no video codec`) {
		t.Errorf("a codec without encoder: %v", err)
	}
	for range 2 {
		if outcome := s.Outcome(); !strings.HasPrefix(outcome, "failed: ") {
			t.Errorf("the slave logged %q", outcome)
		}
	}
}

func TestAbort(t *testing.T) {
	s, m := served(t)
	j := transcode.Job{Label: "broken", Source: transcodetest.Failing{}, Size: 1 << 20, Output: transcodetest.AVC}
	if _, err := m.Transcode(context.Background(), j, io.Discard); !errors.Is(err, transcodetest.ErrFailing) {
		t.Errorf("the master: %v", err)
	}
	if outcome := s.Outcome(); outcome != "failed: transcode: the master cannot read the source: "+transcodetest.ErrFailing.Error() {
		t.Errorf("the slave logged %q", outcome)
	}
}

// An invalid output is refused before any slave is dialled.
func TestInvalidOutput(t *testing.T) {
	m := master.Master{URL: "ws://127.0.0.1:1/nowhere"}
	var failure *transcode.Failure
	if _, err := transcodetest.Run(t, m, []byte("x"), transcode.Output{Format: "mp4"}); !errors.As(err, &failure) {
		t.Errorf("%v, no Failure", err)
	}
}

// fakeSlave serves a slave that does as act says once the master has started its job, answering how the master
// closes the connection.
func fakeSlave(t *testing.T, subprotocols []string, act func(conn *websocket.Conn)) (master.Master, <-chan websocket.StatusCode) {
	status := make(chan websocket.StatusCode, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: subprotocols})
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.CloseNow()
		if len(subprotocols) > 0 {
			if msg, _, err := wire.Receive(r.Context(), conn); err != nil {
				t.Error(err)
				return
			} else if _, ok := msg.(*transcode.Start); !ok {
				t.Errorf("%T where the master starts", msg)
				return
			}
			act(conn)
		}
		for {
			if _, _, err := conn.Read(r.Context()); err != nil {
				status <- websocket.CloseStatus(err)
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return master.Master{URL: "ws" + strings.TrimPrefix(srv.URL, "http")}, status
}

func TestProtocolErrors(t *testing.T) {
	ctx := context.Background()
	send := func(conn *websocket.Conn, msgs ...transcode.Message) {
		for _, msg := range msgs {
			if err := wire.Send(ctx, conn, msg); err != nil {
				t.Error(err)
			}
		}
	}
	output := func(conn *websocket.Conn, n int) {
		if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, n)); err != nil {
			t.Error(err)
		}
	}
	const size = 100
	for name, act := range map[string]func(conn *websocket.Conn){
		"a result before running": func(conn *websocket.Conn) { send(conn, transcode.Result{Size: 1}) },
		"a read past the size": func(conn *websocket.Conn) {
			send(conn, transcode.Running{}, transcode.Read{ID: 0, Offset: size - 10, Length: 11})
		},
		"output before the result": func(conn *websocket.Conn) {
			send(conn, transcode.Running{})
			output(conn, 3)
		},
		"more output than the result said": func(conn *websocket.Conn) {
			send(conn, transcode.Running{}, transcode.Result{Size: 2})
			output(conn, 3)
		},
		"a negative result": func(conn *websocket.Conn) { send(conn, transcode.Running{}, transcode.Result{Size: -1}) },
		"an unknown message": func(conn *websocket.Conn) {
			send(conn, transcode.Running{})
			if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"seek","offset":1}`)); err != nil {
				t.Error(err)
			}
		},
	} {
		m, status := fakeSlave(t, []string{transcode.Subprotocol}, act)
		var protocol wire.ProtocolError
		if _, err := transcodetest.Run(t, m, make([]byte, size), transcodetest.AVC); !errors.As(err, &protocol) {
			t.Errorf("%s: %v, no protocol error", name, err)
		}
		if got := <-status; got != websocket.StatusProtocolError {
			t.Errorf("%s: the master closed with %v", name, got)
		}
	}

	m, status := fakeSlave(t, nil, nil)
	var protocol wire.ProtocolError
	if _, err := transcodetest.Run(t, m, make([]byte, size), transcodetest.AVC); !errors.As(err, &protocol) || !strings.Contains(err.Error(), transcode.Subprotocol) {
		t.Errorf("no subprotocol: %v", err)
	}
	if got := <-status; got != websocket.StatusProtocolError {
		t.Errorf("no subprotocol: the master closed with %v", got)
	}

	m, _ = fakeSlave(t, []string{transcode.Subprotocol}, func(conn *websocket.Conn) {
		send(conn, transcode.Running{}, transcode.Result{Size: 10})
		output(conn, 3)
		conn.Close(websocket.StatusNormalClosure, "")
	})
	if _, err := transcodetest.Run(t, m, make([]byte, size), transcodetest.AVC); err == nil || errors.As(err, &protocol) || !strings.Contains(err.Error(), "before the whole output") {
		t.Errorf("a close before the whole output: %v", err)
	}
}
