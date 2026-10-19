package webcast

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/bluenviron/mediacommon/pkg/codecs/h264"
	"github.com/coder/websocket"
	"greendrake.nz/go/camstream"
	"greendrake.nz/go/webcast/internal/muxer"
)

// What a binary frame's payload is.
const (
	kindVideo byte = 0
	kindAudio byte = 1
	kindRaw   byte = 2
)

// rawAudio is the flag of a raw payload whose stream carries audio.
const rawAudio byte = 1

// The codec byte of an audio payload.
var audioCodecs = map[camstream.Codec]byte{
	camstream.PCMA: 0,
	camstream.PCMU: 1,
}

const (
	// The fMP4 timeline's clock rate.
	clockRate uint32 = 90000
	// A write that cannot complete in this long means the socket is dead rather than
	// merely behind; being behind is what the outbound queue is for.
	writeTimeout = 5 * time.Second
	// How far behind a viewer may fall before it is dropped. Every channel feeds this
	// one queue, so it also bounds the memory one browser can make the server hold:
	// roughly a second of a full grid.
	outboundQueueSize = 256
	// One byte carries the channel, so this is how many streams a single socket can
	// serve at once. The grid needs one per tile.
	maxChannels = 256
)

// connection is one browser's socket. It multiplexes any number of streams over it,
// which is what keeps a page of tiles off the browser's one-connection-per-host
// handshake queue.
//
// The wire protocol is JSON text frames from the client:
//
//	{"op":"sub","id":7,"key":"main"}  -> {"op":"subbed","id":7,"ch":3}
//	                                    {"op":"error","id":7,"key":"main"}
//	{"op":"unsub","ch":3}
//	{"op":"reset","ch":3}
//	{"op":"audio","ch":3,"on":true}
//
// and binary frames from the server, each [channel byte][kind byte][payload]. Kind 0 is
// fMP4: an initialization segment, then media segments of one frame each. Kind 1 is a
// frame of the camera's audio, sent only while the viewer has it on:
// [u32 ms][u16 sample rate][u8 codec][samples], big-endian, due that many milliseconds
// into the fMP4 timeline, codec 0 for G.711 A-law and 1 for µ-law. A frame of just the
// channel byte acknowledges a reset: the next payload on that channel is a fresh
// initialization segment, and the timeline starts again from it. {"op":"gone","ch":3}
// says a channel has stopped and the viewer should subscribe again. A key names a
// stream as the host chooses to.
//
// A sub with "raw":true is another host relaying the stream (Relay): from the first key
// frame on, every frame, video and audio, goes out as it is, kind 2,
// [u8 flags][appendFrame], flag 1 set when the stream carries audio. A raw channel takes
// no reset or audio.
type connection struct {
	lookup Lookup
	logger *slog.Logger
	ws     *websocket.Conn
	// Ends with the connection, and every subscription and socket operation on it with it.
	ctx    context.Context
	cancel context.CancelFunc
	// Everything bound for the socket queues here for the writer goroutine, which is
	// the only thing that writes to it: a channel must never wait on one viewer's link,
	// or a single slow browser would stall every channel on it.
	out chan outbound
	// Closed by shutDown, once.
	done chan struct{}
	// Closed once the writer goroutine has finished with the socket.
	written chan struct{}
	views   map[byte]*view
	// Guards views and closed.
	viewsMutex sync.Mutex
	closed     bool
}

// outbound is a message bound for the socket.
type outbound struct {
	kind websocket.MessageType
	data []byte
}

type clientOp struct {
	Op  string `json:"op"`
	ID  uint32 `json:"id"`
	Key string `json:"key"`
	Ch  byte   `json:"ch"`
	On  bool   `json:"on"`
	Raw bool   `json:"raw"`
}

