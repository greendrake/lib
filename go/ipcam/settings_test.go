package ipcam

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
)

func TestCheck(t *testing.T) {
	choiceOf := Kind{Type: Choice, Options: []Option{{"a", "A"}, {"b", "B"}}}
	tests := []struct {
		kind Kind
		// The value as the wire has it.
		value string
		want  any
		// The problem, where the value is refused.
		problem string
	}{
		{toggle, `true`, true, ""},
		{toggle, `"yes"`, nil, "not true or false"},
		{whole, `-3`, -3, ""},
		{whole, `1.5`, nil, "not a whole number"},
		{whole, `1e300`, nil, "not a whole number"},
		{whole, `"3"`, nil, "not a whole number"},
		{port, `65535`, 65535, ""},
		{port, `0`, nil, "not a whole number from 1 to 65535"},
		{port, `70000`, nil, "not a whole number from 1 to 65535"},
		{atLeast(1), `0`, nil, "not a whole number from 1"},
		{text, `"ntp.example"`, "ntp.example", ""},
		{text, `5`, nil, "not a text"},
		{textOf(1, 3, `\"`), `"Ünï"`, "Ünï", ""},
		{textOf(1, 3, `\"`), `""`, nil, `not a text of 1 to 3 characters without \ or "`},
		{textOf(1, 3, `\"`), `"four"`, nil, `not a text of 1 to 3 characters without \ or "`},
		{textOf(1, 3, `\"`), `"a\"b"`, nil, `not a text of 1 to 3 characters without \ or "`},
		{Kind{Type: Text, MinLength: textOf(2, 0, "").MinLength}, `"a"`, nil, "not a text of at least 2 characters"},
		{Kind{Type: Text, MaxLength: textOf(0, 2, "").MaxLength}, `"abc"`, nil, "not a text of at most 2 characters"},
		{share, `0.943359375`, 0.943359375, ""},
		{share, `1`, 1.0, ""},
		{share, `1.01`, nil, "not a share of the picture, from 0 to 1"},
		{share, `-0.5`, nil, "not a share of the picture, from 0 to 1"},
		{share, `"half"`, nil, "not a share of the picture, from 0 to 1"},
		{address, `"192.0.2.1"`, "192.0.2.1", ""},
		{address, `"256.0.0.1"`, nil, "not an IPv4 address: four numbers to 255 joined by dots"},
		{address, `"::1"`, nil, "not an IPv4 address: four numbers to 255 joined by dots"},
		{address, `1`, nil, "not an IPv4 address: four numbers to 255 joined by dots"},
		{choiceOf, `"b"`, "b", ""},
		{choiceOf, `"c"`, nil, `"c" is none of the options`},
		{choiceOf, `0`, nil, "not an option's value"},
		{Kind{Type: Pinned, Only: "H.265"}, `"H.265"`, "H.265", ""},
		{Kind{Type: Pinned, Only: "H.265"}, `"H.264"`, nil, "it is written as H.265 alone"},
		{Kind{Type: Pinned, Only: "H.265"}, `true`, nil, "not H.265"},
		{readOnly, `"00:00:5e:00:53:01"`, nil, "the setting is read only"},
	}
	for _, test := range tests {
		var value any
		if err := json.Unmarshal([]byte(test.value), &value); err != nil {
			t.Fatal(err)
		}
		got, err := Check(test.kind, value)
		var invalid *InvalidValue
		switch {
		case test.problem == "" && (err != nil || got != test.want):
			t.Errorf("%s %s: %v, %v", test.kind.Type, test.value, got, err)
		case test.problem != "" && (!errors.As(err, &invalid) || invalid.Problem != test.problem):
			t.Errorf("%s %s refused with %v", test.kind.Type, test.value, err)
		}
	}
	// A value in its kind's form checks as it is.
	if got, err := Check(port, 123); err != nil || got != 123 {
		t.Errorf("an int: %v, %v", got, err)
	}
}

// specificsOf has every one of keys bound.
func specificsOf(keys ...Key) map[Key]Specifics {
	specifics := map[Key]Specifics{}
	for _, key := range keys {
		specifics[key] = Specifics{}
	}
	return specifics
}

// keysOf are the keys of settings.
func keysOf(settings []Setting) []Key {
	keys := []Key{}
	for _, s := range settings {
		keys = append(keys, s.Key)
	}
	return keys
}

