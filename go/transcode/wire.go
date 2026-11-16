package transcode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
)

// The protocol between a master and a slave: one job per connection, a WebSocket (RFC 6455) from the master to the
// slave, living exactly as long as the connection — either side closing it, or the connection dropping, ends the job.
//
// Text messages are one JSON object each (Message), its "type" saying which, and nothing its type does not have.
// Binary messages are bytes: from the master, the answers to the slave's reads; from the slave, the output. A message
// out of place, a malformed one or an answer of the wrong length closes the connection with 1002; a failure inside
// either side that no message covers, with 1011; a slave closing, with 1001.
const (
	// Subprotocol is the protocol's WebSocket subprotocol.
	Subprotocol = "transcode.v1"
	// MaxChunk is the most bytes a read asks for, and an output chunk carries.
	MaxChunk = 1 << 20
	// AnswerHeader is the length of a read's answer ahead of the bytes: the read's ID, a big-endian unsigned integer.
	AnswerHeader = 8
)

// Message is a text message of the protocol: Start or Abort from the master; Queued, Running, Read, Advanced, Result
// or a Failure (failed) from the slave.
type Message interface{ messageType() string }

// Status is a message saying how a job is getting on: Queued, Running or Advanced.
type Status interface {
	Message
	Progress() Progress
}

// Start is the master's first message, once: the job.
type Start struct {
	Label  string `json:"label"`
	Size   int64  `json:"size"`
	Output Output `json:"output"`
}

// Abort says the master cannot read the source; it closes the connection after it.
type Abort struct {
	Error string `json:"error"`
}

// Queued says the job waits for a slot, Ahead jobs before it; it comes again as its place changes.
type Queued struct {
	Ahead int `json:"ahead"`
}

// Running says the job has its slot; reads follow.
type Running struct{}

// Read asks for Length bytes of the source from Offset: at most MaxChunk, never past its size; the ID is unique within
// the job. The master answers every read, in the order they came, with a binary message: the ID (AnswerHeader bytes),
// then exactly the bytes asked. The slave drops by its ID an answer it no longer wants.
type Read struct {
	ID     uint64 `json:"id"`
	Offset int64  `json:"offset"`
	Length int    `json:"length"`
}

// Advanced is the progress message: how far into the source the output has come, every few seconds while ffmpeg runs.
type Advanced struct {
	Seconds float64 `json:"seconds"`
}

// Result says ffmpeg has succeeded: the output follows, in binary messages of at most MaxChunk bytes adding up to
// Size. The slave closes the connection with 1000 after it, as it does after a failed message.
type Result struct {
	Size int64 `json:"size"`
}

func (Start) messageType() string    { return "start" }
func (Abort) messageType() string    { return "abort" }
func (Queued) messageType() string   { return "queued" }
func (Running) messageType() string  { return "running" }
func (Read) messageType() string     { return "read" }
func (Advanced) messageType() string { return "progress" }
func (Result) messageType() string   { return "result" }
func (Failure) messageType() string  { return "failed" }

func (q Queued) Progress() Progress   { return Progress{Queued: q.Ahead} }
func (Running) Progress() Progress    { return Progress{Running: true} }
func (a Advanced) Progress() Progress { return Progress{Running: true, Seconds: a.Seconds} }

// messages make an empty message of each type, by the name its "type" carries.
var messages = func() map[string]func() Message {
	byType := map[string]func() Message{}
	for _, m := range []Message{Start{}, Abort{}, Queued{}, Running{}, Read{}, Advanced{}, Result{}, Failure{}} {
		t := reflect.TypeOf(m)
		byType[m.messageType()] = func() Message { return reflect.New(t).Interface().(Message) }
	}
	return byType
}()

// Encode is m as a text message: its JSON object, the "type" first.
func Encode(m Message) ([]byte, error) {
	body, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("transcode: encoding a %s message: %w", m.messageType(), err)
	}
	head := `{"type":"` + m.messageType() + `"`
	if len(body) > len("{}") {
		head += ","
	}
	return append([]byte(head), body[1:]...), nil
}

// Decode reads a text message of any type, as a pointer to it; the receiver checks it is one it takes. A field its type
// does not have, at any depth, makes it malformed: a peer never takes a message of another version of the protocol for
// one it knows, leaving out what it does not know.
func Decode(b []byte) (Message, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return nil, fmt.Errorf("a malformed message: %w", err)
	}
	var typ string
	if t, ok := fields["type"]; ok {
		if err := json.Unmarshal(t, &typ); err != nil {
			return nil, fmt.Errorf("a malformed message: %w", err)
		}
	}
	newMessage, ok := messages[typ]
	if !ok {
		return nil, fmt.Errorf("a message of unknown type %q", typ)
	}
	delete(fields, "type")
	body, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("a malformed %s message: %w", typ, err)
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	m := newMessage()
	if err := dec.Decode(m); err != nil {
		return nil, fmt.Errorf("a malformed %s message: %w", typ, err)
	}
	return m, nil
}
