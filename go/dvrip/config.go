package dvrip

import (
	"context"
	"encoding/json"
	"fmt"

	"greendrake.nz/go/dvrip/internal/wire"
)

// Get reads the configuration the device keeps under name: a group ("General"), one
// configuration ("General.Location"), or one channel's ("Camera.Param.[0]").
func (s *Session) Get(ctx context.Context, name string) (json.RawMessage, error) {
	return s.query(ctx, wire.ConfigGet, name)
}

// Set replaces the configuration under name with v. Restart reports that the device
// took it but applies it only once restarted.
func (s *Session) Set(ctx context.Context, name string, v any) (restart bool, err error) {
	r, err := s.command(ctx, wire.ConfigSet, name, v)
	if err != nil {
		return false, err
	}
	return r.ret.restart(), nil
}

// Ability reads one of the device's descriptions of what it supports.
func (s *Session) Ability(ctx context.Context, name string) (json.RawMessage, error) {
	return s.query(ctx, wire.Ability, name)
}

// Info reads one of the device's reports on itself ("SystemInfo").
func (s *Session) Info(ctx context.Context, name string) (json.RawMessage, error) {
	return s.query(ctx, wire.SystemInfo, name)
}

// Config binds a configuration name to the type it decodes to.
type Config[T any] struct {
	Name string
}

func (c Config[T]) Get(ctx context.Context, s *Session) (T, error) {
	return decodeAs[T](s.Get(ctx, c.Name))
}

// Set writes v over the configuration the device has. The types here declare what
// their users need, not every field a firmware keeps, and the device would reset any
// field a write leaves out; so the device's own configuration is read first and v
// laid over it, leaving everything v does not declare as the device had it.
func (c Config[T]) Set(ctx context.Context, s *Session, v T) (restart bool, err error) {
	current, err := s.Get(ctx, c.Name)
	if err != nil {
		return false, err
	}
	update, err := json.Marshal(v)
	if err != nil {
		return false, err
	}
	merged, err := wire.Overlay(current, update)
	if err != nil {
		return false, fmt.Errorf("dvrip: %s: %w", c.Name, err)
	}
	return s.Set(ctx, c.Name, merged)
}

// ChannelConfig is a configuration the device keeps per channel, as an array.
type ChannelConfig[T any] struct {
	Name string
}

// Channel addresses one channel's configuration.
func (c ChannelConfig[T]) Channel(n int) Config[T] {
	return Config[T]{Name: fmt.Sprintf("%s.[%d]", c.Name, n)}
}

// All addresses every channel's at once.
func (c ChannelConfig[T]) All() Config[[]T] {
	return Config[[]T]{Name: c.Name}
}

// AbilityOf binds an ability name to the type it decodes to.
type AbilityOf[T any] struct {
	Name string
}

func (a AbilityOf[T]) Get(ctx context.Context, s *Session) (T, error) {
	return decodeAs[T](s.Ability(ctx, a.Name))
}
