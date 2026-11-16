// Package wire is the WebSocket side of the transcode protocol, as the master and the slave both speak it.
package wire

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/coder/websocket"
	"greendrake.nz/go/transcode"
)

// maxReason is the most bytes of a reason a close frame holds.
const maxReason = 123

// ProtocolError is a message out of place, a malformed one or an answer of the wrong length: it closes the
// connection with 1002.
type ProtocolError string

func (e ProtocolError) Error() string { return "transcode: protocol error: " + string(e) }

// Errorf is a ProtocolError.
func Errorf(format string, args ...any) error { return ProtocolError(fmt.Sprintf(format, args...)) }

// Send writes m as a text message.
func Send(ctx context.Context, conn *websocket.Conn, m transcode.Message) error {
	b, err := transcode.Encode(m)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, b)
}

// Receive reads a message: a text one decoded, or a binary one's bytes.
func Receive(ctx context.Context, conn *websocket.Conn) (transcode.Message, []byte, error) {
	typ, b, err := conn.Read(ctx)
	switch {
	case err != nil:
		return nil, nil, err
	case typ == websocket.MessageBinary:
		return nil, b, nil
	}
	m, err := transcode.Decode(b)
	if err != nil {
		return nil, nil, ProtocolError(err.Error())
	}
	return m, nil, nil
}

// End closes conn as err, its job's end, calls for: 1000 for none, 1002 for a ProtocolError, 1011 for any other.
func End(conn *websocket.Conn, err error) {
	var protocol ProtocolError
	switch {
	case err == nil:
		Close(conn, websocket.StatusNormalClosure, "")
	case errors.As(err, &protocol):
		Close(conn, websocket.StatusProtocolError, string(protocol))
	default:
		Close(conn, websocket.StatusInternalError, err.Error())
	}
}

// Close closes conn with code and as much of reason as a close frame holds. The job has ended whatever the closing
// handshake meets — a peer gone, a connection already closed — so that is not reported.
func Close(conn *websocket.Conn, code websocket.StatusCode, reason string) {
	if len(reason) > maxReason {
		reason = strings.ToValidUTF8(reason[:maxReason], "")
	}
	_ = conn.Close(code, reason)
}
