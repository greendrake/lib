// Package dvripcam is the core's ports over greendrake.nz/go/dvrip: the cameras that
// speak DVRIP (Sofia), streams and settings. It alone knows their configurations' names,
// shapes and encodings, and which of a family's variants a camera is, by what the camera
// reports of itself.
package dvripcam

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	"greendrake.nz/go/camstream"
	"greendrake.nz/go/dvrip"
	"greendrake.nz/go/ipcam"
	"greendrake.nz/go/ipcam/dvripcam/internal/monitor"
)

// channel is the one channel the cameras have.
const channel = 0

// stopTimeout bounds stopping a stream. A camera that leaves the Stop unanswered that
// long is gone: the abandoned request ends the login, and the connection dials again.
const stopTimeout = 5 * time.Second

// streamTypes are the streams by the core's keys.
var streamTypes = map[ipcam.StreamKey]dvrip.StreamType{ipcam.Main: dvrip.Main, ipcam.Extra: dvrip.Extra}

// Driver logs in to DVRIP cameras: an ipcam.SettingsDriver.
type Driver struct{}

// Dial logs in as DialSettings does.
func (d Driver) Dial(ctx context.Context, address, user, password string) (ipcam.Camera, error) {
	return d.DialSettings(ctx, address, user, password)
}

// DialSettings logs in, and reads the camera's report on itself, which a device that
// speaks some other protocol fails, and how it encodes its streams.
func (Driver) DialSettings(ctx context.Context, address, user, password string) (ipcam.Login, error) {
	session, err := dvrip.Dial(ctx, address, user, password)
	if err != nil {
		return nil, failure(err)
	}
	c, err := read(ctx, session)
	if err != nil {
		session.Close()
		return nil, failure(err)
	}
	return c, nil
}

// failure is why a login failed, as the core tells failures apart.
func failure(err error) error {
	var refused *dvrip.RetError
	var unreachable *net.OpError
	switch {
	case errors.As(err, &refused) && refused.IsCredentials():
		return fmt.Errorf("%w: %w", ipcam.ErrCredentials, err)
	case errors.As(err, &unreachable) || errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w: %w", ipcam.ErrUnreachable, err)
	}
	return fmt.Errorf("%w: %w", ipcam.ErrUnrecognised, err)
}

// capabilities are what the camera says it can do, read once a login, when the settings
// first need them. A description the camera does not keep leaves its part empty.
type capabilities struct {
	functions dvrip.Functions
	// By stream, for the streams it describes.
	resolutions map[ipcam.StreamKey][]string
	languages   []string
	standards   []string
}

// unlessAbsent passes through what a read answered, but a description the camera does
// not keep as none.
func unlessAbsent[T any](v T, err error) (T, error) {
	var refused *dvrip.RetError
	if errors.As(err, &refused) && refused.IsAbsent() {
		var none T
		return none, nil
	}
	return v, err
}

func capabilitiesOf(ctx context.Context, s *dvrip.Session) (capabilities, error) {
	var caps capabilities
	var err error
	if caps.functions, err = unlessAbsent(dvrip.SystemFunction.Get(ctx, s)); err != nil {
		return caps, err
	}
	encoding, err := unlessAbsent(dvrip.EncodeCapability.Get(ctx, s))
	if err != nil {
		return caps, err
	}
	caps.resolutions = map[ipcam.StreamKey][]string{}
	for key, stream := range streamTypes {
		described, err := encoding.Stream(stream)
		if err != nil {
			continue
		}
		if caps.resolutions[key], err = described.Resolutions(); err != nil {
			return caps, err
		}
	}
	if caps.languages, err = unlessAbsent(dvrip.MultiLanguage.Get(ctx, s)); err != nil {
		return caps, err
	}
	standards, err := unlessAbsent(dvrip.MultiVstd.Get(ctx, s))
	caps.standards = strings.FieldsFunc(standards, func(r rune) bool { return r == '|' })
	return caps, err
}

// selection is the camera's binding of each setting it has, and their catalogue.
type selection struct {
	bindings  map[ipcam.Key]*binding
	catalogue *ipcam.Catalogue
}

// selectionFor selects for a camera with caps: a key's binding is the first that holds.
func selectionFor(caps capabilities) selection {
	bound := map[ipcam.Key]*binding{}
	specifics := map[ipcam.Key]ipcam.Specifics{}
	for _, b := range bindings {
		if _, chosen := bound[b.key]; chosen || b.when != nil && !b.when(caps) {
			continue
		}
		bound[b.key] = b
		specifics[b.key] = ipcam.Specifics{}
		if b.specifics != nil {
			specifics[b.key] = b.specifics(caps)
		}
	}
	return selection{bindings: bound, catalogue: ipcam.NewCatalogue(xmeye, specifics)}
}

// bound are the selected bindings, in the table's order.
func (s selection) bound(yield func(*binding) bool) {
	for _, b := range bindings {
		if s.bindings[b.key] == b && !yield(b) {
			return
		}
	}
}

// read reads what a login keeps of the camera: what it is, which the streams need.
func read(ctx context.Context, s *dvrip.Session) (*login, error) {
	if _, err := s.SystemInfo(ctx); err != nil {
		return nil, err
	}
	encode, err := dvrip.SimplifyEncode.Get(ctx, s)
	if err == nil && len(encode) <= channel {
		err = fmt.Errorf("dvripcam: the camera encodes %d channels", len(encode))
	}
	if err != nil {
		return nil, err
	}
	l := &login{session: s, streams: map[ipcam.StreamKey]ipcam.StreamFormat{}}
	for key, stream := range streamTypes {
		format := encode[channel].Format(stream)
		l.streams[key] = ipcam.StreamFormat{Codec: codecOf(format.Video.Compression), Audio: format.AudioEnable}
	}
	return l, nil
}