func TestDescribe(t *testing.T) {
	resolutions := []Option{{"4K", "4K"}, {"1080P", "1080P"}}
	c := NewCatalogue(Extension{}, map[Key]Specifics{
		"network.gateway":                 {Note: "It keeps its old one when given 0.0.0.0."},
		StreamSetting(Main, "resolution"): {Options: resolutions},
		"ntp.enable":                      {},
		"general.timeZone":                {},
	})
	settings := c.Describe(map[Key]any{"ntp.enable": true, "network.gateway": "192.0.2.1", StreamSetting(Main, "resolution"): "4K", "general.timeZone": "+12:00", "ntp.port": 123})
	// In the catalogue's order, the settings without a value, or the camera has not, left
	// out.
	if keys := keysOf(settings); !slices.Equal(keys, []Key{"general.timeZone", StreamSetting(Main, "resolution"), "network.gateway", "ntp.enable"}) {
		t.Fatalf("described %v", keys)
	}
	timeZone, resolution, gateway, ntp := settings[0], settings[1], settings[2], settings[3]
	if gateway.Group != "network" || gateway.Label != "Gateway" || gateway.Tip != "Where the camera sends what is for other networks. It keeps its old one when given 0.0.0.0." ||
		gateway.Kind.Type != IPv4 || gateway.Value != "192.0.2.1" {
		t.Errorf("gateway %+v", gateway)
	}
	if !slices.Equal(resolution.Kind.Options, resolutions) || resolution.Value != "4K" {
		t.Errorf("resolution %+v", resolution)
	}
	if len(timeZone.Kind.Options) != 105 || timeZone.Kind.Options[48] != (Option{"+00:00", "UTC+00:00"}) {
		t.Errorf("time zones %v", timeZone.Kind.Options)
	}
	if ntp.Tip != "Set the camera’s clock from a time server over the network." || ntp.Value != true {
		t.Errorf("NTP %+v", ntp)
	}
}

// A setting's kind is its own on the wire.
func TestSettingOnTheWire(t *testing.T) {
	for setting, want := range map[*Setting]string{
		{Key: "ntp.port", Group: "ntp", Label: "Port", Kind: port, Value: 123}:                                                        `{"Key":"ntp.port","Group":"ntp","Label":"Port","Tip":"","Type":"integer","Min":1,"Max":65535,"Value":123}`,
		{Key: "overlay.nameTop", Group: "overlay", Label: "Name down (%)", Kind: share, Value: 0.943359375}:                           `{"Key":"overlay.nameTop","Group":"overlay","Label":"Name down (%)","Tip":"","Type":"fraction","Value":0.943359375}`,
		{Key: "overlay.name", Group: "overlay", Label: "Camera name", Kind: textOf(1, 42, `\"`), Value: "pond"}:                       `{"Key":"overlay.name","Group":"overlay","Label":"Camera name","Tip":"","Type":"text","MinLength":1,"MaxLength":42,"Excluded":"\\\"","Value":"pond"}`,
		{Key: "general.dateFormat", Group: "general", Label: "Date format", Kind: ChoiceOf([2]string{"ymd", "YYMMDD"}), Value: "ymd"}: `{"Key":"general.dateFormat","Group":"general","Label":"Date format","Tip":"","Type":"choice","Options":[{"Value":"ymd","Label":"YYMMDD"}],"Value":"ymd"}`,
	} {
		if data, err := json.Marshal(setting); err != nil || string(data) != want {
			t.Errorf("%s, %v", data, err)
		}
	}
}

func TestLookup(t *testing.T) {
	c := NewCatalogue(Extension{}, specificsOf("network.mac", "no.such"))
	if mac, err := c.Lookup("network.mac"); err != nil || mac.Kind.Type != ReadOnly || mac.Value != nil {
		t.Fatalf("MAC %+v, %v", mac, err)
	}
	for _, key := range []Key{"no.such", "network.dhcp"} {
		var unknown *UnknownSetting
		if _, err := c.Lookup(key); !errors.As(err, &unknown) || unknown.Key != key {
			t.Errorf("%s: %v", key, err)
		}
	}
}

func TestTheCore(t *testing.T) {
	keys := Extension{}.Keys()
	for _, p := range core {
		for _, s := range p.settings {
			if s.Key.group() != p.group.ID {
				t.Errorf("%s is on the page %s", s.Key, p.group.ID)
			}
			values := map[string]bool{}
			for _, o := range s.Kind.Options {
				if values[o.Value] {
					t.Errorf("%s offers %q twice", s.Key, o.Value)
				}
				values[o.Value] = true
			}
		}
	}
	if keys[0] != "general.timeZone" || keys[len(keys)-1] != "maintenance.deleteAfterDays" {
		t.Errorf("%d settings, from %s to %s", len(keys), keys[0], keys[len(keys)-1])
	}
}

// extension is an adapter's: a setting in a group of the core's, and a group of its own.
var extension = Extension{
	Groups: []Group{{ID: "lens", Title: "Lens"}},
	Settings: []Spec{
		{Key: "lens.zoom", Label: "Zoom", Kind: IntegerIn(1, 30)},
		{Key: "ntp.backup", Label: "Backup server", Kind: Kind{Type: Text}},
	},
}

