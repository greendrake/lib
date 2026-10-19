package dvripcam

import (
	"fmt"
	"math"
	"net/netip"

	"greendrake.nz/go/dvrip"
	"greendrake.nz/go/ipcam"
)

// codec is how a field of type F holds a setting's value, in its kind's form.
type codec[F any] struct {
	read  func(F) (any, error)
	write func(value any) (F, error)
}

// as is value as the T its setting's kind takes.
func as[T any](value any) (T, error) {
	v, ok := value.(T)
	if !ok {
		return v, fmt.Errorf("dvripcam: %v (%T) is no %T", value, value, v)
	}
	return v, nil
}

// same holds the value as it is.
func same[F bool | int | string]() codec[F] {
	return codec[F]{
		read:  func(f F) (any, error) { return f, nil },
		write: as[F],
	}
}

// named holds a string as the device's string type F.
func named[F ~string]() codec[F] {
	return codec[F]{
		read: func(f F) (any, error) { return string(f), nil },
		write: func(value any) (F, error) {
			s, err := as[string](value)
			return F(s), err
		},
	}
}

var flag = codec[dvrip.Flag]{
	read: func(f dvrip.Flag) (any, error) { return bool(f), nil },
	write: func(value any) (dvrip.Flag, error) {
		on, err := as[bool](value)
		return dvrip.Flag(on), err
	},
}

var hexNumber = codec[dvrip.Hex]{
	read: func(h dvrip.Hex) (any, error) { return int(h), nil },
	write: func(value any) (dvrip.Hex, error) {
		n, err := as[int](value)
		return dvrip.Hex(n), err
	},
}

var dotted = codec[dvrip.IPv4]{
	read: func(a dvrip.IPv4) (any, error) { return a.String(), nil },
	write: func(value any) (dvrip.IPv4, error) {
		s, err := as[string](value)
		if err != nil {
			return dvrip.IPv4{}, err
		}
		address, err := netip.ParseAddr(s)
		return dvrip.IPv4{Addr: address}, err
	},
}

// relativeSpan is what the cameras' relative coordinates run to, across or down the
// picture from 0.
const relativeSpan = 8192

// share holds a share of the picture as the camera's relative coordinate. Every
// coordinate is a fraction a float64 holds exactly, so each reads back as it was.
var share = codec[int]{
	read: func(n int) (any, error) { return float64(n) / relativeSpan, nil },
	write: func(value any) (int, error) {
		f, err := as[float64](value)
		return int(math.Round(f * relativeSpan)), err
	},
}

// zone holds a time zone option as the device's minutes west of UTC.
var zone = codec[int]{
	read: func(west int) (any, error) { return ipcam.ZoneOption(-west), nil },
	write: func(value any) (int, error) {
		option, err := as[string](value)
		if err != nil {
			return 0, err
		}
		east, err := ipcam.ZoneMinutes(option)
		return -east, err
	},
}

// enum holds a choice's option values as the device's values by them.
func enum[F comparable](values map[string]F) codec[F] {
	options := map[F]string{}
	for option, v := range values {
		options[v] = option
	}
	return codec[F]{
		read: func(f F) (any, error) {
			option, known := options[f]
			if !known {
				return nil, fmt.Errorf("dvripcam: the camera's %v is none of the options", f)
			}
			return option, nil
		},
		write: func(value any) (F, error) {
			option, err := as[string](value)
			v, known := values[option]
			if err == nil && !known {
				err = fmt.Errorf("dvripcam: no option %q", option)
			}
			return v, err
		},
	}
}

// toggle holds a switch as the device's values on and off.
func toggle[F comparable](on, off F) codec[F] {
	return codec[F]{
		read: func(f F) (any, error) {
			if f != on && f != off {
				return nil, fmt.Errorf("dvripcam: the camera's %v is neither on nor off", f)
			}
			return f == on, nil
		},
		write: func(value any) (F, error) {
			set, err := as[bool](value)
			if set {
				return on, err
			}
			return off, err
		},
	}
}

// codecs are the core's codecs by the names the cameras give them.
var codecs = map[string]ipcam.Codec{"H.265": ipcam.H265, "H.264": ipcam.H264}

// codecOf is the codec a camera names so; one outside the table keeps the camera's
// name, a codec the app does not cast.
func codecOf(compression string) ipcam.Codec {
	if codec, known := codecs[compression]; known {
		return codec
	}
	return ipcam.Codec(compression)
}

// compression holds a stream's codec as the camera names it.
var compression = func() codec[string] {
	names := map[string]string{}
	for name, codec := range codecs {
		names[string(codec)] = name
	}
	return codec[string]{
		read:  func(name string) (any, error) { return string(codecOf(name)), nil },
		write: enum(names).write,
	}
}()

// offered are options whose values are their labels.
func offered(values []string) []ipcam.Option {
	options := make([]ipcam.Option, len(values))
	for i, v := range values {
		options[i] = ipcam.Option{Value: v, Label: v}
	}
	return options
}
