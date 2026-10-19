package dvriptest

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// devices are the recorded devices, a directory each: see devices/README.md.
//
//go:embed devices
var devices embed.FS

// Replies is a recorded device's replies, by query name.
func Replies(device string) (map[string]json.RawMessage, error) {
	files, err := fs.Glob(devices, path.Join("devices", device, "*.json"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("dvriptest: no device %q recorded", device)
	}
	replies := map[string]json.RawMessage{}
	for _, file := range files {
		data, err := devices.ReadFile(file)
		if err != nil {
			return nil, err
		}
		replies[strings.TrimSuffix(path.Base(file), ".json")] = data
	}
	return replies, nil
}

// Recording is the stream a monitor of either stream plays, whatever the device: xm530's
// extra stream, as dvrip's Monitor.Record saved it. It is the one recording kept, as a
// capture of a main stream is too big to keep.
func Recording() ([]byte, error) {
	return devices.ReadFile("devices/xm530/extra.h265.bin")
}
