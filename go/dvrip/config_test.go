package dvrip

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"greendrake.nz/go/dvrip/dvriptest"
)

type roundTripCase struct {
	name string
	run  func(ctx context.Context, s *Session) error
}

// roundTrip reads a configuration as its type and writes back what it read.
func roundTrip[T any](c Config[T]) roundTripCase {
	return roundTripCase{name: c.Name, run: func(ctx context.Context, s *Session) error {
		v, err := c.Get(ctx, s)
		if err != nil {
			return err
		}
		_, err = c.Set(ctx, s, v)
		return err
	}}
}

// Writing back what was read must leave the device's configuration exactly as it was:
// what proves each type's keys and value encodings are the device's own, on every
// recorded device that has the configuration.
func TestConfigsWriteBackWhatTheyRead(t *testing.T) {
	cases := []roundTripCase{
		roundTrip(General),
		roundTrip(Location),
		roundTrip(AutoMaintain),
		roundTrip(OnlineUpgrade),
		roundTrip(TimeZone),
		roundTrip(NetCommon),
		roundTrip(IPAdaptive),
		roundTrip(NetDHCP),
		roundTrip(NetDNS),
		roundTrip(OnvifPwdCheckout),
		roundTrip(Correspondent),
		roundTrip(NetNTP),
		roundTrip(SimplifyEncode),
		roundTrip(SmartH264.All()),
		roundTrip(SmartH264V2.Channel(0)),
		roundTrip(CameraParam.Channel(0)),
		roundTrip(CameraParamEx.Channel(0)),
		roundTrip(ClearFog.All()),
		roundTrip(VideoWidget.Channel(0)),
	}
	covered := map[string]bool{}
	for _, device := range recordedDevices {
		t.Run(device, func(t *testing.T) {
			camera := dvriptest.New(t, device, dvriptest.Options{})
			s := dial(t, camera)
			recorded := replies(t, device)
			for _, c := range cases {
				if recorded[c.name] == nil {
					continue
				}
				covered[c.name] = true
				t.Run(c.name, func(t *testing.T) {
					if err := c.run(context.Background(), s); err != nil {
						t.Fatal(err)
					}
					if written := lastSet(t, camera, c.name); !dvriptest.SameJSON(t, written, recorded[c.name]) {
						t.Fatalf("wrote back\n%s\nwhere the device had\n%s", written, recorded[c.name])
					}
				})
			}
		})
	}
	for _, c := range cases {
		if !covered[c.name] {
			t.Errorf("no device recorded %s", c.name)
		}
	}
}