// An extension's settings join the core's groups, after the core's, or add groups of their
// own, after the core's.
func TestAnExtension(t *testing.T) {
	keys := extension.Keys()
	if i := slices.Index(keys, "ntp.backup"); i < 0 || keys[i-1] != "ntp.updatePeriod" || keys[len(keys)-1] != "lens.zoom" {
		t.Fatalf("keys %v", keys)
	}
	c := NewCatalogue(extension, specificsOf("ntp.enable", "ntp.backup", "lens.zoom"))
	if groups := c.Groups(); len(groups) != 3 || groups[0].ID != "ntp" || groups[2] != (Group{ID: "lens", Title: "Lens"}) {
		t.Fatalf("groups %+v", groups)
	}
	if ntp, err := c.Keys("ntp"); err != nil || !slices.Equal(ntp, []Key{"ntp.enable", "ntp.backup"}) {
		t.Fatalf("NTP %v, %v", ntp, err)
	}
	if zoom, err := c.Lookup("lens.zoom"); err != nil || zoom.Group != "lens" || zoom.Label != "Zoom" {
		t.Fatalf("zoom %+v, %v", zoom, err)
	}
	if keys := keysOf(c.Describe(map[Key]any{"lens.zoom": 2, "ntp.backup": "b", "ntp.enable": true})); !slices.Equal(keys, []Key{"ntp.enable", "ntp.backup", "lens.zoom"}) {
		t.Fatalf("described %v", keys)
	}
	// The core's catalogue is not the extension's.
	if keys := (Extension{}).Keys(); slices.Contains(keys, "ntp.backup") {
		t.Fatal("an extension's setting is the core's")
	}
}

// An extension's setting in no group, or of a key the catalogue has, is a mistake.
func TestAMistakenExtension(t *testing.T) {
	for name, ext := range map[string]Extension{
		"in no group":   {Settings: []Spec{{Key: "lens.zoom"}}},
		"the core's":    {Settings: []Spec{{Key: "ntp.port"}}},
		"its own twice": {Settings: []Spec{{Key: "ntp.backup"}, {Key: "ntp.backup"}}},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", name)
				}
			}()
			ext.Keys()
		}()
	}
}

// A camera has the groups it has a setting of, and Maintenance, which holds its clock
// and reboot too.
func TestGroups(t *testing.T) {
	ids := func(groups []Group) []string {
		all := []string{}
		for _, group := range groups {
			all = append(all, group.ID)
		}
		return all
	}
	for _, test := range []struct {
		keys []Key
		want []string
	}{
		{Extension{}.Keys(), []string{"general", "encode", "network", "ntp", "camera", "overlay", "maintenance"}},
		{[]Key{"ntp.server", "general.dst"}, []string{"general", "ntp", "maintenance"}},
		{nil, []string{"maintenance"}},
	} {
		if got := ids(NewCatalogue(Extension{}, specificsOf(test.keys...)).Groups()); !slices.Equal(got, test.want) {
			t.Errorf("%v: %v", test.keys, got)
		}
	}
	if groups := NewCatalogue(Extension{}, specificsOf(Extension{}.Keys()...)).Groups(); groups[3] != (Group{ID: "ntp", Title: "NTP"}) {
		t.Errorf("groups %+v", groups)
	}
}

func TestKeys(t *testing.T) {
	c := NewCatalogue(Extension{}, specificsOf("ntp.updatePeriod", "ntp.enable", "network.dhcp"))
	if keys, err := c.Keys("ntp"); err != nil || !slices.Equal(keys, []Key{"ntp.enable", "ntp.updatePeriod"}) {
		t.Errorf("NTP %v, %v", keys, err)
	}
	if keys, err := c.Keys("camera"); err != nil || len(keys) != 0 {
		t.Errorf("a group the camera has none of: %v, %v", keys, err)
	}
	var unknown *UnknownGroup
	if _, err := c.Keys("nope"); !errors.As(err, &unknown) || unknown.ID != "nope" {
		t.Errorf("no such group: %v", err)
	}
}

func TestZones(t *testing.T) {
	for east, option := range map[int]string{-720: "-12:00", 0: "+00:00", 345: "+05:45", 840: "+14:00"} {
		if got := ZoneOption(east); got != option {
			t.Errorf("%d minutes east is %q", east, got)
		}
		if got, err := ZoneMinutes(option); err != nil || got != east {
			t.Errorf("%q is %d minutes east, %v", option, got, err)
		}
	}
	if _, err := ZoneMinutes("UTC"); err == nil {
		t.Error("a zone of no offset read")
	}
}

func TestErrors(t *testing.T) {
	for err, message := range map[error]string{
		&UnknownSetting{Key: "no.such"}:        `ipcam: no setting "no.such"`,
		&UnknownGroup{ID: "nope"}:              `ipcam: no group "nope"`,
		&InvalidValue{Problem: "not true"}:     "ipcam: not true",
		&Refused{Message: "107 no permission"}: "107 no permission",
	} {
		if err.Error() != message {
			t.Errorf("%q, want %q", err.Error(), message)
		}
	}
}
