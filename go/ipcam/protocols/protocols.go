// Package protocols is the engine's adapters by the names a host's configuration gives
// their protocols: dvrip, DVRIP (Sofia) cameras' streams and settings (dvripcam); and
// rtsp, streams alone, at /0 and /1 as BITVISION's cameras serve them
// (rtspcam.BITVISION).
package protocols

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"greendrake.nz/go/ipcam"
	"greendrake.nz/go/ipcam/dvripcam"
	"greendrake.nz/go/ipcam/rtspcam"
)

// drivers are the protocols' drivers, by name.
var drivers = map[string]ipcam.Driver{"dvrip": dvripcam.Driver{}, "rtsp": rtspcam.BITVISION}

// Driver is the driver of the protocol of name.
func Driver(name string) (ipcam.Driver, error) {
	driver, known := drivers[name]
	if !known {
		return nil, fmt.Errorf("protocols: %q is none of %s", name, strings.Join(slices.Sorted(maps.Keys(drivers)), ", "))
	}
	return driver, nil
}
