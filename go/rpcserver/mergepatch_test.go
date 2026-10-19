package rpcserver

import (
	"reflect"
	"testing"
)

func TestMergePatch(t *testing.T) {
	tests := []struct {
		name        string
		prev, next  map[string]any
		wantPatch   map[string]any
		wantChanged bool
	}{
		{
			name:      "equal documents patch nothing",
			prev:      map[string]any{"a": 1.0, "b": map[string]any{"c": "x"}, "d": []any{1.0, 2.0}},
			next:      map[string]any{"a": 1.0, "b": map[string]any{"c": "x"}, "d": []any{1.0, 2.0}},
			wantPatch: map[string]any{},
		},
		{
			name:        "an added member is carried whole",
			prev:        map[string]any{},
			next:        map[string]any{"gate": map[string]any{"State": "online"}},
			wantPatch:   map[string]any{"gate": map[string]any{"State": "online"}},
			wantChanged: true,
		},
		{
			name:        "a removed member is null",
			prev:        map[string]any{"gate": map[string]any{"State": "online"}, "porch": "x"},
			next:        map[string]any{"porch": "x"},
			wantPatch:   map[string]any{"gate": nil},
			wantChanged: true,
		},
		{
			name:        "a changed scalar is carried",
			prev:        map[string]any{"load": 798.0, "mode": 3.0},
			next:        map[string]any{"load": 801.0, "mode": 3.0},
			wantPatch:   map[string]any{"load": 801.0},
			wantChanged: true,
		},
		{
			name:        "objects within objects are patched in turn",
			prev:        map[string]any{"gate": map[string]any{"State": "online", "Error": ""}, "porch": map[string]any{"State": "online"}},
			next:        map[string]any{"gate": map[string]any{"State": "offline", "Error": "refused"}, "porch": map[string]any{"State": "online"}},
			wantPatch:   map[string]any{"gate": map[string]any{"State": "offline", "Error": "refused"}},
			wantChanged: true,
		},
		{
			name:        "a member gone from a nested object is null there",
			prev:        map[string]any{"gate": map[string]any{"Streams": map[string]any{"main": "H.265"}}},
			next:        map[string]any{"gate": map[string]any{}},
			wantPatch:   map[string]any{"gate": map[string]any{"Streams": nil}},
			wantChanged: true,
		},
		{
			name:        "an object replaced by a scalar, and a scalar by an object, are carried whole",
			prev:        map[string]any{"a": map[string]any{"b": 1.0}, "c": 2.0},
			next:        map[string]any{"a": 1.0, "c": map[string]any{"d": 2.0}},
			wantPatch:   map[string]any{"a": 1.0, "c": map[string]any{"d": 2.0}},
			wantChanged: true,
		},
		{
			name:        "a changed array is carried whole",
			prev:        map[string]any{"d": []any{1.0, 2.0}},
			next:        map[string]any{"d": []any{1.0, 3.0}},
			wantPatch:   map[string]any{"d": []any{1.0, 3.0}},
			wantChanged: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			patch, changed := mergePatch(tc.prev, tc.next)
			if changed != tc.wantChanged || !reflect.DeepEqual(patch, tc.wantPatch) {
				t.Fatalf("got %#v (changed %v), want %#v (changed %v)", patch, changed, tc.wantPatch, tc.wantChanged)
			}
		})
	}
}