// login is an ipcam.Camera, and its Settings, on a DVRIP session.
type login struct {
	session *dvrip.Session
	streams map[ipcam.StreamKey]ipcam.StreamFormat

	mu sync.Mutex
	// Nil until the settings first need it.
	chosen *selection
}

// selected is the login's selection, made from the camera's capabilities the first time
// the settings need it.
func (l *login) selected(ctx context.Context) (selection, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.chosen == nil {
		caps, err := capabilitiesOf(ctx, l.session)
		if err != nil {
			return selection{}, refusal(err)
		}
		chosen := selectionFor(caps)
		l.chosen = &chosen
	}
	return *l.chosen, nil
}

// refusal is the camera's refusal err reports, as the core has it; any other failure
// passes through.
func refusal(err error) error {
	var refused *dvrip.RetError
	if errors.As(err, &refused) {
		return &ipcam.Refused{Message: refused.Error()}
	}
	return err
}

func (l *login) Catalogue(ctx context.Context) (*ipcam.Catalogue, error) {
	s, err := l.selected(ctx)
	return s.catalogue, err
}

// Values reads each configuration the selected bindings of keys need, once. A
// configuration the camera does not keep leaves its settings out.
func (l *login) Values(ctx context.Context, keys []ipcam.Key) (map[ipcam.Key]any, error) {
	s, err := l.selected(ctx)
	if err != nil {
		return nil, err
	}
	configs := map[configuration]any{}
	values := map[ipcam.Key]any{}
	for b := range s.bound {
		if !slices.Contains(keys, b.key) {
			continue
		}
		config, read := configs[b.from]
		if !read {
			var err error
			if config, err = unlessAbsent(b.from.read(ctx, l.session)); err != nil {
				return nil, refusal(err)
			}
			configs[b.from] = config
		}
		if config == nil {
			continue
		}
		value, err := b.value(config)
		if err != nil {
			return nil, err
		}
		values[b.key] = value
	}
	return values, nil
}

// Apply writes the configuration the key's setting lives in, as the camera has it with
// the value set, does what the setting's write also does, and reads it back.
func (l *login) Apply(ctx context.Context, key ipcam.Key, value any) (ipcam.Applied, error) {
	s, err := l.selected(ctx)
	if err != nil {
		return ipcam.Applied{}, err
	}
	b, has := s.bindings[key]
	if !has {
		return ipcam.Applied{}, &ipcam.UnknownSetting{Key: key}
	}
	var then func(ctx context.Context, s *dvrip.Session) error
	if b.also != nil {
		if then, err = b.also(value); err != nil {
			return ipcam.Applied{}, err
		}
	}
	config, err := b.from.read(ctx, l.session)
	if err != nil {
		return ipcam.Applied{}, refusal(err)
	}
	from := b.from.address(config)
	if err := b.assign(config, value); err != nil {
		return ipcam.Applied{}, err
	}
	restart, err := b.from.write(ctx, l.session, config)
	if err != nil {
		return ipcam.Applied{}, refusal(err)
	}
	if then != nil {
		if err := then(ctx, l.session); err != nil {
			return ipcam.Applied{}, refusal(err)
		}
	}
	applied := ipcam.Applied{Restart: restart, Changed: map[ipcam.Key]any{}}
	// Where the camera answers once it applies config, if that moves it.
	moved := func(config any) {
		if to := b.from.address(config); to != from {
			applied.Address = to
		}
	}
	now, err := b.from.read(ctx, l.session)
	if err != nil {
		select {
		case <-l.session.Done():
			// The camera took the write and the login with it, as one that restarts its
			// network on an address change does: it answers where it was told to, and
			// what it has is read on the next login.
			moved(config)
			return applied, nil
		default:
			return ipcam.Applied{}, refusal(err)
		}
	}
	// Where it has its address, which can be other than where it was told to.
	moved(now)
	for other := range s.bound {
		if other.from != b.from {
			continue
		}
		if applied.Changed[other.key], err = other.value(now); err != nil {
			return ipcam.Applied{}, err
		}
	}
	return applied, nil
}

func (l *login) Clock(ctx context.Context) (time.Time, error) {
	t, err := l.session.Time(ctx)
	return t, refusal(err)
}

func (l *login) SetClock(ctx context.Context, t time.Time) error {
	return refusal(l.session.SetTime(ctx, t))
}

func (l *login) Reboot(ctx context.Context) error {
	return refusal(l.session.Reboot(ctx))
}

func (l *login) Streams() map[ipcam.StreamKey]ipcam.StreamFormat {
	return l.streams
}

// OpenStream monitors the stream on the login's session; shutting it down stops the
// monitor alone, leaving the session to the other stream and the settings.
func (l *login) OpenStream(ctx context.Context, key ipcam.StreamKey) (camstream.Monitor, error) {
	stream, err := l.session.Monitor(ctx, channel, streamTypes[key])
	if err != nil {
		return nil, refusal(err)
	}
	return monitor.New(stream, l.streams[key].Audio, func() {
		ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
		defer cancel()
		stream.Close(ctx)
	}), nil
}

func (l *login) Done() <-chan struct{} {
	return l.session.Done()
}

func (l *login) Err() error {
	return l.session.Err()
}

func (l *login) Close() {
	l.session.Close()
}
