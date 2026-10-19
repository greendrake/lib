package dvripcam

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"greendrake.nz/go/dvrip"
	"greendrake.nz/go/dvrip/dvriptest"
	"greendrake.nz/go/ipcam"
)

// recordedDevices are the devices the settings are verified against, with the
// catalogue's settings each lacks.
var recordedDevices = map[string][]ipcam.Key{
	// The Overlay group's settings, as its replies have no AVEnc.VideoWidget.
	"techage":             {"overlay.name", "overlay.showName", "overlay.nameLeft", "overlay.nameTop", "overlay.showTime", "overlay.timeLeft", "overlay.timeTop"},
	"techage-hi3516ev100": {ipcam.StreamSetting(ipcam.Extra, "smartEncode")},
	"xm530":               nil,
}

func dial(t *testing.T, fake *dvriptest.Camera) *login {
	t.Helper()
	c, err := Driver{}.DialSettings(context.Background(), fake.Address(), "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c.(*login)
}

// The driver's cameras have settings, and Dial logs in to them as DialSettings does.
func TestTheDriverHasSettings(t *testing.T) {
	var driver ipcam.Driver = Driver{}
	if _, settings := driver.(ipcam.SettingsDriver); !settings {
		t.Fatal("a DVRIP driver has no settings")
	}
	c, err := driver.Dial(context.Background(), dvriptest.New(t, "techage", dvriptest.Options{}).Address(), "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if _, login := c.(*login); !login {
		t.Fatalf("dialled %T", c)
	}
}

// selected is l's selection, which the test needs made.
func selected(t *testing.T, l *login) selection {
	t.Helper()
	s, err := l.selected(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// lastWritten is the configuration last written under name.
func lastWritten(fake *dvriptest.Camera, name string) json.RawMessage {
	commands := fake.Commands(name)
	for i := len(commands) - 1; i >= 0; i-- {
		if written, set := commands[i].Body[name]; set {
			return written
		}
	}
	return nil
}

func TestBindingsCoverTheCatalogue(t *testing.T) {
	bound := map[ipcam.Key]bool{}
	for _, b := range bindings {
		bound[b.key] = true
	}
	keys := xmeye.Keys()
	for _, key := range keys {
		if !bound[key] {
			t.Errorf("no binding of %s", key)
		}
	}
	if len(bound) != len(keys) {
		t.Errorf("%d keys bound, of %d in the catalogue", len(bound), len(keys))
	}
}

// Every setting a recorded device has reads, described completely, and writing its value
// back writes the device's configuration as recorded.
func TestSettingsWriteBackAsRecorded(t *testing.T) {
	for device, lacks := range recordedDevices {
		t.Run(device, func(t *testing.T) {
			recorded, err := dvriptest.Replies(device)
			if err != nil {
				t.Fatal(err)
			}
			fake := dvriptest.New(t, device, dvriptest.Options{})
			l := dial(t, fake)
			ctx := context.Background()
			values, err := l.Values(ctx, xmeye.Keys())
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range xmeye.Keys() {
				if _, has := values[key]; has == slices.Contains(lacks, key) {
					t.Errorf("has %s: %v", key, has)
				}
			}
			chosen := selected(t, l)
			for _, s := range chosen.catalogue.Describe(values) {
				if s.Kind.Type == ipcam.Choice && len(s.Kind.Options) == 0 {
					t.Errorf("%s offers nothing", s.Key)
				}
				if s.Kind.Type == ipcam.ReadOnly {
					continue
				}
				applied, err := l.Apply(ctx, s.Key, s.Value)
				if err != nil || applied.Changed[s.Key] != s.Value || applied.Restart || applied.Address != "" {
					t.Errorf("%s: %+v, %v", s.Key, applied, err)
					continue
				}
				name := chosen.bindings[s.Key].from.name()
				if written := lastWritten(fake, name); !dvriptest.SameJSON(t, written, recorded[name]) {
					t.Errorf("%s wrote %s where the device had %s", s.Key, written, recorded[name])
				}
			}
		})
	}
}

// Settings are read from the configurations they live in, and no others.
func TestValuesReadWhatTheyNeed(t *testing.T) {
	fake := dvriptest.New(t, "techage", dvriptest.Options{})
	l := dial(t, fake)
	values, err := l.Values(context.Background(), []ipcam.Key{"ntp.server", "ntp.port"})
	if err != nil || len(values) != 2 || values["ntp.port"] != 123 {
		t.Fatalf("values %v, %v", values, err)
	}
	for name, reads := range map[string]int{dvrip.NetNTP.Name: 1, dvrip.General.Name: 0, dvrip.NetCommon.Name: 0, dvrip.CameraParam.Channel(channel).Name: 0} {
		if got := len(fake.Commands(name)); got != reads {
			t.Errorf("%s read %d times", name, got)
		}
	}
}

// A camera's flags say which SmartEncode it has, the second version where it has both.
func TestSmartEncodeByTheFlags(t *testing.T) {
	main, extra := ipcam.StreamSetting(ipcam.Main, "smartEncode"), ipcam.StreamSetting(ipcam.Extra, "smartEncode")
	tests := map[string]struct {
		main  configuration
		extra bool
	}{
		"techage":             {smartV2, true},
		"techage-hi3516ev100": {smartV1, false},
		"xm530":               {smartV2, true},
	}
	for device, want := range tests {
		chosen := selected(t, dial(t, dvriptest.New(t, device, dvriptest.Options{})))
		_, hasExtra := chosen.bindings[extra]
		if b := chosen.bindings[main]; b == nil || b.from != want.main || hasExtra != want.extra {
			t.Errorf("%s: main %v, extra %v", device, b, hasExtra)
		}
	}
}

// A description or a configuration the camera does not keep leaves out the settings
// that depend on it.
func TestAbsences(t *testing.T) {
	fake := dvriptest.New(t, "techage", dvriptest.Options{Replies: map[string]json.RawMessage{
		"SystemFunction":     nil,
		"MultiLanguage":      nil,
		"MultiVstd":          nil,
		"NetWork.IPAdaptive": nil,
		"EncodeCapability":   json.RawMessage(`{"EncodeInfo":[{"StreamType":"MainStream","ResolutionMask":"0x00004000"}],"MaxBitrate":1}`),
	}})
	l := dial(t, fake)
	catalogue, err := l.Catalogue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []ipcam.Key{"general.language", "general.videoStandard", ipcam.StreamSetting(ipcam.Extra, "resolution"), ipcam.StreamSetting(ipcam.Main, "smartEncode")} {
		if _, err := catalogue.Lookup(key); err == nil {
			t.Errorf("has %s", key)
		}
	}
	resolution, err := catalogue.Lookup(ipcam.StreamSetting(ipcam.Main, "resolution"))
	if resolutions := resolution.Options; err != nil || len(resolutions) != 1 || resolutions[0] != (ipcam.Option{Value: "1080P", Label: "1080P"}) {
		t.Errorf("main resolutions %v, %v", resolutions, err)
	}
	values, err := l.Values(context.Background(), xmeye.Keys())
	if err != nil {
		t.Fatal(err)
	}
	if _, has := values["network.adaptiveIP"]; has {
		t.Error("read a configuration the camera does not keep")
	}
	if values["network.dhcp"] != true {
		t.Errorf("DHCP %v", values["network.dhcp"])
	}
}

func TestRefusals(t *testing.T) {
	fake := dvriptest.New(t, "techage", dvriptest.Options{Refuse: map[string]int{"General.General": 107, "OPTimeQuery": 107, "NetWork.NetNTP": 107}})
	l := dial(t, fake)
	ctx := context.Background()
	var refused *ipcam.Refused
	if _, err := l.Values(ctx, xmeye.Keys()); !errors.As(err, &refused) || !strings.Contains(refused.Message, "107") {
		t.Errorf("values: %v", err)
	}
	if _, err := l.Clock(ctx); !errors.As(err, &refused) {
		t.Errorf("clock: %v", err)
	}
	if _, err := l.Apply(ctx, "ntp.port", 123); !errors.As(err, &refused) {
		t.Errorf("a write: %v", err)
	}
	var unknown *ipcam.UnknownSetting
	if _, err := dial(t, dvriptest.New(t, "techage-hi3516ev100", dvriptest.Options{})).Apply(ctx, ipcam.StreamSetting(ipcam.Extra, "smartEncode"), true); !errors.As(err, &unknown) {
		t.Errorf("a setting the camera does not have: %v", err)
	}
}

// A configuration of a shape a binding cannot reach into fails the read.
func TestShapes(t *testing.T) {
	l := dial(t, dvriptest.New(t, "techage", dvriptest.Options{Replies: map[string]json.RawMessage{"Camera.ClearFog": json.RawMessage(`[]`)}}))
	if _, err := l.Values(context.Background(), xmeye.Keys()); err == nil || !strings.Contains(err.Error(), "Camera.ClearFog has no such field") {
		t.Fatalf("got %v", err)
	}
	if _, err := l.Apply(context.Background(), "camera.defog", true); err == nil {
		t.Fatal("wrote into a configuration of no such field")
	}
}

// A write that moves the camera says where it went.
func TestWritesThatMove(t *testing.T) {
	l := dial(t, dvriptest.New(t, "techage", dvriptest.Options{}))
	ctx := context.Background()
	values, err := l.Values(ctx, xmeye.Keys())
	if err != nil {
		t.Fatal(err)
	}
	applied, err := l.Apply(ctx, "network.port", 34570)
	if want := net.JoinHostPort(values["network.address"].(string), "34570"); err != nil || applied.Address != want || applied.Changed["network.port"] != 34570 || applied.Changed["network.httpPort"] != values["network.httpPort"] {
		t.Fatalf("applied %+v, %v; want at %s", applied, err, want)
	}
	if applied, err := l.Apply(ctx, "network.httpPort", 8080); err != nil || applied.Address != "" {
		t.Fatalf("applied %+v, %v", applied, err)
	}
	// A camera that keeps its port stays where it is.
	l = dial(t, dvriptest.New(t, "techage", dvriptest.Options{Ignores: map[string]bool{dvrip.NetCommon.Name: true}}))
	if applied, err := l.Apply(ctx, "network.port", 34570); err != nil || applied.Address != "" || applied.Changed["network.port"] != values["network.port"] {
		t.Fatalf("applied %+v, %v", applied, err)
	}
}

func TestRestart(t *testing.T) {
	l := dial(t, dvriptest.New(t, "techage", dvriptest.Options{SetStatus: map[string]int{dvrip.SimplifyEncode.Name: 603}}))
	if applied, err := l.Apply(context.Background(), ipcam.StreamSetting(ipcam.Extra, "codec"), string(ipcam.H265)); err != nil || !applied.Restart {
		t.Fatalf("applied %+v, %v", applied, err)
	}
}

func TestDialFailures(t *testing.T) {
	dialling := func(options dvriptest.Options) error {
		_, err := Driver{}.Dial(context.Background(), dvriptest.New(t, "techage", options).Address(), "admin", "")
		return err
	}
	if err := dialling(dvriptest.Options{Password: "secret"}); !errors.Is(err, ipcam.ErrCredentials) {
		t.Errorf("wrong password: %v", err)
	}
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := closed.Addr().String()
	closed.Close()
	if _, err := (Driver{}).Dial(context.Background(), address, "admin", ""); !errors.Is(err, ipcam.ErrUnreachable) {
		t.Errorf("closed port: %v", err)
	}
	for name, options := range map[string]dvriptest.Options{
		"no report":  {Refuse: map[string]int{"SystemInfo": 107}},
		"no channel": {Replies: map[string]json.RawMessage{"Simplify.Encode": json.RawMessage(`[]`)}},
	} {
		if err := dialling(options); !errors.Is(err, ipcam.ErrUnrecognised) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A capability the camera does not describe fails its settings, and leaves the login
// and its streams to the rest.
func TestCapabilityFailures(t *testing.T) {
	ctx := context.Background()
	for name, test := range map[string]struct {
		options dvriptest.Options
		// What the failure says.
		says string
	}{
		"functions refused": {dvriptest.Options{Refuse: map[string]int{"SystemFunction": 107}}, "SystemFunction: 107"},
		"encoding refused":  {dvriptest.Options{Refuse: map[string]int{"EncodeCapability": 107}}, "EncodeCapability: 107"},
		"languages refused": {dvriptest.Options{Refuse: map[string]int{"MultiLanguage": 107}}, "MultiLanguage: 107"},
		"standards refused": {dvriptest.Options{Refuse: map[string]int{"MultiVstd": 107}}, "MultiVstd: 107"},
		"an unnamed size": {
			dvriptest.Options{Replies: map[string]json.RawMessage{"EncodeCapability": json.RawMessage(`{"EncodeInfo":[{"StreamType":"MainStream","ResolutionMask":"0x04000000"}]}`)}},
			"no name for resolution mask bit 26",
		},
	} {
		l := dial(t, dvriptest.New(t, "techage", test.options))
		if _, err := l.Catalogue(ctx); err == nil || !strings.Contains(err.Error(), test.says) {
			t.Errorf("%s: catalogue %v", name, err)
		}
		if _, err := l.Values(ctx, xmeye.Keys()); err == nil || !strings.Contains(err.Error(), test.says) {
			t.Errorf("%s: values %v", name, err)
		}
		if _, err := l.Apply(ctx, "ntp.port", 123); err == nil || !strings.Contains(err.Error(), test.says) {
			t.Errorf("%s: a write %v", name, err)
		}
		if streams := l.Streams(); streams[ipcam.Main].Codec != ipcam.H265 {
			t.Errorf("%s: streams %+v", name, streams)
		}
	}
}

// A write the camera restarts its network on can end the login before it is read back:
// the write is taken, and the camera answers where it was moved to.
func TestWritesThatEndTheLogin(t *testing.T) {
	l := dial(t, dvriptest.New(t, "techage", dvriptest.Options{Restarts: map[string]bool{dvrip.NetCommon.Name: true}, RebootPause: time.Hour}))
	ctx := context.Background()
	values, err := l.Values(ctx, xmeye.Keys())
	if err != nil {
		t.Fatal(err)
	}
	applied, err := l.Apply(ctx, "network.port", 34570)
	if want := net.JoinHostPort(values["network.address"].(string), "34570"); err != nil || applied.Address != want || len(applied.Changed) != 0 {
		t.Fatalf("applied %+v, %v; want at %s", applied, err, want)
	}
	select {
	case <-l.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the login outlived the restart")
	}
}

func TestStreams(t *testing.T) {
	if streams := dial(t, dvriptest.New(t, "techage", dvriptest.Options{})).Streams(); streams[ipcam.Main] != (ipcam.StreamFormat{Codec: ipcam.H265, Audio: true}) || streams[ipcam.Extra].Codec != ipcam.H265 {
		t.Errorf("streams %+v", streams)
	}
	fake := dvriptest.New(t, "techage", dvriptest.Options{})
	if err := fake.Overlay(dvrip.SimplifyEncode.Name, json.RawMessage(`[{"ExtraFormat":{"AudioEnable":false,"Video":{"Compression":"H.264"}}}]`)); err != nil {
		t.Fatal(err)
	}
	if extra := dial(t, fake).Streams()[ipcam.Extra]; extra != (ipcam.StreamFormat{Codec: ipcam.H264}) {
		t.Errorf("extra %+v", extra)
	}
}

func TestOperations(t *testing.T) {
	l := dial(t, dvriptest.New(t, "techage", dvriptest.Options{RebootPause: time.Hour}))
	ctx := context.Background()
	set := time.Date(2026, 10, 14, 9, 30, 0, 0, time.UTC)
	if err := l.SetClock(ctx, set); err != nil {
		t.Fatal(err)
	}
	if clock, err := l.Clock(ctx); err != nil || clock.Sub(set) > 2*time.Second || clock.Before(set) {
		t.Fatalf("clock %v, %v", clock, err)
	}
	if err := l.Reboot(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-l.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the reboot did not end the login")
	}
	if l.Err() == nil {
		t.Fatal("ended for no reason")
	}
}

func TestCodecs(t *testing.T) {
	days := enum(map[string]string{"never": "Never"})
	if _, err := days.read("Someday"); err == nil {
		t.Error("read a value of no option")
	}
	if _, err := days.write("someday"); err == nil {
		t.Error("wrote an option of no value")
	}
	if _, err := days.write(true); err == nil {
		t.Error("wrote a value of another kind")
	}
	rule := toggle(dvrip.DSTOn, dvrip.DSTOff)
	if on, err := rule.read(dvrip.DSTOn); err != nil || on != true {
		t.Errorf("on read as %v, %v", on, err)
	}
	if _, err := rule.read("Sometimes"); err == nil {
		t.Error("read a switch neither on nor off")
	}
	if off, err := rule.write(false); err != nil || off != dvrip.DSTOff {
		t.Errorf("off written as %v, %v", off, err)
	}
	if _, err := dotted.write("not an address"); err == nil {
		t.Error("wrote an address of no form")
	}
	if _, err := dotted.write(1); err == nil {
		t.Error("wrote an address of another kind")
	}
	if west, err := zone.write("+12:00"); err != nil || west != -720 {
		t.Errorf("+12:00 written as %d, %v", west, err)
	}
	if _, err := zone.write("noon"); err == nil {
		t.Error("wrote a zone of no offset")
	}
	if _, err := zone.write(1); err == nil {
		t.Error("wrote a zone of another kind")
	}
	if codec, err := compression.read("MJPEG"); err != nil || codec != "MJPEG" {
		t.Errorf("a codec no table names read as %v, %v", codec, err)
	}
	if name, err := compression.write("H.264"); err != nil || name != "H.264" {
		t.Errorf("H.264 written as %q, %v", name, err)
	}
	if _, err := compression.write("MJPEG"); err == nil {
		t.Error("wrote a codec no camera names")
	}
	if _, err := flag.write(1); err == nil {
		t.Error("wrote a switch of another kind")
	}
	if _, err := hexNumber.write("1"); err == nil {
		t.Error("wrote a number of another kind")
	}
	if _, err := named[dvrip.VideoFormat]().write(1); err == nil {
		t.Error("wrote a name of another kind")
	}
	for coordinate, fraction := range map[int]float64{0: 0, 7728: 0.943359375, 8192: 1} {
		if got, err := share.read(coordinate); err != nil || got != fraction {
			t.Errorf("%d read as %v, %v", coordinate, got, err)
		}
		if got, err := share.write(fraction); err != nil || got != coordinate {
			t.Errorf("%v written as %d, %v", fraction, got, err)
		}
	}
	if got, err := share.write(0.9434); err != nil || got != 7728 {
		t.Errorf("94.34 %% written as %d, %v", got, err)
	}
	if _, err := share.write("half"); err == nil {
		t.Error("wrote a share of another kind")
	}
}

// The name renders as the bitmap the camera drew, and was seen drawing, for it.
func TestTitleOf(t *testing.T) {
	drawn, err := os.ReadFile("testdata/pond.title")
	if err != nil {
		t.Fatal(err)
	}
	if title, err := titleOf("pond"); err != nil || title.Width != 40 || title.Height != 20 || !slices.Equal(title.Rows, drawn) {
		t.Fatalf("rendered %d×%d, %v", title.Width, title.Height, err)
	}
	var invalid *ipcam.InvalidValue
	if _, err := titleOf("カメラ"); !errors.As(err, &invalid) || invalid.Problem != `'カ' is a character the camera's title cannot be drawn in` {
		t.Fatalf("rendered a name the font lacks: %v", err)
	}
	if _, err := drawing(1); err == nil {
		t.Fatal("drew a name of another kind")
	}
}

// A name written is stored and drawn; one the font lacks a character of is neither.
func TestTheNameIsDrawn(t *testing.T) {
	fake := dvriptest.New(t, "techage-hi3516ev100", dvriptest.Options{})
	l := dial(t, fake)
	ctx := context.Background()
	applied, err := l.Apply(ctx, "overlay.name", "porch")
	if err != nil || applied.Changed["overlay.name"] != "porch" {
		t.Fatalf("applied %+v, %v", applied, err)
	}
	want, err := titleOf("porch")
	if err != nil {
		t.Fatal(err)
	}
	drawn := func() dvrip.Bitmap {
		width, height, rows, ok := fake.Title()
		if !ok {
			t.Fatal("drew nothing")
		}
		return dvrip.Bitmap{Width: width, Height: height, Rows: rows}
	}
	if got := drawn(); !reflect.DeepEqual(got, want) {
		t.Fatalf("drew %+v", got)
	}
	name := dvrip.VideoWidget.Channel(channel).Name
	writes := len(fake.Commands(name))
	var invalid *ipcam.InvalidValue
	if _, err := l.Apply(ctx, "overlay.name", "カメラ"); !errors.As(err, &invalid) {
		t.Fatalf("a name the font lacks: %v", err)
	}
	if len(fake.Commands(name)) != writes {
		t.Fatal("a name the font lacks reached the camera")
	}
	if !reflect.DeepEqual(drawn(), want) {
		t.Fatal("a name the font lacks was drawn")
	}
}
