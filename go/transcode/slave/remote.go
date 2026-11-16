package slave

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"

	"github.com/coder/websocket"
	"greendrake.nz/go/transcode"
	"greendrake.nz/go/transcode/internal/wire"
)

// window is how far ahead of what ffmpeg has taken a request's stream asks the master for the source: enough to
// keep the source flowing over a link's latency, little enough that a seek, which gives up what was asked ahead,
// wastes no more.
const window = 4 << 20

// ServeHTTP does the job of the master at the other end of a WebSocket, over the transcode protocol, in the slave's
// queue with its in-process jobs: its source is the master's answers to the reads its ffmpeg makes, its output goes
// back over the connection.
func (s *Slave) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{transcode.Subprotocol}})
	if err != nil {
		s.logger.Warn("transcode: a WebSocket handshake refused", "from", r.RemoteAddr, "err", err)
		return
	}
	defer conn.CloseNow()
	ctx, cancel := s.jobContext(r.Context())
	defer cancel(nil)
	// Whatever ends the job before it ends of itself — the slave closing, a master yet to start included; an abort or
	// the master gone (closed already); a protocol error — closes the connection as it calls for, which also unblocks
	// a read or a write a stalled master holds up. The handler returns once it has.
	ended := make(chan struct{})
	closing := context.AfterFunc(ctx, func() {
		end(conn, context.Cause(ctx))
		close(ended)
	})
	start, err := receiveStart(conn)
	switch {
	case err != nil && ctx.Err() == nil:
		s.logger.Warn("transcode: a master refused", "from", r.RemoteAddr, "err", err)
	case err == nil:
		rm := &remote{conn: conn, end: cancel}
		go rm.receive()
		_, err = s.do(ctx, cancel, task{
			origin:  r.RemoteAddr,
			label:   start.Label,
			size:    start.Size,
			output:  start.Output,
			source:  rm,
			report:  func(st transcode.Status) { rm.send(st) },
			deliver: rm.deliver,
		})
		if failure, ok := errors.AsType[*transcode.Failure](err); ok {
			err = rm.send(failure)
		}
	}
	if !closing() {
		<-ended
		return
	}
	end(conn, err)
}

// receiveStart reads the master's start, on a connection speaking the protocol. It waits for as long as the
// connection lasts: the job's end closes it.
func receiveStart(conn *websocket.Conn) (*transcode.Start, error) {
	if conn.Subprotocol() != transcode.Subprotocol {
		return nil, wire.Errorf("the master speaks no %s", transcode.Subprotocol)
	}
	conn.SetReadLimit(transcode.AnswerHeader + transcode.MaxChunk)
	msg, _, err := wire.Receive(context.Background(), conn)
	if err != nil {
		return nil, err
	}
	start, ok := msg.(*transcode.Start)
	switch {
	case !ok:
		return nil, wire.Errorf("%T where the master starts", msg)
	case start.Size < 0:
		return nil, wire.Errorf("a source of %d bytes", start.Size)
	}
	return start, nil
}

// end closes conn as err, the end of its job, calls for: wire.End's, and 1001 for the slave closing.
func end(conn *websocket.Conn, err error) {
	if errors.Is(err, errClosed) {
		wire.Close(conn, websocket.StatusGoingAway, "the slave is closing")
		return
	}
	wire.End(conn, err)
}

// remote is the source of a master's job: the master's answers to the reads its streams ask for, one stream for each
// of ffmpeg's requests.
type remote struct {
	conn   *websocket.Conn
	end    context.CancelCauseFunc // ends the job
	sendMu sync.Mutex              // orders the reads as sent, which is the order their answers come in
	mu     sync.Mutex
	next   uint64       // the next read's ID
	asked  []*read      // asked for and not yet answered, in the order asked
	err    error        // why the source reads no more
	n      atomic.Int64 // bytes answered
}

// read is a read asked of the master.
type read struct {
	id        uint64
	offset    int64
	length    int
	done      chan struct{} // closed once data or err is in
	data      []byte
	err       error
	abandoned bool // its stream wants it no longer: its answer is dropped
}

// receive reads the master's messages until the connection ends: the answers to the reads, and an abort.
func (rm *remote) receive() {
	for {
		msg, answer, err := wire.Receive(context.Background(), rm.conn)
		var protocol wire.ProtocolError
		switch {
		case errors.As(err, &protocol):
			rm.fail(err)
		case err != nil:
			rm.fail(fmt.Errorf("transcode: the master has gone (%w): %w", err, context.Canceled))
			return
		case msg == nil:
			rm.answer(answer)
		default:
			if abort, ok := msg.(*transcode.Abort); ok {
				rm.fail(fmt.Errorf("transcode: the master cannot read the source: %s", abort.Error))
			} else {
				rm.fail(wire.Errorf("%T from the master after its start", msg))
			}
		}
	}
}

