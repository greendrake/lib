// Command genrecording writes camstreamtest's recording (greendrake.nz/go/camstream's
// camstreamtest.Recording) to the file named: the recording dvriptest plays, as dvripcam's
// monitors yield it. Rerun it, at the ipcam module's root, when either changes:
//
//	go run ./dvripcam/internal/genrecording <camstream>/camstreamtest/recording.gob
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"greendrake.nz/go/camstream"
	"greendrake.nz/go/camstream/camstreamtest"
	"greendrake.nz/go/dvrip"
	"greendrake.nz/go/dvrip/dvriptest"
	"greendrake.nz/go/ipcam/dvripcam/internal/monitor"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: genrecording <file>")
		os.Exit(2)
	}
	frames, err := recording()
	var b bytes.Buffer
	if err == nil {
		err = camstreamtest.WriteRecording(&b, frames)
	}
	if err == nil {
		err = os.WriteFile(os.Args[1], b.Bytes(), 0o644)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "genrecording: %v\n", err)
		os.Exit(1)
	}
}

// recording is dvriptest's recording, every frame of it, as a monitor yields it.
func recording() ([]*camstream.Frame, error) {
	recorded, err := dvriptest.Recording()
	if err != nil {
		return nil, err
	}
	m := monitor.New(dvrip.NewReplay(bytes.NewReader(recorded)), true, func() {})
	var frames []*camstream.Frame
	for {
		f, err := m.Next()
		if errors.Is(err, io.EOF) {
			return frames, nil
		}
		if err != nil {
			return nil, err
		}
		frames = append(frames, f)
	}
}