func TestConfigsDecode(t *testing.T) {
	camera := dvriptest.New(t, "techage", dvriptest.Options{})
	s := dial(t, camera)
	ctx := context.Background()

	netCommon, err := NetCommon.Get(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	want := NetCommonConfig{
		HostIP:        IPv4{netip.MustParseAddr("198.51.100.10")},
		Submask:       IPv4{netip.MustParseAddr("255.255.255.0")},
		GateWay:       IPv4{netip.MustParseAddr("198.51.100.1")},
		MAC:           "00:00:5e:00:53:01",
		TCPPort:       34567,
		HttpPort:      80,
		TransferPlan:  TransferQuality,
		UseHSDownLoad: false,
	}
	if netCommon != want {
		t.Fatalf("NetCommon decoded as %+v", netCommon)
	}

	upgrade, err := OnlineUpgrade.Get(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	// The device writes this one's switches in both forms: AutoCheck true, AutoUpgradeImp 0.
	if upgrade != (OnlineUpgradeConfig{Enable: true, AutoCheck: true}) {
		t.Fatalf("NetWork.OnlineUpgrade decoded as %+v", upgrade)
	}

	param, err := CameraParam.Channel(0).Get(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if param.ExposureParam.LeastTime != 0x100 || param.ExposureParam.MostTime != 0x10000 || param.EsShutter != SlowShutterLow || param.DayNfLevel != 3 ||
		!param.GainParam.AutoGain || param.GainParam.Gain != 50 || param.DayNightColor != DayNightStarIR || param.BLCMode != BacklightOff ||
		param.WhiteBalance != WhiteBalanceAuto || param.IRCUTMode != IRCutSynchronous || param.ApertureMode || param.PictureFlip {
		t.Fatalf("Camera.Param decoded as %+v", param)
	}

	paramEx, err := CameraParamEx.Channel(0).Get(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if paramEx != (CameraParamExConfig{Style: ImageStyle2, BroadTrends: Gain{Gain: 50}, AutomaticAdjustment: 3}) {
		t.Fatalf("Camera.ParamEx decoded as %+v", paramEx)
	}

	location, err := Location.Get(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if location.TimeFormat != Time24Hour || location.VideoFormat != PAL || location.DSTStart != (DSTPoint{Year: 2027, Month: 5, Day: 1, Hour: 1, Minute: 1}) {
		t.Fatalf("General.Location decoded as %+v", location)
	}

	correspondent, err := Correspondent.Get(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if correspondent.Port != 8899 {
		t.Fatalf("OEMcfg.Correspondent decoded as %+v", correspondent)
	}

	encode, err := SimplifyEncode.Get(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(encode) != 1 || !encode[0].MainFormat.AudioEnable || encode[0].MainFormat.Video.Resolution != "4K" || encode[0].MainFormat.Video.Compression != "H.265" ||
		encode[0].MainFormat.Video.BitRateControl != VBR || encode[0].ExtraFormat.Video.BitRate != 1024 {
		t.Fatalf("Simplify.Encode decoded as %+v", encode)
	}

	functions, err := SystemFunction.Get(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if !functions["PreviewFunction"]["Talk"] || functions["PreviewFunction"]["Tour"] || !functions["EncodeFunction"]["SmartH264V2"] {
		t.Fatalf("SystemFunction decoded as %v", functions)
	}

	capability, err := EncodeCapability.Get(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(capability.EncodeInfo) != 5 || capability.EncodeInfo[1].StreamType != "ExtraStream2" || capability.MaxBitrate != 49152 {
		t.Fatalf("EncodeCapability decoded as %+v", capability)
	}
	for stream, want := range map[StreamType][]string{Main: {"1080P", "3M", "5M", "4M", "4K"}, Extra: {"D1", "HD1", "CIF", "QVGA"}} {
		described, err := capability.Stream(stream)
		if err != nil {
			t.Fatal(err)
		}
		resolutions, err := described.Resolutions()
		if err != nil || !slices.Equal(resolutions, want) {
			t.Fatalf("%s stream resolutions %v, %v", stream, resolutions, err)
		}
	}
	if _, err := (EncodeCapabilities{EncodeInfo: capability.EncodeInfo[:1]}).Stream(Extra); err == nil {
		t.Fatal("a device describing no extra stream described one")
	}

	languages, err := MultiLanguage.Get(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	standards, err := MultiVstd.Get(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(languages) != 22 || languages[2] != "English" || standards != "PAL|NTSC" {
		t.Fatalf("decoded languages %v, standards %q", languages, standards)
	}
}

func TestTimeZoneDecodes(t *testing.T) {
	camera := dvriptest.New(t, "techage", dvriptest.Options{})
	zone, err := TimeZone.Get(context.Background(), dial(t, camera))
	if err != nil || zone.MinutesWest != -480 {
		t.Fatalf("System.TimeZone decoded as %+v, %v", zone, err)
	}
}

func TestConfigSetKeepsWhatTheTypeLeavesOut(t *testing.T) {
	camera := dvriptest.New(t, "techage", dvriptest.Options{})
	s := dial(t, camera)
	ctx := context.Background()
	general, err := General.Get(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	general.LocalNo = 7
	if _, err := General.Set(ctx, s, general); err != nil {
		t.Fatal(err)
	}
	written := lastSet(t, camera, General.Name)
	want := `{"AutoLogout":0,"FontSize":24,"IranCalendarEnable":0,"LocalNo":7,"MachineName":"LocalHost","OverWrite":"OverWrite","ScreenAutoShutdown":10,"ScreenSaveTime":0,"VideoOutPut":"Auto"}`
	if !dvriptest.SameJSON(t, written, json.RawMessage(want)) {
		t.Fatalf("wrote %s", written)
	}
}

func TestSetReportsARequiredRestart(t *testing.T) {
	camera := dvriptest.New(t, "techage", dvriptest.Options{SetStatus: map[string]int{"General.General": 603}})
	s := dial(t, camera)
	restart, err := s.Set(context.Background(), "General.General", map[string]int{"LocalNo": 1})
	if err != nil || !restart {
		t.Fatalf("got restart %v, %v; want a restart required", restart, err)
	}
	if restart, err := s.Set(context.Background(), "General.Location", map[string]string{"DateFormat": "DDMMYY"}); err != nil || restart {
		t.Fatalf("another configuration got restart %v, %v", restart, err)
	}
}

func TestRefusedConfigurationIsARetError(t *testing.T) {
	camera := dvriptest.New(t, "techage", dvriptest.Options{Refuse: map[string]int{"General.General": 107}})
	s := dial(t, camera)
	var retErr *RetError
	if _, err := s.Set(context.Background(), "General.General", map[string]int{"LocalNo": 1}); !errors.As(err, &retErr) || retErr.Ret != 107 {
		t.Fatalf("got %v, want 107", err)
	}
	if _, err := s.Get(context.Background(), "No.Such"); !errors.As(err, &retErr) || retErr.Ret != 607 || retErr.Request != "No.Such" {
		t.Fatalf("got %v, want 607 for No.Such", err)
	}
}

func TestChannelConfigNames(t *testing.T) {
	if got := CameraParam.Channel(2).Name; got != "Camera.Param.[2]" {
		t.Fatalf("channel name %q", got)
	}
	if got := CameraParam.All().Name; got != "Camera.Param" {
		t.Fatalf("all-channel name %q", got)
	}
}

func TestWritesRefusedBeforeReachingTheDevice(t *testing.T) {
	camera := dvriptest.New(t, "techage", dvriptest.Options{Refuse: map[string]int{"NetWork.NetDNS": 107}})
	s := dial(t, camera)
	ctx := context.Background()
	// An address left unset has no form the device would take.
	if _, err := NetCommon.Set(ctx, s, NetCommonConfig{}); err == nil {
		t.Fatal("a configuration without addresses should not be written")
	}
	if _, err := s.Set(ctx, "General.General", make(chan int)); err == nil {
		t.Fatal("a value with no JSON form should not be written")
	}
	// Writing starts by reading what the device has.
	var retErr *RetError
	if _, err := NetDNS.Set(ctx, s, NetDNSConfig{}); !errors.As(err, &retErr) || retErr.Ret != 107 {
		t.Fatalf("got %v, want the read refused", err)
	}
	if len(camera.Commands("General.General")) != 0 || len(camera.Commands("NetWork.NetCommon")) != 1 {
		t.Fatal("nothing should have been written")
	}
	if err := s.Err(); err != nil {
		t.Fatalf("refused writes ended the session: %v", err)
	}
}

func TestReplyOfTheWrongShape(t *testing.T) {
	camera := dvriptest.New(t, "techage", dvriptest.Options{})
	s := dial(t, camera)
	if _, err := (AbilityOf[string]{Name: "MultiLanguage"}).Get(context.Background(), s); err == nil || !strings.Contains(err.Error(), "decoding string") {
		t.Fatalf("got %v", err)
	}
}

func TestFlag(t *testing.T) {
	forms := map[string]Flag{`true`: true, `1`: true, `"0x00000001"`: true, `"0x1"`: true, `false`: false, `0`: false, `"0x00000000"`: false, `"0x0"`: false}
	for data, want := range forms {
		var f Flag
		if err := json.Unmarshal([]byte(data), &f); err != nil || f != want {
			t.Fatalf("%s unmarshalled as %v, %v", data, f, err)
		}
	}
	for _, bad := range []string{`2`, `"1"`, `"0x00000002"`, `"0x2"`, `"0x"`, `null`, `{`} {
		var f Flag
		if err := f.UnmarshalJSON([]byte(bad)); err == nil {
			t.Fatalf("%s should not be a Flag", bad)
		}
	}
}

func TestEncodeFormatByStream(t *testing.T) {
	e := EncodeChannel{MainFormat: EncodeFormat{Video: VideoEncoding{Resolution: "4K"}}, ExtraFormat: EncodeFormat{Video: VideoEncoding{Resolution: "D1"}}}
	if e.Format(Main).Video.Resolution != "4K" || e.Format(Extra).Video.Resolution != "D1" {
		t.Fatal("formats crossed")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("a stream that does not exist has a format")
		}
	}()
	e.Format("Extra2")
}

func TestUnnamedResolutionBit(t *testing.T) {
	if _, err := (StreamCapability{ResolutionMask: 1 << 26}).Resolutions(); err == nil {
		t.Fatal("resolution bit 26 named")
	}
}

// A stream with no sizes has an empty set of them, which encodes as a list.
func TestNoResolutions(t *testing.T) {
	resolutions, err := StreamCapability{}.Resolutions()
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := json.Marshal(resolutions); string(data) != "[]" {
		t.Fatalf("encoded as %s", data)
	}
}

func TestHexAndIPv4(t *testing.T) {
	data, err := json.Marshal(Hex(0x100))
	if err != nil || string(data) != `"0x00000100"` {
		t.Fatalf("Hex marshalled as %s, %v", data, err)
	}
	var h Hex
	if err := json.Unmarshal([]byte(`"0x100"`), &h); err != nil || h != 0x100 {
		t.Fatalf("Hex unmarshalled as %#x, %v", h, err)
	}
	for _, bad := range []string{`"zz"`, `256`, `"256"`, `"0x"`, `"0x100000000"`} {
		if err := json.Unmarshal([]byte(bad), &h); err == nil {
			t.Fatalf("%s should not be a Hex", bad)
		}
	}
	address := IPv4{netip.MustParseAddr("203.0.113.114")}
	data, err = json.Marshal(address)
	if err != nil || string(data) != `"0x727100CB"` {
		t.Fatalf("IPv4 marshalled as %s, %v", data, err)
	}
	var back IPv4
	if err := json.Unmarshal(data, &back); err != nil || back != address {
		t.Fatalf("IPv4 unmarshalled as %v, %v", back, err)
	}
	if _, err := json.Marshal(IPv4{netip.MustParseAddr("::1")}); err == nil {
		t.Fatal("an IPv6 address should not marshal")
	}
	if err := json.Unmarshal([]byte(`"nope"`), &back); err == nil {
		t.Fatal("a non-hex address should not unmarshal")
	}
}

func TestOperations(t *testing.T) {
	export := []byte("PK\x03\x04 zip")
	camera := dvriptest.New(t, "xm530", dvriptest.Options{Export: export})
	s := dial(t, camera)
	ctx := context.Background()

	info, err := s.SystemInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.SoftWareVersion != "V5.00.R02.0003068d.10010.349f00.0020000" || info.TalkOutChannel != 1 || info.DeviceRunTime != 0x1fcc14 {
		t.Fatalf("SystemInfo decoded as %+v", info)
	}

	// The camera's clock runs on from where the recorded device's stood.
	clockNear := func(want time.Time) {
		t.Helper()
		clock, err := s.Time(ctx)
		if err != nil || clock.Before(want) || clock.Sub(want) > 2*time.Second {
			t.Fatalf("time %v, %v; want %v", clock, err, want)
		}
	}
	clockNear(time.Date(2026, 10, 19, 5, 16, 49, 0, time.UTC))

	if err := s.SetTime(ctx, time.Date(2026, 10, 5, 21, 30, 0, 0, time.FixedZone("NZDT", 13*3600))); err != nil {
		t.Fatal(err)
	}
	var set string
	payload(t, camera.Commands("OPTimeSetting")[0], &set)
	if set != "2026-10-05 21:30:00" {
		t.Fatalf("set the clock to %q", set)
	}
	clockNear(time.Date(2026, 10, 5, 21, 30, 0, 0, time.UTC))

	got, err := s.ExportConfig(ctx)
	if err != nil || string(got) != string(export) {
		t.Fatalf("export %q, %v", got, err)
	}

	if err := s.Reboot(ctx); err != nil {
		t.Fatal(err)
	}
	var machine map[string]string
	payload(t, camera.Commands("OPMachine")[0], &machine)
	if machine["Action"] != "Reboot" {
		t.Fatalf("machine operation %v", machine)
	}
	waitFor(t, "the reboot to end the session", time.Second, ended(s))
}

// A title bitmap goes out as it is, and one of no size it could be not at all.
func TestDrawTitle(t *testing.T) {
	camera := dvriptest.New(t, "techage-hi3516ev100", dvriptest.Options{})
	s := dial(t, camera)
	ctx := context.Background()
	title := Bitmap{Width: 16, Height: 3, Rows: []byte{0x80, 0x01, 0xff, 0x00, 0x18, 0x81}}
	if err := s.DrawTitle(ctx, title); err != nil {
		t.Fatal(err)
	}
	drawn := func() Bitmap {
		width, height, rows, ok := camera.Title()
		if !ok {
			t.Fatal("drew nothing")
		}
		return Bitmap{Width: width, Height: height, Rows: rows}
	}
	if got := drawn(); !reflect.DeepEqual(got, title) {
		t.Fatalf("drew %+v", got)
	}
	if err := s.DrawTitle(ctx, Bitmap{Width: 12, Height: 1, Rows: []byte{0, 0}}); err == nil {
		t.Fatal("drew a bitmap 12 pixels wide")
	}
	if !reflect.DeepEqual(drawn(), title) {
		t.Fatal("a bitmap of no such size reached the camera")
	}
}

func TestOperationsReportTheDevicesStatus(t *testing.T) {
	camera := dvriptest.New(t, "techage", dvriptest.Options{
		Refuse:  map[string]int{"OPTimeQuery": 107, "SystemInfo": 107},
		Replies: map[string]json.RawMessage{"MultiVstd": nil},
	})
	s := dial(t, camera)
	ctx := context.Background()
	var retErr *RetError
	if _, err := s.Time(ctx); !errors.As(err, &retErr) {
		t.Fatalf("time got %v", err)
	}
	if _, err := s.SystemInfo(ctx); !errors.As(err, &retErr) {
		t.Fatalf("system info got %v", err)
	}
	if _, err := MultiVstd.Get(ctx, s); !errors.As(err, &retErr) || retErr.Ret != 607 {
		t.Fatalf("missing ability got %v", err)
	}
}
