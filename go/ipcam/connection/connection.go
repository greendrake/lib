// Package connection keeps a camera logged in: one login for its settings and every
// stream, dialled again whenever it ends, with the state of it pushed. Every read and
// write of the settings goes through it, checked against what the camera's settings take
// before the camera is asked.
package connection

import (
	"context"
	"errors"
	"sync"
	"time"

	"greendrake.nz/go/ipcam"
)

const (
	// dialTimeout bounds logging in: a camera slower than that is as good as offline.
	dialTimeout = 5 * time.Second
	// redial is how long after a failed dial the camera is dialled again. It is on the
	// LAN, so a refused dial costs nothing, and coming back is noticed within it.
	redial = time.Second
)

var (
	// ErrOffline answers a call made while the camera is offline.
	ErrOffline = errors.New("connection: the camera is offline")
	// ErrNoSettings answers a settings call on a camera whose driver has no settings.
	ErrNoSettings = errors.New("connection: the camera has no settings")
)

// Status is whether the camera is logged in to.
type Status string

const (
	Online  Status = "online"
	Offline Status = "offline"
)

// State is the connection's, as the browser is told it.
type State struct {
	State   Status
	Address string
	// Why the last dial failed, while offline.
	Error string
	// Whether the camera has settings: its driver's, so known while offline too.
	Settings bool
	// Set by a write the camera takes only once restarted; the next login clears it.
	RestartPending bool
	// Each stream as the camera encoded it at the last login: a codec change applies
	// only once the camera restarts. None before the first.
	Streams map[ipcam.StreamKey]StreamState
	// The codec the engine casts.
	Cast ipcam.Codec
}

// StreamState is how the camera encodes a stream, and whether the engine casts it.
type StreamState struct {
	Codec ipcam.Codec
	Cast  bool
}

// Connection is a camera's login.
type Connection struct {
	// Logs in to the camera at address, its settings with the login where its driver is
	// an ipcam.SettingsDriver.
	logIn func(ctx context.Context, address string) (ipcam.Camera, ipcam.Settings, error)
	// Signalled by a change of the state that Run is not the one to make.
	changed chan struct{}

	mu      sync.Mutex
	address string
	camera  ipcam.Camera
	// The camera's settings, while online on a SettingsDriver.
	settings ipcam.Settings
	state    State
	// Closed while online, and replaced by an open one on going offline: what Await
	// waits on.
	online chan struct{}
}

func newConnection(driver ipcam.Driver, address, user, password string) *Connection {
	logIn := func(ctx context.Context, address string) (ipcam.Camera, ipcam.Settings, error) {
		cam, err := driver.Dial(ctx, address, user, password)
		return cam, nil, err
	}
	settings, configurable := driver.(ipcam.SettingsDriver)
	if configurable {
		logIn = func(ctx context.Context, address string) (ipcam.Camera, ipcam.Settings, error) {
			login, err := settings.DialSettings(ctx, address, user, password)
			return login, login, err
		}
	}
	return &Connection{
		logIn:   logIn,
		changed: make(chan struct{}, 1),
		address: address,
		state:   State{State: Offline, Address: address, Settings: configurable, Cast: ipcam.Cast},
		online:  make(chan struct{}),
	}
}

// Dial logs in to the camera at address through driver, failing as the driver does.
func Dial(ctx context.Context, driver ipcam.Driver, address, user, password string) (*Connection, error) {
	c := newConnection(driver, address, user, password)
	cam, settings, err := c.login(ctx)
	if err != nil {
		return nil, err
	}
	c.up(cam, settings)
	return c, nil
}

// Start is the connection to the camera at address through driver, offline until Run
// has dialled it.
func Start(driver ipcam.Driver, address, user, password string) *Connection {
	return newConnection(driver, address, user, password)
}

func (c *Connection) login(ctx context.Context) (ipcam.Camera, ipcam.Settings, error) {
	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	c.mu.Lock()
	address := c.address
	c.mu.Unlock()
	return c.logIn(ctx, address)
}

// up takes a fresh login, and its settings where its driver has them.
func (c *Connection) up(cam ipcam.Camera, settings ipcam.Settings) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.camera = cam
	c.settings = settings
	streams := map[ipcam.StreamKey]StreamState{}
	for key, format := range cam.Streams() {
		streams[key] = StreamState{Codec: format.Codec, Cast: format.Codec == ipcam.Cast}
	}
	c.state = State{State: Online, Address: c.address, Settings: c.state.Settings, Streams: streams, Cast: ipcam.Cast}
	close(c.online)
}

// down records the login's end.
func (c *Connection) down(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.camera = nil
	c.settings = nil
	c.online = make(chan struct{})
	c.state.State = Offline
	c.state.Error = err.Error()
}

