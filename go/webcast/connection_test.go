package webcast

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"greendrake.nz/go/camstream"
	"greendrake.nz/go/camstream/camstreamtest"
)

const testKey = "extra"

// A source closes its monitor after its last subscriber has left, so whether the monitor
// is shut answers the question these tests are really asking: is a subscription still
// taken?

type serverMessage struct {
	Op  string `json:"op"`
	ID  uint32 `json:"id"`
	Ch  byte   `json:"ch"`
	Key string `json:"key"`
}

func startTestServer(t *testing.T, lookup Lookup, opts ...Option) (*httptest.Server, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	server := httptest.NewServer(Handler(ctx, lookup, opts...))
	t.Cleanup(func() {
		cancel()
		server.Close()
	})
	return server, cancel
}

// source runs over cam until the test ends.
func source(t *testing.T, cam *camstreamtest.Camera) *camstream.Source {
	ctx, cancel := context.WithCancel(context.Background())
	s := camstream.NewSource(ctx, cam.Open)
	t.Cleanup(func() {
		cancel()
		<-s.Done()
	})
	return s
}

// streamServer serves testKey, to viewers and relays, from a source over the camera it
// answers.
func streamServer(t *testing.T) (*httptest.Server, *camstreamtest.Camera, context.CancelFunc) {
	t.Helper()
	cam := camstreamtest.NewCamera(t)
	s := source(t, cam)
	server, cancel := startTestServer(t, func(key string, _ bool) *camstream.Source {
		if key != testKey {
			return nil
		}
		return s
	})
	return server, cam, cancel
}

// urlOf is where server's handler is reached by a socket.
func urlOf(server *httptest.Server) string {
	return "ws" + strings.TrimPrefix(server.URL, "http")
}

func dialFrom(t *testing.T, server *httptest.Server, origin string) (*websocket.Conn, error) {
	t.Helper()
	ws, _, err := websocket.Dial(context.Background(), urlOf(server), &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {origin}}})
	if err == nil {
		ws.SetReadLimit(-1)
		t.Cleanup(func() { ws.CloseNow() })
	}
	return ws, err
}

// dial opens a socket on server as a page of its own origin does.
func dial(t *testing.T, server *httptest.Server) *websocket.Conn {
	t.Helper()
	ws, err := dialFrom(t, server, server.URL)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return ws
}

func within(t *testing.T, what string, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func sendOp(t *testing.T, ws *websocket.Conn, op map[string]any) {
	t.Helper()
	data, err := json.Marshal(op)
	if err != nil {
		t.Fatalf("marshal %v: %v", op, err)
	}
	if err := ws.Write(context.Background(), websocket.MessageText, data); err != nil {
		t.Fatalf("send %v: %v", op, err)
	}
}

// receiveMessage reads the next message, which must be of kind.
func receiveMessage(t *testing.T, ws *websocket.Conn, kind websocket.MessageType) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, message, err := ws.Read(ctx)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if got != kind {
		t.Fatalf("received a %v message, % x…, where a %v one was due", got, message[:min(len(message), 8)], kind)
	}
	return message
}