// newConnection makes a connection serving the streams lookup finds, until ctx ends.
func newConnection(ctx context.Context, lookup Lookup, logger *slog.Logger) *connection {
	ctx, cancel := context.WithCancel(ctx)
	return &connection{
		lookup:  lookup,
		logger:  logger,
		ctx:     ctx,
		cancel:  cancel,
		out:     make(chan outbound, outboundQueueSize),
		done:    make(chan struct{}),
		written: make(chan struct{}),
		views:   make(map[byte]*view),
	}
}

// serve runs the socket's read loop and returns when the socket dies or the connection
// ends, which is what keeps the HTTP handler alive for as long as the viewer is watching.
func (c *connection) serve(ws *websocket.Conn) {
	c.ws = ws
	go c.writeLoop()
	defer func() {
		c.shutDown()
		// The writer has to be off the socket before it closes: a close written into the
		// middle of a media frame would be the last thing the viewer ever decoded.
		<-c.written
		ws.CloseNow()
	}()
	for {
		// A read ends with the connection's context, closing the socket.
		_, message, err := ws.Read(c.ctx)
		if err != nil {
			return
		}
		var op clientOp
		if err := json.Unmarshal(message, &op); err != nil {
			continue
		}
		switch op.Op {
		case "sub":
			c.subscribe(op)
		case "unsub":
			c.unsubscribe(op.Ch)
		case "reset":
			c.reset(op.Ch)
		case "audio":
			c.setAudio(op.Ch, op.On)
		}
	}
}

// writeLoop is the one writer of the socket. A write ends with the connection's context,
// which shutDown ends at once rather than after a write in flight has run its course.
func (c *connection) writeLoop() {
	defer close(c.written)
	for {
		select {
		case message := <-c.out:
			ctx, cancel := context.WithTimeout(c.ctx, writeTimeout)
			err := c.ws.Write(ctx, message.kind, message.data)
			cancel()
			if err != nil {
				c.shutDown()
				return
			}
		case <-c.done:
			return
		}
	}
}

func (c *connection) subscribe(op clientOp) {
	source := c.lookup(op.Key, op.Raw)
	if source == nil {
		// The stream is not served, or has nothing to give right now (its camera is off
		// the codec the host casts); either way the viewer should come back.
		c.refuse(op)
		return
	}
	ctx, cancel := context.WithCancel(c.ctx)
	v := &view{conn: c, key: op.Key, cancel: cancel, raw: op.Raw}
	c.viewsMutex.Lock()
	ch, free := c.freeChannel()
	if free {
		v.ch = ch
		c.views[ch] = v
	}
	c.viewsMutex.Unlock()
	if !free {
		cancel()
		c.refuse(op)
		return
	}
	// The channel has to reach the viewer before any frame carrying it does, and both
	// travel the one outbound queue, so answer before subscribing.
	c.sendJSON(map[string]any{"op": "subbed", "id": op.ID, "ch": ch})
	go v.watch(source.Subscribe(ctx))
}

// freeChannel returns the lowest channel not in use. False means there is none to be
// had — including because the connection is finished, which must gain no subscriptions.
// Callers hold viewsMutex.
func (c *connection) freeChannel() (byte, bool) {
	if c.closed {
		return 0, false
	}
	for i := 0; i < maxChannels; i++ {
		if _, taken := c.views[byte(i)]; !taken {
			return byte(i), true
		}
	}
	return 0, false
}

// unsubscribe ends a channel. Its writes are suppressed at once, so the channel can be
// handed to a new subscription straight away. The viewer asked for this, so it gets no
// "gone".
func (c *connection) unsubscribe(ch byte) {
	c.viewsMutex.Lock()
	v := c.views[ch]
	delete(c.views, ch)
	c.viewsMutex.Unlock()
	if v != nil {
		v.suppress()
		v.cancel()
	}
}

func (c *connection) view(ch byte) *view {
	c.viewsMutex.Lock()
	defer c.viewsMutex.Unlock()
	return c.views[ch]
}

