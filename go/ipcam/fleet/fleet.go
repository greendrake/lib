// Package fleet is a host's cameras by name: each one's connection, and a source of each
// of its streams, which its viewers, its recorders and whatever else the host has
// subscribe to. A stream is opened on the camera's login, or where its StreamsFrom says,
// while it has subscribers, waiting out the camera being offline.
//
// A stop the camera leaves unanswered ends the login, and with it every stream on that
// camera: a camera in that state is in no shape to stream, and the connection's redial
// restores it, recorders starting new files.
package fleet

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"

	"greendrake.nz/go/camstream"
	"greendrake.nz/go/ipcam"
	"greendrake.nz/go/ipcam/connection"
	"greendrake.nz/go/ipcam/internal/failures"
	"greendrake.nz/go/ipcam/record"
)

// Camera is one of a fleet's.
type Camera struct {
	// Its connection, Dialled or Started, which the fleet runs.
	Conn *connection.Connection
	// The streams it records, for as long as the fleet runs.
	Record []ipcam.StreamKey
	// Where its streams come from rather than off its login (another host relaying them,
	// say): an attempt at opening one, as a camstream.Open. Nil for the camera itself.
	// Either way a stream is opened only while the camera is online on a login that
	// encodes it in the cast codec, and the camera is logged in to all the same, for its
	// state and settings.
	StreamsFrom func(ctx context.Context, key ipcam.StreamKey) (camstream.Monitor, error)
}

// Option configures a Fleet.
type Option func(*Fleet)

// WithLogger is where the fleet logs a stream that fails to open, and a recording that
// fails; the default is slog.Default().
func WithLogger(l *slog.Logger) Option {
	return func(f *Fleet) { f.logger = l }
}

// Fleet is the cameras, running.
type Fleet struct {
	// What the cameras run until: New's, which Start's logins run on too.
	ctx     context.Context
	logger  *slog.Logger
	cameras map[string]*camera
	running sync.WaitGroup
}

type camera struct {
	conn *connection.Connection
	// Set where the camera's streams come through its StreamsFrom.
	relayed bool
	sources map[ipcam.StreamKey]*camstream.Source
}

// New runs cameras until ctx ends: the sources of their streams, and their recorders,
// which record to <recordDir>/<name>. The cameras are logged in to by Start, which is
// what lets whatever their states are pushed through be built over the fleet first.
func New(ctx context.Context, cameras map[string]Camera, recordDir string, opts ...Option) *Fleet {
	f := &Fleet{ctx: ctx, logger: slog.Default(), cameras: map[string]*camera{}}
	for _, opt := range opts {
		opt(f)
	}
	for name, c := range cameras {
		cam := &camera{conn: c.Conn, relayed: c.StreamsFrom != nil, sources: map[ipcam.StreamKey]*camstream.Source{}}
		for _, key := range ipcam.StreamKeys {
			source := camstream.NewSource(ctx, f.open(name, c, key))
			cam.sources[key] = source
			f.running.Go(func() { <-source.Done() })
		}
		for _, key := range c.Record {
			f.running.Go(func() { record.Run(ctx, cam.sources[key], filepath.Join(recordDir, name), string(key), f.logger) })
		}
		f.cameras[name] = cam
	}
	return f
}

// Start runs the cameras' connections, each change of a camera's state pushed as its
// name and state.
func (f *Fleet) Start(push func(name string, state connection.State)) {
	for name, cam := range f.cameras {
		f.running.Go(func() {
			cam.conn.Run(f.ctx, func(state connection.State) { push(name, state) })
		})
	}
}

// open opens the named camera's stream on its login, or where its StreamsFrom says,
// waiting out the camera being offline. A login that does not encode the stream in the
// cast codec is waited out too: a codec change applies at the next. An attempt that
// fails, rather than being given up, is logged where it is news, so that a stream that
// cannot open says why without saying it at every retry.
func (f *Fleet) open(name string, c Camera, key ipcam.StreamKey) camstream.Open {
	// The attempts' failures: the source makes the attempts one at a time.
	var last failures.Last
	return func(ctx context.Context) (monitor camstream.Monitor, err error) {
		defer func() {
			switch {
			case err == nil:
				last.Succeeded()
			case ctx.Err() == nil && last.News(err):
				f.logger.Warn("ipcam: a stream fails to open", "camera", name, "stream", key, "err", err)
			}
		}()
		for {
			cam, err := c.Conn.Await(ctx)
			if err != nil {
				return nil, err
			}
			if cam.Streams()[key].Codec == ipcam.Cast {
				if c.StreamsFrom == nil {
					return cam.OpenStream(ctx, key)
				}
				return c.StreamsFrom(ctx, key)
			}
			select {
			case <-cam.Done():
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
}

// Wait waits for the fleet to have stopped: logins closed, monitors shut down, and
// recordings closed.
func (f *Fleet) Wait() {
	f.running.Wait()
}

// Conn is the connection of the camera name names.
func (f *Fleet) Conn(name string) (*connection.Connection, bool) {
	cam, known := f.cameras[name]
	if !known {
		return nil, false
	}
	return cam.conn, true
}

// States are every camera's state, by name.
func (f *Fleet) States() map[string]connection.State {
	states := map[string]connection.State{}
	for name, cam := range f.cameras {
		states[name] = cam.conn.State()
	}
	return states
}

// Source is the source of the named camera's stream of key, and whether the camera's
// streams come through its StreamsFrom. Not ok means the fleet has no such camera or
// stream, or the camera encodes the stream in another codec than the cast one, which is
// not opened.
func (f *Fleet) Source(name string, key ipcam.StreamKey) (src *camstream.Source, relayed bool, ok bool) {
	cam, known := f.cameras[name]
	if !known {
		return nil, false, false
	}
	source, has := cam.sources[key]
	if state, encoded := cam.conn.State().Streams[key]; !has || encoded && !state.Cast {
		return nil, false, false
	}
	return source, cam.relayed, true
}
