// Package ipcamtest is cameras in memory behind the core's ports, for the tests of what is
// built on them: a SettingsDriver's, whose cameras have the core's settings, and a
// Driver's, whose cameras have streams alone. Their streams are tested over a protocol's
// adapter.
package ipcamtest

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
	"testing"
	"time"

	"greendrake.nz/go/camstream"
	"greendrake.nz/go/ipcam"
)

// Seed is what a camera has, and how it answers writes.
type Seed struct {
	Specifics map[ipcam.Key]ipcam.Specifics
	Values    map[ipcam.Key]any
	Streams   map[ipcam.StreamKey]ipcam.StreamFormat
	// The settings whose writes take a restart.
	Restart map[ipcam.Key]bool
	// The settings whose writes the camera refuses, with what it says.
	Refuse map[ipcam.Key]string
	// The values the camera keeps whatever a setting is written as.
	Keeps map[ipcam.Key]any
	// Where a write of a setting moves the camera to.
	Moves map[ipcam.Key]string
	// Refuses every login the password, as a camera does a password it does not take.
	Refuses bool
}

// Write is a value a camera was given.
type Write struct {
	Key   ipcam.Key
	Value any
}

// Driver reaches the cameras added to it, by address: cameras with streams alone, as
// RTSP cameras are.
type Driver struct {
	t       testing.TB
	mu      sync.Mutex
	cameras map[string]*Camera
}

func NewDriver(t testing.TB) *Driver {
	return &Driver{t: t, cameras: map[string]*Camera{}}
}

// SettingsDriver is a Driver whose cameras have settings.
type SettingsDriver struct {
	*Driver
}

func NewSettingsDriver(t testing.TB) SettingsDriver {
	return SettingsDriver{NewDriver(t)}
}

// Add puts a camera with seed at address.
func (d *Driver) Add(address string, seed Seed) *Camera {
	d.mu.Lock()
	defer d.mu.Unlock()
	c := &Camera{t: d.t, seed: seed, values: maps.Clone(seed.Values)}
	d.cameras[address] = c
	return c
}

func (d *Driver) Dial(_ context.Context, address, _, _ string) (ipcam.Camera, error) {
	return d.login(address)
}

func (d SettingsDriver) DialSettings(_ context.Context, address, _, _ string) (ipcam.Login, error) {
	return d.login(address)
}

func (d *Driver) login(address string) (ipcam.Login, error) {
	d.mu.Lock()
	c, known := d.cameras[address]
	d.mu.Unlock()
	if !known {
		return nil, fmt.Errorf("%w: no camera at %s", ipcam.ErrUnreachable, address)
	}
	return c.login()
}

// Camera is one in memory: its values and clock outlive its logins, and a reboot ends
// every login and keeps it away until Return.
type Camera struct {
	t       testing.TB
	seed    Seed
	mu      sync.Mutex
	values  map[ipcam.Key]any
	clock   time.Time
	away    bool
	logins  []*login
	dials   int
	written []Write
}

// Dials is how many times the camera has been dialled.
func (c *Camera) Dials() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dials
}

// Written is what the camera was given, in order.
func (c *Camera) Written() []Write {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.written
}

// Return brings the camera back from a reboot.
func (c *Camera) Return() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.away = false
}

func (c *Camera) login() (ipcam.Login, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dials++
	if c.seed.Refuses {
		return nil, fmt.Errorf("%w: wrong password", ipcam.ErrCredentials)
	}
	if c.away {
		return nil, fmt.Errorf("%w: rebooting", ipcam.ErrUnreachable)
	}
	l := &login{camera: c, done: make(chan struct{})}
	c.logins = append(c.logins, l)
	return l, nil
}

// login is a Camera's: an ipcam.Login, until it ends.
type login struct {
	camera *Camera
	done   chan struct{}
	end    sync.Once
	err    error
}

// errEnded answers what is asked of a login that has ended.
var errEnded = errors.New("ipcamtest: the login has ended")

func (l *login) finish(err error) {
	l.end.Do(func() {
		l.err = err
		close(l.done)
	})
}

// locked runs f on the login's camera under its lock, or answers errEnded.
func locked[T any](l *login, f func(c *Camera) (T, error)) (T, error) {
	select {
	case <-l.done:
		var none T
		return none, errEnded
	default:
	}
	l.camera.mu.Lock()
	defer l.camera.mu.Unlock()
	return f(l.camera)
}

// Catalogue is the core's, of the settings the seed's Specifics names.
func (l *login) Catalogue(context.Context) (*ipcam.Catalogue, error) {
	return locked(l, func(c *Camera) (*ipcam.Catalogue, error) {
		return ipcam.NewCatalogue(ipcam.Extension{}, c.seed.Specifics), nil
	})
}

func (l *login) Values(_ context.Context, keys []ipcam.Key) (map[ipcam.Key]any, error) {
	return locked(l, func(c *Camera) (map[ipcam.Key]any, error) {
		values := map[ipcam.Key]any{}
		for _, key := range keys {
			if v, has := c.values[key]; has {
				values[key] = v
			}
		}
		return values, nil
	})
}

func (l *login) Apply(_ context.Context, key ipcam.Key, value any) (ipcam.Applied, error) {
	return locked(l, func(c *Camera) (ipcam.Applied, error) {
		if message, refused := c.seed.Refuse[key]; refused {
			return ipcam.Applied{}, &ipcam.Refused{Message: message}
		}
		c.written = append(c.written, Write{Key: key, Value: value})
		c.values[key] = value
		if kept, keeps := c.seed.Keeps[key]; keeps {
			c.values[key] = kept
		}
		return ipcam.Applied{Restart: c.seed.Restart[key], Changed: map[ipcam.Key]any{key: c.values[key]}, Address: c.seed.Moves[key]}, nil
	})
}

func (l *login) Clock(context.Context) (time.Time, error) {
	return locked(l, func(c *Camera) (time.Time, error) {
		return c.clock, nil
	})
}

func (l *login) SetClock(_ context.Context, t time.Time) error {
	_, err := locked(l, func(c *Camera) (struct{}, error) {
		c.clock = t
		return struct{}{}, nil
	})
	return err
}

// Reboot ends every login, and keeps the camera away until Return.
func (l *login) Reboot(context.Context) error {
	_, err := locked(l, func(c *Camera) (struct{}, error) {
		c.away = true
		for _, other := range c.logins {
			other.finish(errors.New("ipcamtest: rebooted"))
		}
		c.logins = nil
		return struct{}{}, nil
	})
	return err
}

func (l *login) Streams() map[ipcam.StreamKey]ipcam.StreamFormat {
	return l.camera.seed.Streams
}

// OpenStream fails the test: streams are tested over a protocol's adapter.
func (l *login) OpenStream(context.Context, ipcam.StreamKey) (camstream.Monitor, error) {
	l.camera.t.Error("ipcamtest: a stream was opened on a camera in memory")
	return nil, errors.New("ipcamtest: no streams")
}

func (l *login) Done() <-chan struct{} {
	return l.done
}

func (l *login) Err() error {
	return l.err
}

func (l *login) Close() {
	l.finish(errEnded)
}