func (c *connection) reset(ch byte) {
	v := c.view(ch)
	if v == nil {
		c.sendGone(ch)
		return
	}
	v.reset()
}

// setAudio needs no answer: a channel no longer subscribed has been reported gone, or
// was unsubscribed by the viewer itself.
func (c *connection) setAudio(ch byte, on bool) {
	if v := c.view(ch); v != nil {
		v.setAudio(on)
	}
}

// forget drops a channel whose stream stopped underneath it, and tells the viewer so
// that it can subscribe again rather than wait out a timeout.
func (c *connection) forget(v *view) {
	c.viewsMutex.Lock()
	live := c.views[v.ch] == v
	if live {
		delete(c.views, v.ch)
	}
	c.viewsMutex.Unlock()
	if live {
		v.cancel()
		c.sendGone(v.ch)
	}
}

// sendGone tells the viewer a channel is no longer being served, so that it subscribes
// again rather than waiting out a timeout on a stream that will never arrive.
func (c *connection) sendGone(ch byte) {
	c.sendJSON(map[string]any{"op": "gone", "ch": ch})
}

func (c *connection) refuse(op clientOp) {
	c.sendJSON(map[string]any{"op": "error", "id": op.ID, "key": op.Key})
}

// write queues one binary frame: the channel, what the payload is, and the payload.
func (c *connection) write(ch, kind byte, payload []byte) {
	message := make([]byte, len(payload)+2)
	message[0] = ch
	message[1] = kind
	copy(message[2:], payload)
	c.send(outbound{websocket.MessageBinary, message})
}

func (c *connection) sendJSON(message map[string]any) {
	data, err := json.Marshal(message)
	if err == nil {
		c.send(outbound{websocket.MessageText, data})
	}
}

// send queues one message for the writer. A full queue means the viewer is a
// grid-second behind and not catching up, so it is dropped and left to reconnect.
// Queueing never blocks, which is what keeps one channel's slow link out of every other
// channel's way.
func (c *connection) send(message outbound) {
	select {
	case c.out <- message:
	case <-c.done:
	default:
		c.shutDown()
	}
}

// shutDown ends the connection and every subscription on it. Nothing it does waits on a
// channel: a frame being written when it runs goes nowhere, the socket closing under it.
func (c *connection) shutDown() {
	c.viewsMutex.Lock()
	if c.closed {
		c.viewsMutex.Unlock()
		return
	}
	c.closed = true
	clear(c.views)
	c.viewsMutex.Unlock()
	close(c.done)
	c.cancel()
}

// view is one stream on one connection: a subscription to the stream's source, muxed
// for the viewer, or relayed as it is. Each needs its own muxer, because a viewer joining
// has to receive what the muxer produces from the beginning, not from part way through.
type view struct {
	conn *connection
	ch   byte
	// The stream's key, as the viewer subscribed to it.
	key string
	// Ends the subscription.
	cancel context.CancelFunc
	// Set for another host relaying the stream, which takes its frames as they are.
	raw   bool
	muxer *muxer.Muxer
	// Where the viewer's timeline starts on the stream's clock: the key frame its
	// initialization segment was made from. Set once started.
	start time.Duration
	// Set once the first key frame has gone out.
	started bool
	// Whether the viewer has its audio on.
	audio bool
	// Set once the view must not write: when the viewer unsubscribes, its channel then
	// whoever takes it next's, and when the view has failed.
	suppressed bool
	// Guards the fields above, and is held across the handover of a frame to the
	// connection, so that suppression takes effect on a frame boundary.
	stateMutex sync.Mutex
}

// watch sends the subscription's frames to the viewer until it ends.
func (v *view) watch(sub *camstream.Subscription) {
	for f := range sub.Frames() {
		// Known now the subscription has delivered a frame.
		v.send(f, sub.HasAudio())
	}
	v.conn.forget(v)
}

