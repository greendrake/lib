// Package ipcam is the camera engine's core: what it asks of a camera, through the ports
// Driver, Camera and Settings that each protocol's adapter implements, and the settings
// it edits, in terms of its own rather than any protocol's.
package ipcam

import (
	"context"
	"errors"
	"time"

	"greendrake.nz/go/camstream"
)

// Driver logs in to the cameras of one protocol. One whose cameras have settings is a
// SettingsDriver too.
type Driver interface {
	// Dial logs in to the camera at address. It fails with ErrCredentials,
	// ErrUnreachable or ErrUnrecognised.
	Dial(ctx context.Context, address, user, password string) (Camera, error)
}

// SettingsDriver is a Driver whose cameras have Settings.
type SettingsDriver interface {
	Driver
	// DialSettings logs in as Dial does, the login's settings with it.
	DialSettings(ctx context.Context, address, user, password string) (Login, error)
}

// Login is a SettingsDriver's: a login on a camera, and the camera's settings.
type Login interface {
	Camera
	Settings
}

// Camera is one login on a camera, until Done: its streams.
type Camera interface {
	// Each stream's codec and audio, as at login: a codec change applies once the camera
	// restarts.
	Streams() map[StreamKey]StreamFormat
	OpenStream(ctx context.Context, key StreamKey) (camstream.Monitor, error)
	// Closed when the login ends, with Err saying why.
	Done() <-chan struct{}
	Err() error
	Close()
}

// Settings are what a SettingsDriver's login has besides its streams. Their failures to
// do what they are asked are Refused.
type Settings interface {
	// The settings it has, described (NewCatalogue): its adapter's catalogue, with what
	// the camera adds to the description of each, the options of a choice it decides, a
	// note on its own behaviour. Fixed for the login; failing to tell fails the settings
	// alone, not the login or its streams.
	Catalogue(ctx context.Context) (*Catalogue, error)
	// The value of each of keys' settings it has, in its kind's form, read from the
	// camera as those settings alone need.
	Values(ctx context.Context, keys []Key) (map[Key]any, error)
	// Writes a value Check has made of what the setting's kind takes.
	Apply(ctx context.Context, key Key, value any) (Applied, error)
	// Its wall clock, in no zone: the camera keeps none.
	Clock(ctx context.Context) (time.Time, error)
	SetClock(ctx context.Context, t time.Time) error
	Reboot(ctx context.Context) error
}

// Applied answers a write.
type Applied struct {
	// The camera took it, but applies it only once restarted.
	Restart bool
	// Every setting the write can have changed, as the camera has them now: the camera
	// can keep another value than the one written.
	Changed map[Key]any
	// Where the camera answers from now on, if the write moved it; empty otherwise.
	Address string
}

// StreamKey names one of a camera's streams.
type StreamKey string

const (
	Main  StreamKey = "main"
	Extra StreamKey = "extra"
)

// StreamKeys are the streams every camera has.
var StreamKeys = []StreamKey{Main, Extra}

// StreamFormat is how a camera encodes a stream.
type StreamFormat struct {
	Codec Codec
	Audio bool
}

// Codec is a stream's codec, by its common name.
type Codec string

const (
	H264 Codec = "H.264"
	H265 Codec = "H.265"
)

// Cast is the codec the engine casts to browsers and records: a stream on any other is
// not opened.
const Cast = H265

// The failures to log in.
var (
	ErrCredentials  = errors.New("ipcam: the user or password was refused")
	ErrUnreachable  = errors.New("ipcam: unreachable")
	ErrUnrecognised = errors.New("ipcam: not answering as the driver's cameras do")
)

// Refused is the camera's refusal of what it was asked, in its words.
type Refused struct {
	Message string
}

func (r *Refused) Error() string {
	return r.Message
}
