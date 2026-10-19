package wire_test

import (
	"encoding/json"
	"testing"

	"greendrake.nz/go/dvrip/dvriptest"
	"greendrake.nz/go/dvrip/internal/wire"
)

func TestOverlay(t *testing.T) {
	tests := map[string]struct{ base, top, want string }{
		"objects merge":                {`{"a":1,"b":{"c":2,"d":3}}`, `{"b":{"c":9}}`, `{"a":1,"b":{"c":9,"d":3}}`},
		"same-length arrays merge":     {`[{"a":1,"b":2},{"a":3,"b":4}]`, `[{"a":5},{"a":6}]`, `[{"a":5,"b":2},{"a":6,"b":4}]`},
		"other arrays replace":         {`[1,2,3]`, `[4]`, `[4]`},
		"values replace":               {`{"a":"x"}`, `{"a":{"b":1}}`, `{"a":{"b":1}}`},
		"numbers pass through exactly": {`{"a":12345678901234567,"b":0.1}`, `{}`, `{"a":12345678901234567,"b":0.1}`},
		"flags keep the device's form": {`{"a":1,"b":0,"c":false,"d":"0x00000000","e":"0x1"}`, `{"a":false,"b":true,"c":true,"d":true,"e":false}`, `{"a":0,"b":1,"c":true,"d":"0x00000001","e":"0x0"}`},
		"other values under a flag":    {`{"a":7,"b":"x","c":"0x2"}`, `{"a":true,"b":false,"c":true}`, `{"a":true,"b":false,"c":true}`},
		"hex keeps the device's width": {`{"a":"0x02","b":"0x00000000","c":"0x0"}`, `{"a":"0x00000006","b":"0x00000002","c":"0x00010000"}`, `{"a":"0x06","b":"0x00000002","c":"0x10000"}`},
		"hex over other values":        {`{"a":"x","b":2}`, `{"a":"0x00000006","b":"0x00000006"}`, `{"a":"0x00000006","b":"0x00000006"}`},
		"other values over hex":        {`{"a":"0x02","b":"0xZZ"}`, `{"a":"x","b":"0x1"}`, `{"a":"x","b":"0x1"}`},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := wire.Overlay(json.RawMessage(test.base), json.RawMessage(test.top))
			if err != nil {
				t.Fatal(err)
			}
			if !dvriptest.SameJSON(t, got, json.RawMessage(test.want)) {
				t.Fatalf("got %s, want %s", got, test.want)
			}
		})
	}
	for _, broken := range [][2]string{{`{`, `{}`}, {`{}`, `{`}} {
		if _, err := wire.Overlay(json.RawMessage(broken[0]), json.RawMessage(broken[1])); err == nil {
			t.Fatalf("%q over %q should not overlay", broken[1], broken[0])
		}
	}
}
