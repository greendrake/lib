// Package ipcamrpc is a greendrake.nz/go/ipcam fleet over the wire, to a browser app: the
// methods it calls on the cameras (greendrake.nz/go/rpcserver), each on the camera it
// names; every camera's state, which it keeps (a Broadcast document); and the streams it
// watches (greendrake.nz/go/webcast), keyed <camera>/<stream>. Upstream relays a camera's
// streams off another host serving them so.
package ipcamrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"greendrake.nz/go/camstream"
	"greendrake.nz/go/ipcam"
	"greendrake.nz/go/ipcam/connection"
	"greendrake.nz/go/ipcam/fleet"
	"greendrake.nz/go/rpcserver"
	"greendrake.nz/go/rpcserver/rpcargs"
	"greendrake.nz/go/webcast"
)

// StateType is the document the cameras' states are published as: every camera's state,
// by name.
const StateType = "cameras"

// Cameras publishes the fleet's cameras on b as the document StateType, and is the push
// the fleet is started with: each change of a camera's state refreshes the document.
func Cameras(b *rpcserver.Broadcast, f *fleet.Fleet) func(name string, state connection.State) {
	doc := b.Document(StateType, func() any { return f.States() })
	return func(string, connection.State) { doc.Refresh() }
}

// streamKey is what a stream is served by: "<camera>/<stream>", "gate/main".
func streamKey(camera string, key ipcam.StreamKey) string {
	return camera + "/" + string(key)
}

// Streams serves the fleet's streams, by streamKey, to viewers and to the hosts relaying
// them, until ctx ends. A stream whose camera encodes it in another codec than the cast
// one is refused, and so is a relay of a stream that is relayed itself (its camera's
// StreamsFrom set), which keeps a relay from leading back to where it started.
func Streams(ctx context.Context, f *fleet.Fleet, opts ...webcast.Option) http.Handler {
	return webcast.Handler(ctx, func(key string, raw bool) *camstream.Source {
		i := strings.LastIndex(key, "/")
		if i < 0 {
			return nil
		}
		source, relayed, ok := f.Source(key[:i], ipcam.StreamKey(key[i+1:]))
		if !ok || raw && relayed {
			return nil
		}
		return source
	}, opts...)
}

// Upstream is a fleet.Camera's StreamsFrom relaying the streams of the camera named so on
// the host whose Streams url (ws:// or wss://) reaches.
func Upstream(url, camera string, opts ...webcast.RelayOption) func(ctx context.Context, key ipcam.StreamKey) (camstream.Monitor, error) {
	return func(ctx context.Context, key ipcam.StreamKey) (camstream.Monitor, error) {
		return webcast.Relay(ctx, url, streamKey(camera, key), opts...)
	}
}

// Access is what every method asks of its caller, as rpcserver.MethodDef has it: the
// host's to decide. The zero Access lets anyone call.
type Access struct {
	Auth   rpcserver.AuthRequirement
	Access any
}

// Methods is the dispatch table, every method on the camera of the fleet it names, asking
// access of its caller.
func Methods(f *fleet.Fleet, access Access) map[string]rpcserver.MethodDef {
	methods := map[string]rpcserver.MethodDef{}
	for name, define := range definitions {
		methods[name] = rpcserver.MethodDef{Auth: access.Auth, Access: access.Access, Handler: define(f, name)}
	}
	return methods
}

// definitions are the methods by name, each a handler on a fleet.
var definitions = map[string]func(f *fleet.Fleet, name string) rpcserver.HandlerFunc{
	"settings.groups": method(getGroups),
	"settings.get":    method(getSettings),
	"settings.set":    method(setSetting),
	"camera.time":     method(onSettings(readTime)),
	"camera.setTime":  method(onSettings(setTime)),
	"camera.reboot":   method(onSettings(reboot)),
}

// On names the camera a call is on: every method's arguments have it.
type On struct {
	Camera string
}

func (o On) camera() string {
	return o.Camera
}

// named is arguments that name a camera.
type named interface {
	camera() string
}

// handler is a method's own part: what it does with its camera's connection and its
// arguments.
type handler[A named] func(ctx context.Context, conn *connection.Connection, args A) (any, error)