func receive(t *testing.T, ws *websocket.Conn) serverMessage {
	t.Helper()
	var parsed serverMessage
	if err := json.Unmarshal(receiveMessage(t, ws, websocket.MessageText), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return parsed
}

func receiveBinary(t *testing.T, ws *websocket.Conn) []byte {
	t.Helper()
	return receiveMessage(t, ws, websocket.MessageBinary)
}

func subscribeTo(t *testing.T, ws *websocket.Conn, id uint32, key string) serverMessage {
	t.Helper()
	sendOp(t, ws, map[string]any{"op": "sub", "id": id, "key": key})
	return receive(t, ws)
}

// subscribed subscribes to testKey, which must be answered with a channel.
func subscribed(t *testing.T, ws *websocket.Conn, id uint32) byte {
	t.Helper()
	answer := subscribeTo(t, ws, id, testKey)
	if answer.Op != "subbed" || answer.ID != id {
		t.Fatalf("expected to be subscribed, got %+v", answer)
	}
	return answer.Ch
}

// A page of another origin than the endpoint's is refused, unless a pattern admits it;
// a client of no origin, another host relaying, is not checked.
func TestOrigins(t *testing.T) {
	server, _ := startTestServer(t, func(string, bool) *camstream.Source { return nil }, WithOriginPatterns("https://*.example.com"))
	for origin, admitted := range map[string]bool{
		server.URL:                 true,
		"https://app.example.com":  true,
		"http://app.example.com":   false,
		"https://elsewhere.org":    false,
		"https://example.com.evil": false,
	} {
		if _, err := dialFrom(t, server, origin); (err == nil) != admitted {
			t.Errorf("%s: %v", origin, err)
		}
	}
	ws, _, err := websocket.Dial(context.Background(), urlOf(server), nil)
	if err != nil {
		t.Fatalf("no origin: %v", err)
	}
	ws.CloseNow()
}

func TestSubscriptionEndsOnUnsubscribe(t *testing.T) {
	server, cam, _ := streamServer(t)
	ws := dial(t, server)
	ch := subscribed(t, ws, 1)
	monitor := camstreamtest.NewMonitor()
	cam.Opens(monitor)
	sendOp(t, ws, map[string]any{"op": "unsub", "ch": ch})
	within(t, "the stream to close with its last viewer", monitor.Shut)
}

func TestSubscriptionsEndOnSocketClose(t *testing.T) {
	server, cam, _ := streamServer(t)
	ws := dial(t, server)
	for id := uint32(1); id <= 3; id++ {
		subscribed(t, ws, id)
	}
	monitor := camstreamtest.NewMonitor()
	cam.Opens(monitor)
	ws.CloseNow()
	within(t, "every subscription to end with the socket", monitor.Shut)
}

func TestSubscriptionsEndOnShutDown(t *testing.T) {
	server, cam, cancel := streamServer(t)
	ws := dial(t, server)
	subscribed(t, ws, 1)
	monitor := camstreamtest.NewMonitor()
	cam.Opens(monitor)
	cancel()
	within(t, "every subscription to end with the app", monitor.Shut)
}

func TestRefusesStreamItCannotServe(t *testing.T) {
	server, _ := startTestServer(t, func(string, bool) *camstream.Source { return nil })
	answer := subscribeTo(t, dial(t, server), 7, testKey)
	if answer.Op != "error" || answer.ID != 7 || answer.Key != testKey {
		t.Fatalf("expected %v to be refused, got %+v", testKey, answer)
	}
}

// A host that serves a stream to viewers alone refuses to relay it.
func TestRefusesARelayOfAStreamItServesViewersAlone(t *testing.T) {
	s := source(t, camstreamtest.NewCamera(t))
	server, _ := startTestServer(t, func(_ string, raw bool) *camstream.Source {
		if raw {
			return nil
		}
		return s
	})
	ws := dial(t, server)
	sendOp(t, ws, map[string]any{"op": "sub", "id": 7, "key": testKey, "raw": true})
	if answer := receive(t, ws); answer.Op != "error" || answer.ID != 7 {
		t.Fatalf("expected the relay to be refused, got %+v", answer)
	}
	if answer := subscribeTo(t, ws, 8, testKey); answer.Op != "subbed" {
		t.Fatalf("expected the viewer to be served, got %+v", answer)
	}
}

func TestChannelIsReusedAfterUnsubscribe(t *testing.T) {
	server, _, _ := streamServer(t)
	ws := dial(t, server)
	first := subscribed(t, ws, 1)
	sendOp(t, ws, map[string]any{"op": "unsub", "ch": first})
	if second := subscribed(t, ws, 2); second != first {
		t.Fatalf("expected channel %v to be handed out again, got %v", first, second)
	}
}

func TestResetOfAnUnknownChannelIsAnswered(t *testing.T) {
	server, _, _ := streamServer(t)
	ws := dial(t, server)
	sendOp(t, ws, map[string]any{"op": "reset", "ch": 9})
	answer := receive(t, ws)
	if answer.Op != "gone" || answer.Ch != 9 {
		t.Fatalf("expected the channel to be reported gone, got %+v", answer)
	}
}

// A stream that ends is reported gone, for the viewer to subscribe again.
func TestEndedStreamIsGone(t *testing.T) {
	ws, monitor, ch := startedStream(t)
	monitor.End()
	if answer := receive(t, ws); answer.Op != "gone" || answer.Ch != ch {
		t.Fatalf("expected the channel to be reported gone, got %+v", answer)
	}
}

func TestSlowViewerIsDroppedRatherThanBlocking(t *testing.T) {
	// A viewer that stops reading must not hold up the streams feeding it — one socket
	// carries every tile — so writes queue, and a queue that fills drops the viewer.
	c := newConnection(context.Background(), nil, slog.Default())
	written := make(chan struct{})
	go func() {
		defer close(written)
		payload := make([]byte, 64)
		for i := 0; i < outboundQueueSize+10; i++ {
			c.write(0, kindVideo, payload)
		}
	}()
	within(t, "writing to a viewer that never reads to return", written)
	within(t, "the connection to drop the viewer", c.done)
}

// keyFrame is a camera's key frame, Annex-B: its parameter sets, then its picture.
var keyFrame = func() []byte {
	nalus, err := camstreamtest.KeyFrame()
	if err != nil {
		panic(err)
	}
	return camstreamtest.AnnexB(nalus...)
}()

// videoFrame is a camera's key frame, or a frame of a single trailing slice.
func videoFrame(pts time.Duration, keyframe bool) *camstream.Frame {
	data := camstreamtest.AnnexB(camstreamtest.NALUnit(1))
	if keyframe {
		data = keyFrame
	}
	return &camstream.Frame{Kind: camstream.Video, Codec: camstream.H265, Keyframe: keyframe, PTS: pts, Duration: 40 * time.Millisecond, Data: data}
}

func audioFrame(pts time.Duration, fill byte) *camstream.Frame {
	return &camstream.Frame{Kind: camstream.Audio, Codec: camstream.PCMA, SampleRate: 8000, PTS: pts, Data: bytes.Repeat([]byte{fill}, 160)}
}

// receiveVideo receives the next binary frame, which must be the channel's video.
func receiveVideo(t *testing.T, ws *websocket.Conn, ch byte) []byte {
	t.Helper()
	message := receiveBinary(t, ws)
	if len(message) < 2 || message[0] != ch || message[1] != kindVideo {
		t.Fatalf("expected video on channel %v, got % x", ch, message[:min(len(message), 8)])
	}
	return message[2:]
}

// fragmentTiming reads a media segment's decode time and sample duration.
func fragmentTiming(t *testing.T, segment []byte) (dts uint64, duration uint32) {
	t.Helper()
	tfdt := bytes.Index(segment, []byte("tfdt"))
	tfhd := bytes.Index(segment, []byte("tfhd"))
	if tfdt < 0 || tfhd < 0 {
		t.Fatalf("not a media segment: % x", segment)
	}
	// tfdt: version 1, flags, 64-bit decode time. tfhd: version, flags, track, duration.
	return binary.BigEndian.Uint64(segment[tfdt+8:]), binary.BigEndian.Uint32(segment[tfhd+12:])
}

// startedStream is a viewer subscribed to testKey, whose stream is open on the monitor.
func startedStream(t *testing.T) (*websocket.Conn, *camstreamtest.Monitor, byte) {
	t.Helper()
	server, cam, _ := streamServer(t)
	ws := dial(t, server)
	ch := subscribed(t, ws, 1)
	monitor := camstreamtest.NewMonitor()
	cam.Opens(monitor)
	return ws, monitor, ch
}

// The viewer's timeline starts at the key frame it first gets, and runs on the
// stream's clock from there.
func TestVideoIsTimedFromTheFirstKeyFrame(t *testing.T) {
	ws, monitor, ch := startedStream(t)
	monitor.Frames <- videoFrame(9*time.Second, false)
	monitor.Frames <- videoFrame(10*time.Second, true)
	monitor.Frames <- videoFrame(10*time.Second+40*time.Millisecond, false)
	if init := receiveVideo(t, ws, ch); !bytes.Contains(init, []byte("moov")) {
		t.Fatalf("expected an initialization segment first, got % x", init[:16])
	}
	for _, want := range []uint64{0, 3600} {
		dts, duration := fragmentTiming(t, receiveVideo(t, ws, ch))
		if dts != want || duration != 3600 {
			t.Fatalf("segment at %d lasting %d, want %d lasting 3600", dts, duration, want)
		}
	}
}

func TestAudioGoesOutOnlyWhileOn(t *testing.T) {
	ws, monitor, ch := startedStream(t)
	// Each subscribe is answered only once every op before it has been handled.
	barrier := func(id uint32) {
		subscribed(t, ws, id)
	}
	monitor.Frames <- videoFrame(10*time.Second, true)
	monitor.Frames <- audioFrame(10*time.Second+20*time.Millisecond, 1)
	receiveVideo(t, ws, ch)
	receiveVideo(t, ws, ch)

	sendOp(t, ws, map[string]any{"op": "audio", "ch": ch, "on": true})
	barrier(2)
	monitor.Frames <- audioFrame(9*time.Second, 2)
	monitor.Frames <- audioFrame(10*time.Second+60*time.Millisecond, 3)
	message := receiveBinary(t, ws)
	want := []byte{ch, kindAudio, 0, 0, 0, 60, 0x1F, 0x40, 0}
	if !bytes.Equal(message[:len(want)], want) || !bytes.Equal(message[len(want):], bytes.Repeat([]byte{3}, 160)) {
		t.Fatalf("audio arrived as % x", message[:min(len(message), 12)])
	}

	sendOp(t, ws, map[string]any{"op": "audio", "ch": ch, "on": false})
	barrier(3)
	monitor.Frames <- audioFrame(10*time.Second+80*time.Millisecond, 4)
	monitor.Frames <- videoFrame(10*time.Second+40*time.Millisecond, false)
	receiveVideo(t, ws, ch)
}

// A relayed stream starts at its first key frame, and goes out frame by frame as it is,
// video and audio, flagged with whether the stream carries audio. A reset is no relay's.
func TestRawFramesFromTheFirstKeyFrame(t *testing.T) {
	server, cam, _ := streamServer(t)
	ws := dial(t, server)
	sendOp(t, ws, map[string]any{"op": "sub", "id": 1, "key": testKey, "raw": true})
	answer := receive(t, ws)
	if answer.Op != "subbed" {
		t.Fatalf("expected to be subscribed, got %+v", answer)
	}
	monitor := camstreamtest.NewMonitor()
	monitor.Audio = true
	cam.Opens(monitor)
	sent := []*camstream.Frame{videoFrame(10*time.Second, true), audioFrame(10*time.Second+20*time.Millisecond, 1), videoFrame(10*time.Second+40*time.Millisecond, false)}
	monitor.Frames <- videoFrame(9*time.Second, false)
	for _, f := range sent {
		monitor.Frames <- f
	}
	sendOp(t, ws, map[string]any{"op": "reset", "ch": answer.Ch})
	for _, want := range sent {
		message := receiveBinary(t, ws)
		if len(message) < 3 || message[0] != answer.Ch || message[1] != kindRaw || message[2] != rawAudio {
			t.Fatalf("expected a raw frame with audio on channel %v, got % x", answer.Ch, message[:min(len(message), 8)])
		}
		if got, err := parseFrame(message[3:]); err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("relayed %+v, %v, want %+v", got, err, want)
		}
	}
	// Each subscribe is answered only once every op before it has been handled, the reset
	// included, which had it been a viewer's would have been acknowledged first.
	if answer := subscribeTo(t, ws, 2, testKey); answer.Op != "subbed" {
		t.Fatalf("expected the next answer to be a subscription's, got %+v", answer)
	}
}

// A key frame the muxer cannot describe ends the view: the viewer is told it is gone, and
// nothing goes out on its channel.
func TestAKeyFrameOfNoParameterSetsEndsTheView(t *testing.T) {
	ws, monitor, ch := startedStream(t)
	monitor.Frames <- &camstream.Frame{Kind: camstream.Video, Codec: camstream.H265, Keyframe: true, PTS: time.Second, Data: camstreamtest.AnnexB(camstreamtest.NALUnit(19))}
	if answer := receive(t, ws); answer.Op != "gone" || answer.Ch != ch {
		t.Fatalf("expected the channel to be reported gone, got %+v", answer)
	}
}

func TestResetIsAcknowledged(t *testing.T) {
	ws, _, ch := startedStream(t)
	sendOp(t, ws, map[string]any{"op": "reset", "ch": ch})
	if message := receiveBinary(t, ws); !bytes.Equal(message, []byte{ch}) {
		t.Fatalf("expected a bare channel byte, got % x", message)
	}
}