// answer takes the master's answer to its oldest read outstanding.
func (rm *remote) answer(b []byte) {
	if len(b) < transcode.AnswerHeader {
		rm.fail(wire.Errorf("an answer of %d bytes", len(b)))
		return
	}
	id := binary.BigEndian.Uint64(b)
	rm.mu.Lock()
	switch {
	case len(rm.asked) == 0 || rm.asked[0].id != id:
		rm.mu.Unlock()
		rm.fail(wire.Errorf("an answer to read %d out of turn", id))
		return
	case len(b)-transcode.AnswerHeader != rm.asked[0].length:
		rm.mu.Unlock()
		rm.fail(wire.Errorf("%d bytes answering read %d of %d", len(b)-transcode.AnswerHeader, id, rm.asked[0].length))
		return
	}
	rd := rm.asked[0]
	rm.asked = rm.asked[1:]
	if !rd.abandoned {
		rd.data = b[transcode.AnswerHeader:]
	}
	close(rd.done)
	rm.mu.Unlock()
	rm.n.Add(int64(rd.length))
}

// fail ends the job, and the source, for err: the reads outstanding fail with it. The job ends first, so that its
// cause is err, and not the loopback input's account of a read failing with it.
func (rm *remote) fail(err error) {
	rm.end(err)
	rm.mu.Lock()
	if rm.err == nil {
		rm.err = err
		for _, rd := range rm.asked {
			rd.err = err
			close(rd.done)
		}
		rm.asked = nil
	}
	rm.mu.Unlock()
}

// send writes m to the master; a failure ends the job.
func (rm *remote) send(m transcode.Message) error {
	err := wire.Send(context.Background(), rm.conn, m)
	if err != nil {
		rm.fail(fmt.Errorf("transcode: sending to the master: %w", err))
	}
	return err
}

// ask asks the master for length bytes of the source from offset.
func (rm *remote) ask(offset int64, length int) *read {
	rd := &read{offset: offset, length: length, done: make(chan struct{})}
	rm.sendMu.Lock()
	defer rm.sendMu.Unlock()
	rm.mu.Lock()
	if rm.err != nil {
		rd.err = rm.err
		close(rd.done)
		rm.mu.Unlock()
		return rd
	}
	rd.id = rm.next
	rm.next++
	rm.asked = append(rm.asked, rd)
	rm.mu.Unlock()
	rm.send(transcode.Read{ID: rd.id, Offset: offset, Length: length}) // a failure fails rd, with the job
	return rd
}

func (rm *remote) open(ctx context.Context, off, n int64) io.ReadCloser {
	return &stream{rm: rm, ctx: ctx, pos: off, end: off + n, asked: off}
}

func (rm *remote) taken() int64 { return rm.n.Load() }

// deliver sends the master the result, then the output.
func (rm *remote) deliver(out *os.File, size int64) (int64, error) {
	if err := rm.send(transcode.Result{Size: size}); err != nil {
		return 0, err
	}
	var n int64
	for chunk := make([]byte, transcode.MaxChunk); ; {
		k, err := io.ReadFull(out, chunk)
		if k > 0 {
			if err := rm.conn.Write(context.Background(), websocket.MessageBinary, chunk[:k]); err != nil {
				return n, fmt.Errorf("transcode: sending the output: %w", err)
			}
			n += int64(k)
		}
		switch {
		case err == io.EOF, err == io.ErrUnexpectedEOF:
			return n, nil
		case err != nil:
			return n, fmt.Errorf("transcode: reading the output: %w", err)
		}
	}
}

// stream reads a range of a remote source, for one of ffmpeg's requests, asking for up to a window ahead of its
// reader.
type stream struct {
	rm       *remote
	ctx      context.Context
	pos, end int64   // the next byte to give, and the range's end
	asked    int64   // where the next read asks from
	reads    []*read // asked for and not given in full, in order
}

func (st *stream) Read(p []byte) (int, error) {
	if st.pos == st.end {
		return 0, io.EOF
	}
	for st.asked < st.end && st.asked-st.pos < window {
		length := min(transcode.MaxChunk, st.end-st.asked)
		st.reads = append(st.reads, st.rm.ask(st.asked, int(length)))
		st.asked += length
	}
	rd := st.reads[0]
	select {
	case <-rd.done:
	case <-st.ctx.Done():
		return 0, context.Cause(st.ctx)
	}
	if rd.err != nil {
		return 0, rd.err
	}
	k := copy(p, rd.data[st.pos-rd.offset:])
	st.pos += int64(k)
	if st.pos == rd.offset+int64(rd.length) {
		st.reads = st.reads[1:]
	}
	return k, nil
}

// Close gives up the reads asked for and not yet given: their answers are dropped.
func (st *stream) Close() error {
	st.rm.mu.Lock()
	for _, rd := range st.reads {
		rd.abandoned = true
		rd.data = nil
	}
	st.rm.mu.Unlock()
	return nil
}
