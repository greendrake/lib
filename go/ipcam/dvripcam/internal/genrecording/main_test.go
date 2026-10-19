package main

import (
	"reflect"
	"testing"

	"greendrake.nz/go/camstream/camstreamtest"
)

// camstreamtest's recording is the one this writes: it was rerun since either changed.
func TestTheRecordingIsCurrent(t *testing.T) {
	want, err := recording()
	if err != nil {
		t.Fatal(err)
	}
	got, err := camstreamtest.Recording()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("camstreamtest has %d frames, the recording %d: rerun genrecording", len(got), len(want))
	}
}