// method makes h a method's handler on a fleet: its arguments decoded as A, the camera
// they name found, and its failure answered as the wire says it.
func method[A named](h handler[A]) func(f *fleet.Fleet, name string) rpcserver.HandlerFunc {
	return func(f *fleet.Fleet, name string) rpcserver.HandlerFunc {
		return func(hc *rpcserver.HandlerContext, raw json.RawMessage) (any, error) {
			var args A
			if err := rpcargs.ParseArguments(raw, &args); err != nil {
				return nil, err
			}
			conn, known := f.Conn(args.camera())
			if !known {
				return nil, problem("UNKNOWN_CAMERA", fmt.Sprintf("There is no camera %q.", args.camera()))
			}
			result, err := h(hc.Ctx, conn, args)
			if err != nil {
				return nil, answer(name, err)
			}
			return result, nil
		}
	}
}

// Problem is the details of a failure the app shows as it is: what went wrong, in words.
type Problem struct {
	Message string
}

func problem(code, message string) error {
	return rpcserver.NewAPIErrorWithDetails(code, Problem{Message: message})
}

// answer is err as the wire says it.
func answer(name string, err error) error {
	var refused *ipcam.Refused
	var unknown *ipcam.UnknownSetting
	var noGroup *ipcam.UnknownGroup
	var invalid *ipcam.InvalidValue
	switch {
	case errors.Is(err, connection.ErrNoSettings):
		return problem("NO_SETTINGS", "The camera has no settings to change.")
	case errors.Is(err, connection.ErrOffline):
		return problem("CAMERA_OFFLINE", "The camera is offline.")
	case errors.As(err, &refused):
		// The camera's own words.
		return problem("CAMERA_REFUSED", "The camera refused: "+refused.Message)
	case errors.As(err, &unknown):
		return problem("UNKNOWN_SETTING", fmt.Sprintf("The camera has no setting %q.", unknown.Key))
	case errors.As(err, &noGroup):
		return problem("UNKNOWN_GROUP", fmt.Sprintf("There is no group of settings %q.", noGroup.ID))
	case errors.As(err, &invalid):
		// A sentence of it: "Not a whole number from 1 to 65535."
		return problem("INVALID_VALUE", strings.ToUpper(invalid.Problem[:1])+invalid.Problem[1:]+".")
	}
	return rpcserver.Answer(name, err, nil)
}

// onSettings makes h a handler of the camera's settings, which answers NO_SETTINGS on a
// camera with none, and CAMERA_OFFLINE while it is offline.
func onSettings[A named](h func(ctx context.Context, settings ipcam.Settings, args A) (any, error)) handler[A] {
	return func(ctx context.Context, conn *connection.Connection, args A) (any, error) {
		settings, err := conn.Configurable()
		if err != nil {
			return nil, err
		}
		return h(ctx, settings, args)
	}
}

func getGroups(ctx context.Context, conn *connection.Connection, _ On) (any, error) {
	groups, err := conn.Groups(ctx)
	if err != nil {
		return nil, err
	}
	return groups, nil
}

type getArgs struct {
	On
	Group string
}

func getSettings(ctx context.Context, conn *connection.Connection, args getArgs) (any, error) {
	settings, err := conn.Settings(ctx, args.Group)
	if err != nil {
		return nil, err
	}
	return settings, nil
}

type setArgs struct {
	On
	Key ipcam.Key
	// As encoding/json decodes it, which ipcam.Check takes.
	Value any
}

// SetResult answers a write.
type SetResult struct {
	// The camera took it, but applies it only once restarted.
	Restart bool
	// Every setting the write can have changed, as the camera has them now.
	Changed []ipcam.Setting
}

func setSetting(ctx context.Context, conn *connection.Connection, args setArgs) (any, error) {
	restart, changed, err := conn.Apply(ctx, args.Key, args.Value)
	if err != nil {
		return nil, err
	}
	return SetResult{Restart: restart, Changed: changed}, nil
}

// Clock is the camera's wall clock, as time.DateTime in no zone: the camera keeps
// none, and the app sends the browser's local wall time.
type Clock struct {
	Time string
}

type clockArgs struct {
	On
	Clock
}

func readTime(ctx context.Context, settings ipcam.Settings, _ On) (any, error) {
	t, err := settings.Clock(ctx)
	if err != nil {
		return nil, err
	}
	return Clock{Time: t.Format(time.DateTime)}, nil
}

func setTime(ctx context.Context, settings ipcam.Settings, args clockArgs) (any, error) {
	t, err := time.Parse(time.DateTime, args.Time)
	if err != nil {
		return nil, rpcserver.NewAPIErrorWithDetails(rpcserver.CodeInvalidArgument, []rpcargs.FieldError{{Field: "Time", Rule: "datetime", Param: time.DateTime}})
	}
	return true, settings.SetClock(ctx, t)
}

func reboot(ctx context.Context, settings ipcam.Settings, _ On) (any, error) {
	return true, settings.Reboot(ctx)
}
