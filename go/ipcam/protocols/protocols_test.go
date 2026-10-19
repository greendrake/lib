package protocols

import (
	"reflect"
	"testing"

	"greendrake.nz/go/ipcam"
	"greendrake.nz/go/ipcam/dvripcam"
	"greendrake.nz/go/ipcam/rtspcam"
)

func TestDriver(t *testing.T) {
	for name, want := range map[string]ipcam.Driver{"dvrip": dvripcam.Driver{}, "rtsp": rtspcam.BITVISION} {
		if got, err := Driver(name); err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %#v, %v", name, got, err)
		}
	}
}

// A protocol there is none of fails, naming those there are.
func TestNoDriver(t *testing.T) {
	if _, err := Driver("onvif"); err == nil || err.Error() != `protocols: "onvif" is none of dvrip, rtsp` {
		t.Fatalf("got %v", err)
	}
}