// failed records a dial that failed while offline, and says whether that changed the
// state.
func (c *Connection) failed(err error) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	changed := c.state.Error != err.Error()
	c.state.Error = err.Error()
	return changed
}

// Run keeps the camera connected until ctx ends, pushing the state as it starts and on
// every change. While offline it dials the camera once a second until it answers, at
// once if it starts offline; a refused password leaves it offline, with the refusal, and
// dials no more. It closes the login on its way out.
func (c *Connection) Run(ctx context.Context, push func(State)) {
	push(c.State())
	wait := time.Duration(0)
	for {
		c.mu.Lock()
		cam := c.camera
		c.mu.Unlock()
		if cam == nil {
			if !c.dial(ctx, push, wait) {
				return
			}
			continue
		}
		select {
		case <-ctx.Done():
			cam.Close()
			return
		case <-c.changed:
			push(c.State())
		case <-cam.Done():
			c.down(cam.Err())
			push(c.State())
			wait = redial
		}
	}
}

// dial dials, the first time after wait, until a login succeeds, or reports ctx's end.
func (c *Connection) dial(ctx context.Context, push func(State), wait time.Duration) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(wait):
		}
		wait = redial
		cam, settings, err := c.login(ctx)
		if ctx.Err() != nil {
			if err == nil {
				cam.Close()
			}
			return false
		}
		if err == nil {
			c.up(cam, settings)
			push(c.State())
			return true
		}
		if c.failed(err) {
			push(c.State())
		}
		if errors.Is(err, ipcam.ErrCredentials) {
			<-ctx.Done()
			return false
		}
	}
}

// Await waits for the camera to be online, or ctx to end.
func (c *Connection) Await(ctx context.Context) (ipcam.Camera, error) {
	for {
		c.mu.Lock()
		cam, online := c.camera, c.online
		c.mu.Unlock()
		if cam != nil {
			return cam, nil
		}
		select {
		case <-online:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// State is the connection's state now. A state's Streams map is made anew with each
// login and never changed, so copies share it.
func (c *Connection) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Configurable answers the camera's settings at once: ErrNoSettings on a camera whose
// driver has none, ErrOffline while offline.
func (c *Connection) Configurable() (ipcam.Settings, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case !c.state.Settings:
		return nil, ErrNoSettings
	case c.settings == nil:
		return nil, ErrOffline
	}
	return c.settings, nil
}

// describing answers the camera's settings, and their catalogue.
func (c *Connection) describing(ctx context.Context) (ipcam.Settings, *ipcam.Catalogue, error) {
	settings, err := c.Configurable()
	if err != nil {
		return nil, nil, err
	}
	catalogue, err := settings.Catalogue(ctx)
	return settings, catalogue, err
}

// Groups are the groups of settings the camera has.
func (c *Connection) Groups(ctx context.Context) ([]ipcam.Group, error) {
	_, catalogue, err := c.describing(ctx)
	if err != nil {
		return nil, err
	}
	return catalogue.Groups(), nil
}

// Settings are the camera's settings of group, described, read from the camera as they
// alone need.
func (c *Connection) Settings(ctx context.Context, group string) ([]ipcam.Setting, error) {
	settings, catalogue, err := c.describing(ctx)
	if err != nil {
		return nil, err
	}
	keys, err := catalogue.Keys(group)
	if err != nil {
		return nil, err
	}
	values, err := settings.Values(ctx, keys)
	if err != nil {
		return nil, err
	}
	return catalogue.Describe(values), nil
}

// Apply writes value to the camera's setting of key, once checked against what the
// setting takes (ipcam.Check, which takes it as encoding/json decodes it). It answers
// whether the camera applies it only once restarted, which is pushed too, and every
// setting the write can have changed. The connection follows a write that moves the
// camera.
func (c *Connection) Apply(ctx context.Context, key ipcam.Key, value any) (restart bool, changed []ipcam.Setting, err error) {
	settings, catalogue, err := c.describing(ctx)
	if err != nil {
		return false, nil, err
	}
	setting, err := catalogue.Lookup(key)
	if err != nil {
		return false, nil, err
	}
	checked, err := ipcam.Check(setting.Kind, value)
	if err != nil {
		return false, nil, err
	}
	applied, err := settings.Apply(ctx, key, checked)
	if err != nil {
		return false, nil, err
	}
	c.mu.Lock()
	if applied.Address != "" {
		// Dialled there once the login on the old address ends.
		c.address = applied.Address
	}
	pending := !c.state.RestartPending && applied.Restart
	c.state.RestartPending = c.state.RestartPending || applied.Restart
	c.mu.Unlock()
	if pending {
		select {
		case c.changed <- struct{}{}:
		default:
		}
	}
	return applied.Restart, catalogue.Describe(applied.Changed), nil
}
