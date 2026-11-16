package slave_test

import (
	"context"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"greendrake.nz/go/transcode"
	"greendrake.nz/go/transcode/internal/transcodetest"
	"greendrake.nz/go/transcode/slave"
)

// master is a master's end of a connection to a slave, as a test drives it.
type master struct {
	t    *testing.T
	conn *websocket.Conn
}

func dial(t *testing.T, url string, subprotocols ...string) master {
	conn, _, err := websocket.Dial(context.Background(), url, &websocket.DialOptions{Subprotocols: subprotocols})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return master{t, conn}
}

func (m master) send(msg transcode.Message) {
	b, err := transcode.Encode(msg)
	if err == nil {
		err = m.conn.Write(context.Background(), websocket.MessageText, b)
	}
	if err != nil {
		m.t.Fatal(err)
	}
}

// receive is the slave's next text message.
func (m master) receive() transcode.Message {
	typ, b, err := m.conn.Read(context.Background())
	if err != nil {
		m.t.Fatal(err)
	}
	if typ != websocket.MessageText {
		m.t.Fatalf("%d bytes of output", len(b))
	}
	msg, err := transcode.Decode(b)
	if err != nil {
		m.t.Fatal(err)
	}
	return msg
}

// read is the slave's next read.
func (m master) read() *transcode.Read {
	for {
		if rd, ok := m.receive().(*transcode.Read); ok {
			return rd
		}
	}
}

// closed is how the slave closes the connection, the messages before it dropped.
func (m master) closed() websocket.StatusCode {
	for {
		if _, _, err := m.conn.Read(context.Background()); err != nil {
			return websocket.CloseStatus(err)
		}
	}
}

func TestRemoteRefused(t *testing.T) {
	s := transcodetest.NewSlave(t, slave.Config{Jobs: 1})
	url := s.Serve(t)

	if status := dial(t, url).closed(); status != websocket.StatusProtocolError {
		t.Errorf("no subprotocol: closed with %v", status)
	}
	for name, first := range map[string]transcode.Message{
		"no start":        transcode.Abort{Error: "nothing"},
		"a negative size": transcode.Start{Size: -1, Output: transcodetest.AVC},
	} {
		m := dial(t, url, transcode.Subprotocol)
		m.send(first)
		if status := m.closed(); status != websocket.StatusProtocolError {
			t.Errorf("%s: closed with %v", name, status)
		}
	}

	m := dial(t, url, transcode.Subprotocol)
	m.send(transcode.Start{Size: 10, Output: transcode.Output{Format: "mp4"}})
	if failure, ok := m.receive().(*transcode.Failure); !ok || !strings.Contains(failure.Message, "neither video nor audio") {
		t.Errorf("an invalid output: %+v", failure)
	}
	if status := m.closed(); status != websocket.StatusNormalClosure {
		t.Errorf("an invalid output: closed with %v", status)
	}
	if outcome := s.Outcome(); !strings.HasPrefix(outcome, "failed: ") {
		t.Errorf("an invalid output: logged %q", outcome)
	}
}

func TestRemoteSlaveClosing(t *testing.T) {
	s := transcodetest.NewSlave(t, slave.Config{Jobs: 1})
	url := s.Serve(t)
	yetToStart := dial(t, url, transcode.Subprotocol)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if status := yetToStart.closed(); status != websocket.StatusGoingAway {
		t.Errorf("a master yet to start: closed with %v", status)
	}
	if status := dial(t, url, transcode.Subprotocol).closed(); status != websocket.StatusGoingAway {
		t.Errorf("a master of a closed slave: closed with %v", status)
	}
}

func TestRemoteMisbehaving(t *testing.T) {
	s := transcodetest.NewSlave(t, slave.Config{Jobs: 1})
	url := s.Serve(t)
	src := transcodetest.Clip(t, 160, 120)
	answer := func(id uint64, b []byte) []byte { return append(binary.BigEndian.AppendUint64(nil, id), b...) }
	for name, c := range map[string]struct {
		misbehave func(m master, rd *transcode.Read)
		status    websocket.StatusCode // the slave closes with; 0: the master closes
		outcome   string
	}{
		"an answer out of turn": {func(m master, rd *transcode.Read) {
			m.conn.Write(context.Background(), websocket.MessageBinary, answer(rd.ID+1, src[rd.Offset:rd.Offset+int64(rd.Length)]))
		}, websocket.StatusProtocolError, "failed: transcode: protocol error: an answer to read"},
		"an answer short": {func(m master, rd *transcode.Read) {
			m.conn.Write(context.Background(), websocket.MessageBinary, answer(rd.ID, src[rd.Offset:rd.Offset+int64(rd.Length)-1]))
		}, websocket.StatusProtocolError, "failed: transcode: protocol error: "},
		"no answer at all": {func(m master, rd *transcode.Read) {
			m.conn.Write(context.Background(), websocket.MessageBinary, []byte{1, 2, 3})
		}, websocket.StatusProtocolError, "failed: transcode: protocol error: an answer of 3 bytes"},
		"a second start": {func(m master, rd *transcode.Read) {
			m.send(transcode.Start{Size: 1, Output: transcodetest.AVC})
		}, websocket.StatusProtocolError, "failed: transcode: protocol error: "},
		"garbage": {func(m master, rd *transcode.Read) {
			m.conn.Write(context.Background(), websocket.MessageText, []byte("{"))
		}, websocket.StatusProtocolError, "failed: transcode: protocol error: "},
		"an abort": {func(m master, rd *transcode.Read) {
			m.send(transcode.Abort{Error: "the disk is on fire"})
			m.conn.Close(websocket.StatusNormalClosure, "")
		}, 0, "failed: transcode: the master cannot read the source: the disk is on fire"},
		"the master gone": {func(m master, rd *transcode.Read) {
			m.conn.CloseNow()
		}, 0, "cancelled"},
	} {
		m := dial(t, url, transcode.Subprotocol)
		m.send(transcode.Start{Label: name, Size: int64(len(src)), Output: transcodetest.AVC})
		c.misbehave(m, m.read())
		if c.status != 0 {
			if status := m.closed(); status != c.status {
				t.Errorf("%s: closed with %v", name, status)
			}
		}
		if outcome := s.Outcome(); !strings.HasPrefix(outcome, c.outcome) {
			t.Errorf("%s: logged %q", name, outcome)
		}
		if jobs := s.Jobs(t); len(jobs) != 0 {
			t.Errorf("%s: left %v", name, jobs)
		}
	}
}
