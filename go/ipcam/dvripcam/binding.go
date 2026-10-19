package dvripcam

import (
	"context"
	"fmt"

	"greendrake.nz/go/dvrip"
	"greendrake.nz/go/ipcam"
)

// configuration is one of the camera's that settings live in, whatever its type.
type configuration interface {
	name() string
	read(ctx context.Context, s *dvrip.Session) (any, error)
	write(ctx context.Context, s *dvrip.Session, config any) (restart bool, err error)
	// Where the camera answers with config, for a configuration whose writes move it;
	// empty for any other.
	address(config any) string
}

// source is a configuration of type T.
type source[T any] struct {
	config dvrip.Config[T]
	// Where the camera answers, for a configuration whose writes move it.
	at func(*T) string
}

func configOf[T any](c dvrip.Config[T]) *source[T] {
	return &source[T]{config: c}
}

func (s *source[T]) name() string {
	return s.config.Name
}

func (s *source[T]) read(ctx context.Context, session *dvrip.Session) (any, error) {
	v, err := s.config.Get(ctx, session)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (s *source[T]) write(ctx context.Context, session *dvrip.Session, config any) (bool, error) {
	return s.config.Set(ctx, session, *config.(*T))
}

func (s *source[T]) address(config any) string {
	if s.at == nil {
		return ""
	}
	return s.at(config.(*T))
}

// binding is a setting's place in a configuration, and how its value there is the
// setting's.
type binding struct {
	key  ipcam.Key
	from configuration
	get  func(config any) (any, error)
	set  func(config any, value any) error
	// Whether the camera has the setting here, where it can have it elsewhere or not at
	// all; always if nil.
	when func(capabilities) bool
	// What the camera adds to the setting's description; nothing if nil.
	specifics func(capabilities) ipcam.Specifics
	// What a write of value also does, once the configuration is written; made ready
	// before anything is, so that a value it cannot do refuses the write. Nothing if nil.
	also func(value any) (then func(ctx context.Context, s *dvrip.Session) error, err error)
}

// field binds key to the field of from's configuration at reaches, its value read and
// written through c.
func field[T, F any](from *source[T], key ipcam.Key, at func(*T) *F, c codec[F]) *binding {
	return &binding{
		key:  key,
		from: from,
		get: func(config any) (any, error) {
			return c.read(*at(config.(*T)))
		},
		set: func(config any, value any) error {
			f, err := c.write(value)
			if err == nil {
				*at(config.(*T)) = f
			}
			return err
		},
	}
}

// only has the camera have the setting so where when holds.
func (b *binding) only(when func(capabilities) bool) *binding {
	b.when = when
	return b
}

// adding has the camera add specifics to the setting's description.
func (b *binding) adding(specifics func(capabilities) ipcam.Specifics) *binding {
	b.specifics = specifics
	return b
}

// doing has a write of the setting also do what also makes ready.
func (b *binding) doing(also func(value any) (func(ctx context.Context, s *dvrip.Session) error, error)) *binding {
	b.also = also
	return b
}

// value is the setting's in config.
func (b *binding) value(config any) (v any, err error) {
	err = b.reach(func() error {
		v, err = b.get(config)
		return err
	})
	return v, err
}

// assign sets the setting in config.
func (b *binding) assign(config, value any) error {
	return b.reach(func() error { return b.set(config, value) })
}

// reach runs f, which reaches into the camera's configuration: one of a shape it
// cannot reach into, a list shorter than the field's index, fails rather than panics.
func (b *binding) reach(f func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("dvripcam: %s: the camera's %s has no such field: %v", b.key, b.from.name(), r)
		}
	}()
	return f()
}
