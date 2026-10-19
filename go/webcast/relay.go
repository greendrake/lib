package webcast

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/coder/websocket"
	"greendrake.nz/go/camstream"
)

// defaultQuiet is the most silence a relay that plays keeps before it takes its host for
// gone, unless WithQuiet says otherwise: a host that is still there ends a stream gone
// quiet sooner (a camera's monitor gives up after a few seconds), and says so.
const defaultQuiet = 10 * time.Second

// relayReadLimit is the largest message a relay takes: a frame, a key frame of a 4K stream
// several times over.
const relayReadLimit = 16 << 20

// errGone is a relayed stream its host has stopped.
var errGone = errors.New("webcast: the host stopped the stream")

// RelayOption configures a Relay.
type RelayOption func(*relay)

// WithQuiet is the most silence a relay that plays keeps before it takes its host for
// gone, 10 seconds unless given.
func WithQuiet(d time.Duration) RelayOption {
	return func(r *relay) { r.quiet = d }
}

// relay is a stream relayed off another host's source of it, over a socket of its own.
type relay struct {
	ws    *websocket.Conn
	quiet time.Duration
	// The channel the stream comes on.
	ch byte
	// Whether the stream carries audio, set by the open.
	audio bool
	// The first frame, which the open reads, until Next has yielded it.
	first *camstream.Frame
}

// Relay opens the stream key names on the host whose Handler url (ws:// or wss://)
// reaches: its frames as they are, from a key frame on, off the host's source of it. It
// opens once the first frame has come, which says whether the stream carries audio,
// waiting for as long as ctx lets it: the host is rightly silent while its camera is away,
// and a host gone meanwhile is found by the socket's TCP keep-alives. ctx bounds the
// opening alone.
func Relay(ctx context.Context, url, key string, opts ...RelayOption) (camstream.Monitor, error) {
	ws, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	ws.SetReadLimit(relayReadLimit)
	r := &relay{ws: ws, quiet: defaultQuiet}
	for _, opt := range opts {
		opt(r)
	}
	if err := r.subscribe(ctx, url, key); err != nil {
		r.ShutDown()
		// A read the context ended fails for the socket it closed, not for the context.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return r, nil
}

// subscribe asks for the stream, raw, and reads its first frame.
func (r *relay) subscribe(ctx context.Context, url, key string) error {
	op, err := json.Marshal(clientOp{Op: "sub", Key: key, Raw: true})
	if err != nil {
		return err
	}
	if err := r.ws.Write(ctx, websocket.MessageText, op); err != nil {
		return err
	}
	var answer clientOp
	if err := r.receive(ctx, 0, func(kind websocket.MessageType, data []byte) (bool, error) {
		return true, json.Unmarshal(data, &answer)
	}); err != nil {
		return err
	}
	if answer.Op != "subbed" {
		return fmt.Errorf("webcast: %s refused the stream %s", url, key)
	}
	r.ch = answer.Ch
	r.first, r.audio, err = r.next(ctx, 0)
	return err
}

// receive reads messages off the socket until take has had one it wants or fails, each
// within quiet of the last where quiet is set, all within ctx.
func (r *relay) receive(ctx context.Context, quiet time.Duration, take func(websocket.MessageType, []byte) (bool, error)) error {
	for {
		readCtx, cancel := ctx, context.CancelFunc(func() {})
		if quiet > 0 {
			readCtx, cancel = context.WithTimeout(ctx, quiet)
		}
		kind, data, err := r.ws.Read(readCtx)
		cancel()
		if err != nil {
			return err
		}
		if done, err := take(kind, data); done || err != nil {
			return err
		}
	}
}

// next reads the stream's next frame, each message within quiet of the last where quiet is
// set, and whether the stream carries audio: of the ops, gone alone is the stream's.
func (r *relay) next(ctx context.Context, quiet time.Duration) (*camstream.Frame, bool, error) {
	var f *camstream.Frame
	var audio bool
	err := r.receive(ctx, quiet, func(kind websocket.MessageType, data []byte) (bool, error) {
		if kind == websocket.MessageText {
			var op clientOp
			if err := json.Unmarshal(data, &op); err != nil {
				return false, err
			}
			if op.Op == "gone" && op.Ch == r.ch {
				return false, errGone
			}
			return false, nil
		}
		if len(data) < 3 || data[0] != r.ch || data[1] != kindRaw {
			return false, fmt.Errorf("webcast: % x is no frame relayed", data[:min(len(data), 3)])
		}
		audio = data[2]&rawAudio != 0
		var err error
		f, err = parseFrame(data[3:])
		return true, err
	})
	return f, audio, err
}

func (r *relay) Next() (*camstream.Frame, error) {
	if f := r.first; f != nil {
		r.first = nil
		return f, nil
	}
	f, _, err := r.next(context.Background(), r.quiet)
	return f, err
}

// HasAudio is the flag the first frame came with: a stream's audio is fixed while it
// plays.
func (r *relay) HasAudio() bool {
	return r.audio
}

// ShutDown closes the socket, which ends the subscription on the host.
func (r *relay) ShutDown() {
	r.ws.CloseNow()
}