func (v *view) send(f *camstream.Frame, audio bool) {
	// The video's NAL units, which the muxer takes.
	var nalus h264.AnnexB
	var unreadable error
	if f.Kind == camstream.Video && !v.raw {
		unreadable = nalus.Unmarshal(f.Data)
	}
	v.stateMutex.Lock()
	defer v.stateMutex.Unlock()
	switch {
	case v.suppressed:
	case v.raw:
		v.sendRaw(f, audio)
	case unreadable != nil:
		v.fail(unreadable)
	case f.Kind == camstream.Video:
		v.sendVideo(f, nalus)
	default:
		v.sendAudio(f)
	}
}

// fail ends the view on a frame it cannot cast: nothing more goes out on its channel, and
// its subscription is cancelled, which tells the viewer the stream is gone. Callers hold
// stateMutex.
func (v *view) fail(err error) {
	v.conn.logger.Warn("webcast: a stream cannot be cast", "key", v.key, "err", err)
	v.suppressed = true
	v.cancel()
}

// sendRaw relays a frame as it is, from the first key frame on, so that the relaying
// host's stream starts as one off a camera does.
func (v *view) sendRaw(f *camstream.Frame, audio bool) {
	v.started = v.started || f.Kind == camstream.Video && f.Keyframe
	if !v.started {
		return
	}
	flags := byte(0)
	if audio {
		flags |= rawAudio
	}
	v.conn.write(v.ch, kindRaw, appendFrame([]byte{flags}, f))
}

// sendVideo puts a video frame on the fMP4 timeline. The browser plays it at the
// stream's own pace; keeping close to live is the player's business.
func (v *view) sendVideo(f *camstream.Frame, nalus [][]byte) {
	if f.Keyframe && !v.started {
		m := &muxer.Muxer{}
		init, err := m.GetInit(nalus, clockRate)
		if err != nil {
			v.fail(err)
			return
		}
		v.muxer, v.started, v.start = m, true, f.PTS
		v.conn.write(v.ch, kindVideo, init)
	}
	if !v.started {
		return
	}
	payload, err := h264.AVCC(nalus).Marshal()
	if err != nil {
		v.fail(err)
		return
	}
	v.conn.write(v.ch, kindVideo, v.muxer.GetPayload(0, payload, f.Keyframe, ticks(f.PTS-v.start), uint32(ticks(f.Duration))))
}

// sendAudio passes an audio frame on as it came, stamped on the fMP4 timeline: browsers
// play G.711 only through code of their own.
func (v *view) sendAudio(f *camstream.Frame) {
	if !v.audio || !v.started || f.PTS < v.start {
		return
	}
	payload := binary.BigEndian.AppendUint32(nil, uint32((f.PTS-v.start)/time.Millisecond))
	payload = binary.BigEndian.AppendUint16(payload, uint16(f.SampleRate))
	payload = append(payload, audioCodecs[f.Codec])
	v.conn.write(v.ch, kindAudio, append(payload, f.Data...))
}

// ticks converts a span of the stream's clock to the fMP4 timeline's.
func ticks(d time.Duration) uint64 {
	return uint64(d/time.Microsecond) * uint64(clockRate) / uint64(time.Second/time.Microsecond)
}

func (v *view) setAudio(on bool) {
	v.stateMutex.Lock()
	defer v.stateMutex.Unlock()
	v.audio = on
}

func (v *view) reset() {
	v.stateMutex.Lock()
	defer v.stateMutex.Unlock()
	if v.suppressed || v.raw {
		return
	}
	v.started = false
	v.muxer = nil
	// A bare channel byte tells the viewer where the old stream ended and to expect a
	// fresh initialization segment next.
	v.conn.send(outbound{websocket.MessageBinary, []byte{v.ch}})
}

// suppress silences the view for good. Once it returns no further frame can go out on
// its channel: a frame in flight has gone, since its handover holds the same lock.
func (v *view) suppress() {
	v.stateMutex.Lock()
	defer v.stateMutex.Unlock()
	v.suppressed = true
}
