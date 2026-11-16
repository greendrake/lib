// Package master has transcode jobs done by a slave on another machine, over the transcode protocol: the source
// stays here, the slave reading the byte ranges its ffmpeg asks for, and the output comes back.
package master

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"

	"github.com/coder/websocket"
	"greendrake.nz/go/transcode"
	"greendrake.nz/go/transcode/internal/wire"
)

// Master is a transcode.Transcoder doing its jobs on the slave at URL, a connection each.
type Master struct {
	URL string // the slave's WebSocket endpoint, ws://host:port/path
}

// phase is how far a job has come, as its master follows it.
type phase int

const (
	waiting   phase = iota // for a slot
	running                // ffmpeg is at it
	receiving              // the output
)

// phases are the phases in which each message may come from the slave.
var phases = map[reflect.Type][]phase{
	reflect.TypeFor[*transcode.Queued]():   {waiting},
	reflect.TypeFor[*transcode.Running]():  {waiting},
	reflect.TypeFor[*transcode.Advanced](): {running},
	reflect.TypeFor[*transcode.Read]():     {running},
	reflect.TypeFor[*transcode.Result]():   {running},
	reflect.TypeFor[*transcode.Failure]():  {waiting, running},
}

// sourceError is the source failing a read, which the slave is told of by an abort.
type sourceError struct{ err error }

func (e *sourceError) Error() string { return "transcode: reading the source: " + e.err.Error() }
func (e *sourceError) Unwrap() error { return e.err }

// Transcode does job on the slave, answering its reads from job.Source in the order they came and writing the output
// to dst. Cancelling ctx closes the connection, which ends the job on the slave; there is no timeout of its own.
func (m Master) Transcode(ctx context.Context, job transcode.Job, dst io.Writer) (int64, error) {
	if err := job.Output.Validate(); err != nil {
		return 0, err
	}
	conn, _, err := websocket.Dial(ctx, m.URL, &websocket.DialOptions{Subprotocols: []string{transcode.Subprotocol}})
	if err != nil {
		return 0, fmt.Errorf("transcode: dialling the slave: %w", err)
	}
	defer conn.CloseNow()
	defer context.AfterFunc(ctx, func() { conn.CloseNow() })() // even while a read of the source holds the master up
	n, err := follow(ctx, conn, job, dst)
	var failure *transcode.Failure
	var source *sourceError
	switch {
	case err != nil && ctx.Err() != nil:
		return n, ctx.Err()
	case errors.As(err, &failure), errors.As(err, &source):
		wire.End(conn, nil) // an end the protocol says
	default:
		wire.End(conn, err)
	}
	return n, err
}

// follow has the slave at the other end of conn do job, to its end.
func follow(ctx context.Context, conn *websocket.Conn, job transcode.Job, dst io.Writer) (int64, error) {
	if conn.Subprotocol() != transcode.Subprotocol {
		return 0, wire.Errorf("the slave speaks no %s", transcode.Subprotocol)
	}
	conn.SetReadLimit(transcode.MaxChunk)
	if err := wire.Send(ctx, conn, transcode.Start{Label: job.Label, Size: job.Size, Output: job.Output}); err != nil {
		return 0, fmt.Errorf("transcode: starting the job: %w", err)
	}
	at, size, n := waiting, int64(0), int64(0)
	for {
		msg, output, err := wire.Receive(ctx, conn)
		var protocol wire.ProtocolError
		switch {
		case errors.As(err, &protocol):
			return n, err
		case err != nil:
			return n, fmt.Errorf("transcode: the connection ended before the whole output: %w", err)
		case msg == nil:
			if at != receiving || int64(len(output)) > size-n {
				return n, wire.Errorf("%d bytes of output out of place", len(output))
			}
			if _, err := dst.Write(output); err != nil {
				return n, fmt.Errorf("transcode: writing the output: %w", err)
			}
			if n += int64(len(output)); n == size {
				return n, nil
			}
			continue
		case !slices.Contains(phases[reflect.TypeOf(msg)], at):
			return n, wire.Errorf("%T out of place", msg)
		}
		switch msg := msg.(type) {
		case *transcode.Running:
			at = running
			job.Report(msg)
		case transcode.Status:
			job.Report(msg)
		case *transcode.Read:
			if err := answer(ctx, conn, job, msg); err != nil {
				return n, err
			}
		case *transcode.Result:
			if msg.Size < 0 {
				return n, wire.Errorf("an output of %d bytes", msg.Size)
			}
			if at, size = receiving, msg.Size; size == 0 {
				return 0, nil
			}
		case *transcode.Failure:
			return n, msg
		}
	}
}

// answer answers the slave's read rd from job.Source; a source error aborts the job.
func answer(ctx context.Context, conn *websocket.Conn, job transcode.Job, rd *transcode.Read) error {
	if rd.Offset < 0 || rd.Length < 1 || rd.Length > transcode.MaxChunk || rd.Offset > job.Size-int64(rd.Length) {
		return wire.Errorf("read %d asks for %d bytes from %d of a source of %d", rd.ID, rd.Length, rd.Offset, job.Size)
	}
	b := make([]byte, transcode.AnswerHeader+rd.Length)
	binary.BigEndian.PutUint64(b, rd.ID)
	if k, err := job.Source.ReadAt(b[transcode.AnswerHeader:], rd.Offset); k < rd.Length {
		// The slave is told, if it can be; the job ends either way.
		_ = wire.Send(ctx, conn, transcode.Abort{Error: err.Error()})
		return &sourceError{err}
	}
	if err := conn.Write(ctx, websocket.MessageBinary, b); err != nil {
		return fmt.Errorf("transcode: answering read %d: %w", rd.ID, err)
	}
	return nil
}
