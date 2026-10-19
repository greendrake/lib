package dvripcam

import (
	"net"
	"slices"
	"strconv"

	"greendrake.nz/go/dvrip"
	"greendrake.nz/go/ipcam"
)

// The configurations the settings live in.
var (
	general       = configOf(dvrip.General)
	location      = configOf(dvrip.Location)
	timeZone      = configOf(dvrip.TimeZone)
	autoMaintain  = configOf(dvrip.AutoMaintain)
	onlineUpgrade = configOf(dvrip.OnlineUpgrade)
	// The camera answers where its address and the protocol's port are.
	netCommon = &source[dvrip.NetCommonConfig]{config: dvrip.NetCommon, at: func(c *dvrip.NetCommonConfig) string {
		return net.JoinHostPort(c.HostIP.String(), strconv.Itoa(c.TCPPort))
	}}
	ipAdaptive    = configOf(dvrip.IPAdaptive)
	netDHCP       = configOf(dvrip.NetDHCP)
	netDNS        = configOf(dvrip.NetDNS)
	onvifCheck    = configOf(dvrip.OnvifPwdCheckout)
	correspondent = configOf(dvrip.Correspondent)
	netNTP        = configOf(dvrip.NetNTP)
	encode        = configOf(dvrip.SimplifyEncode)
	smartV1       = configOf(dvrip.SmartH264.All())
	smartV2       = configOf(dvrip.SmartH264V2.Channel(channel))
	cameraParam   = configOf(dvrip.CameraParam.Channel(channel))
	cameraParamEx = configOf(dvrip.CameraParamEx.Channel(channel))
	clearFog      = configOf(dvrip.ClearFog.All())
	overlays      = configOf(dvrip.VideoWidget.Channel(channel))
)

// encodeFunction is whether the camera has the EncodeFunction flag of name.
func encodeFunction(name string) func(capabilities) bool {
	return func(caps capabilities) bool { return caps.functions["EncodeFunction"][name] }
}

// dstPoint binds a daylight saving point's settings.
func dstPoint(point ipcam.DSTPoint, at func(*dvrip.LocationConfig) *dvrip.DSTPoint) []*binding {
	fields := []struct {
		name string
		at   func(*dvrip.DSTPoint) *int
	}{
		{"month", func(p *dvrip.DSTPoint) *int { return &p.Month }},
		{"week", func(p *dvrip.DSTPoint) *int { return &p.Week }},
		{"day", func(p *dvrip.DSTPoint) *int { return &p.Day }},
		{"hour", func(p *dvrip.DSTPoint) *int { return &p.Hour }},
		{"minute", func(p *dvrip.DSTPoint) *int { return &p.Minute }},
	}
	bound := make([]*binding, len(fields))
	for i, f := range fields {
		bound[i] = field(location, ipcam.DSTSetting(point, f.name), func(c *dvrip.LocationConfig) *int { return f.at(at(c)) }, same[int]())
	}
	return bound
}

// overlaid binds whether an overlay is drawn, and its Placement.
func overlaid(show ipcam.Key, overlay string, at func(*dvrip.VideoWidgetConfig) *dvrip.Overlay) []*binding {
	left, top := ipcam.Placement(overlay)
	return []*binding{
		field(overlays, show, func(c *dvrip.VideoWidgetConfig) *dvrip.Flag { return &at(c).EncodeBlend }, flag),
		field(overlays, left, func(c *dvrip.VideoWidgetConfig) *int { return &at(c).RelativePos[0] }, share),
		field(overlays, top, func(c *dvrip.VideoWidgetConfig) *int { return &at(c).RelativePos[1] }, share),
	}
}

// encoding binds a stream's encoding.
func encoding(stream ipcam.StreamKey) []*binding {
	key := func(name string) ipcam.Key { return ipcam.StreamSetting(stream, name) }
	format := func(c *[]dvrip.EncodeChannel) *dvrip.EncodeFormat { return (*c)[channel].Format(streamTypes[stream]) }
	video := func(c *[]dvrip.EncodeChannel) *dvrip.VideoEncoding { return &format(c).Video }
	return []*binding{
		field(encode, key("video"), func(c *[]dvrip.EncodeChannel) *bool { return &format(c).VideoEnable }, same[bool]()),
		field(encode, key("audio"), func(c *[]dvrip.EncodeChannel) *bool { return &format(c).AudioEnable }, same[bool]()),
		field(encode, key("codec"), func(c *[]dvrip.EncodeChannel) *string { return &video(c).Compression }, compression),
		field(encode, key("resolution"), func(c *[]dvrip.EncodeChannel) *string { return &video(c).Resolution }, same[string]()).
			only(func(caps capabilities) bool { return len(caps.resolutions[stream]) > 0 }).
			adding(func(caps capabilities) ipcam.Specifics {
				return ipcam.Specifics{Options: offered(caps.resolutions[stream])}
			}),
		field(encode, key("frameRate"), func(c *[]dvrip.EncodeChannel) *int { return &video(c).FPS }, same[int]()),
		field(encode, key("bitRateControl"), func(c *[]dvrip.EncodeChannel) *dvrip.BitRateControl { return &video(c).BitRateControl },
			enum(map[string]dvrip.BitRateControl{"cbr": dvrip.CBR, "vbr": dvrip.VBR})),
		field(encode, key("quality"), func(c *[]dvrip.EncodeChannel) *int { return &video(c).Quality }, same[int]()),
		field(encode, key("bitRate"), func(c *[]dvrip.EncodeChannel) *int { return &video(c).BitRate }, same[int]()),
		field(encode, key("keyFrameInterval"), func(c *[]dvrip.EncodeChannel) *int { return &video(c).GOP }, same[int]()),
	}
}

// bindings bind every setting of the catalogue, a key's bindings in the order they are
// tried: the first that holds for the camera is the setting's.
var bindings = slices.Concat(
	[]*binding{
		field(timeZone, "general.timeZone", func(c *dvrip.TimeZoneConfig) *int { return &c.MinutesWest }, zone),
		field(location, "general.dateFormat", func(c *dvrip.LocationConfig) *dvrip.DateFormat { return &c.DateFormat },
			enum(map[string]dvrip.DateFormat{"ymd": dvrip.DateYYMMDD, "mdy": dvrip.DateMMDDYY, "dmy": dvrip.DateDDMMYY})),
		field(location, "general.dateSeparator", func(c *dvrip.LocationConfig) *string { return &c.DateSeparator }, same[string]()),
		field(location, "general.timeFormat", func(c *dvrip.LocationConfig) *dvrip.TimeFormat { return &c.TimeFormat },
			enum(map[string]dvrip.TimeFormat{"24h": dvrip.Time24Hour, "12h": dvrip.Time12Hour})),
		field(location, "general.language", func(c *dvrip.LocationConfig) *string { return &c.Language }, same[string]()).
			only(func(caps capabilities) bool { return len(caps.languages) > 0 }).
			adding(func(caps capabilities) ipcam.Specifics { return ipcam.Specifics{Options: offered(caps.languages)} }),
		field(location, "general.videoStandard", func(c *dvrip.LocationConfig) *dvrip.VideoFormat { return &c.VideoFormat }, named[dvrip.VideoFormat]()).
			only(func(caps capabilities) bool { return len(caps.standards) > 0 }).
			adding(func(caps capabilities) ipcam.Specifics { return ipcam.Specifics{Options: offered(caps.standards)} }),
		field(location, "general.dst", func(c *dvrip.LocationConfig) *dvrip.DSTRule { return &c.DSTRule }, toggle(dvrip.DSTOn, dvrip.DSTOff)),
	},
	dstPoint(ipcam.DSTStart, func(c *dvrip.LocationConfig) *dvrip.DSTPoint { return &c.DSTStart }),
	dstPoint(ipcam.DSTEnd, func(c *dvrip.LocationConfig) *dvrip.DSTPoint { return &c.DSTEnd }),
	[]*binding{
		field(general, "general.whenFull", func(c *dvrip.GeneralConfig) *dvrip.OverWrite { return &c.OverWrite },
			enum(map[string]dvrip.OverWrite{"overwrite": dvrip.OverWriteOldest, "stop": dvrip.OverWriteStop})),
		field(general, "general.deviceNumber", func(c *dvrip.GeneralConfig) *int { return &c.LocalNo }, same[int]()),
		field(general, "general.autoLogout", func(c *dvrip.GeneralConfig) *int { return &c.AutoLogout }, same[int]()),
	},
	encoding(ipcam.Main),
	encoding(ipcam.Extra),
	[]*binding{
		// The vendor's client takes the second version where a camera has both.
		field(smartV2, ipcam.StreamSetting(ipcam.Main, "smartEncode"), func(c *dvrip.SmartH264V2Config) *bool { return &c.Smart264V2[0].SmartH264 }, same[bool]()).
			only(encodeFunction("SmartH264V2")),
		field(smartV1, ipcam.StreamSetting(ipcam.Main, "smartEncode"), func(c *[]dvrip.SmartH264Config) *bool { return &(*c)[channel].SmartH264 }, same[bool]()).
			only(encodeFunction("SmartH264")),
		field(smartV2, ipcam.StreamSetting(ipcam.Extra, "smartEncode"), func(c *dvrip.SmartH264V2Config) *bool { return &c.Smart264V2[1].SmartH264 }, same[bool]()).
			only(encodeFunction("SmartH264V2")),

		field(netDHCP, "network.dhcp", func(c *[]dvrip.DHCPInterface) *bool { return &(*c)[0].Enable }, same[bool]()),
		field(netCommon, "network.address", func(c *dvrip.NetCommonConfig) *dvrip.IPv4 { return &c.HostIP }, dotted),
		field(netCommon, "network.mask", func(c *dvrip.NetCommonConfig) *dvrip.IPv4 { return &c.Submask }, dotted),
		field(netCommon, "network.gateway", func(c *dvrip.NetCommonConfig) *dvrip.IPv4 { return &c.GateWay }, dotted).
			adding(func(capabilities) ipcam.Specifics {
				return ipcam.Specifics{Note: "It keeps its old one when given 0.0.0.0; its own address leaves it with none."}
			}),
		field(netCommon, "network.port", func(c *dvrip.NetCommonConfig) *int { return &c.TCPPort }, same[int]()),
		field(netCommon, "network.httpPort", func(c *dvrip.NetCommonConfig) *int { return &c.HttpPort }, same[int]()),
		field(netCommon, "network.mac", func(c *dvrip.NetCommonConfig) *string { return &c.MAC }, same[string]()),
		field(netCommon, "network.transferPolicy", func(c *dvrip.NetCommonConfig) *dvrip.TransferPlan { return &c.TransferPlan },
			enum(map[string]dvrip.TransferPlan{"quality": dvrip.TransferQuality, "fluency": dvrip.TransferFluency, "autoAdapt": dvrip.TransferAutoAdapt})),
		field(netCommon, "network.highSpeedDownload", func(c *dvrip.NetCommonConfig) *bool { return &c.UseHSDownLoad }, same[bool]()),
		field(netDNS, "network.dns", func(c *dvrip.NetDNSConfig) *dvrip.IPv4 { return &c.Address }, dotted),
		field(netDNS, "network.spareDNS", func(c *dvrip.NetDNSConfig) *dvrip.IPv4 { return &c.SpareAddress }, dotted),
		field(ipAdaptive, "network.adaptiveIP", func(c *dvrip.IPAdaptiveConfig) *bool { return &c.IPAdaptive }, same[bool]()),
		field(correspondent, "network.onvifPort", func(c *dvrip.CorrespondentConfig) *int { return &c.Port }, same[int]()),
		field(onvifCheck, "network.onvifCheck", func(c *dvrip.OnvifPwdCheckoutConfig) *bool { return &c.Enable }, same[bool]()),

		field(netNTP, "ntp.enable", func(c *dvrip.NetNTPConfig) *bool { return &c.Enable }, same[bool]()),
		field(netNTP, "ntp.server", func(c *dvrip.NetNTPConfig) *string { return &c.Server.Name }, same[string]()),
		field(netNTP, "ntp.port", func(c *dvrip.NetNTPConfig) *int { return &c.Server.Port }, same[int]()),
		field(netNTP, "ntp.updatePeriod", func(c *dvrip.NetNTPConfig) *int { return &c.UpdatePeriod }, same[int]()),

		field(cameraParam, "camera.dayNight", func(c *dvrip.CameraParamConfig) *dvrip.DayNightMode { return &c.DayNightColor },
			enum(map[string]dvrip.DayNightMode{
				"auto": dvrip.DayNightStarIR, "color": dvrip.DayNightFullColor, "monochrome": dvrip.DayNightBlackWhite,
				"smartAlert": dvrip.DayNightSmartAlert, "smartLight": dvrip.DayNightIntelligentVariable, "smartInfrared": dvrip.DayNightIntelligentInfrared,
			})),
		field(cameraParam, "camera.exposure", func(c *dvrip.CameraParamConfig) *int { return &c.ExposureParam.Level }, same[int]()),
		field(cameraParam, "camera.exposureMin", func(c *dvrip.CameraParamConfig) *dvrip.Hex { return &c.ExposureParam.LeastTime }, hexNumber),
		field(cameraParam, "camera.exposureMax", func(c *dvrip.CameraParamConfig) *dvrip.Hex { return &c.ExposureParam.MostTime }, hexNumber),
		field(cameraParam, "camera.autoIris", func(c *dvrip.CameraParamConfig) *dvrip.Flag { return &c.ApertureMode }, flag),
		field(cameraParam, "camera.aeReference", func(c *dvrip.CameraParamConfig) *int { return &c.ElecLevel }, same[int]()),
		field(cameraParam, "camera.backlight", func(c *dvrip.CameraParamConfig) *dvrip.BacklightMode { return &c.BLCMode },
			enum(map[string]dvrip.BacklightMode{"off": dvrip.BacklightOff, "on": dvrip.BacklightOn, "auto": dvrip.BacklightAuto})),
		field(cameraParam, "camera.aeSensitivity", func(c *dvrip.CameraParamConfig) *int { return &c.AeSensitivity }, same[int]()),
		field(cameraParam, "camera.autoGain", func(c *dvrip.CameraParamConfig) *dvrip.Flag { return &c.GainParam.AutoGain }, flag),
		field(cameraParam, "camera.gainLimit", func(c *dvrip.CameraParamConfig) *int { return &c.GainParam.Gain }, same[int]()),
		field(cameraParam, "camera.slowShutter", func(c *dvrip.CameraParamConfig) *dvrip.SlowShutter { return &c.EsShutter },
			enum(map[string]dvrip.SlowShutter{"off": dvrip.SlowShutterOff, "low": dvrip.SlowShutterLow, "middle": dvrip.SlowShutterMiddle, "strong": dvrip.SlowShutterStrong})),
		field(cameraParam, "camera.irCut", func(c *dvrip.CameraParamConfig) *dvrip.IRCutMode { return &c.IRCUTMode },
			enum(map[string]dvrip.IRCutMode{"lights": dvrip.IRCutSynchronous, "automatic": dvrip.IRCutAutomatic})),
		field(cameraParam, "camera.dayNightLevel", func(c *dvrip.CameraParamConfig) *int { return &c.DncThr }, same[int]()),
		field(cameraParam, "camera.dayNoiseFilter", func(c *dvrip.CameraParamConfig) *int { return &c.DayNfLevel }, same[int]()),
		field(cameraParam, "camera.nightNoiseFilter", func(c *dvrip.CameraParamConfig) *int { return &c.NightNfLevel }, same[int]()),
		field(cameraParam, "camera.mirror", func(c *dvrip.CameraParamConfig) *dvrip.Flag { return &c.PictureMirror }, flag),
		field(cameraParam, "camera.flip", func(c *dvrip.CameraParamConfig) *dvrip.Flag { return &c.PictureFlip }, flag),
		field(cameraParam, "camera.antiFlicker", func(c *dvrip.CameraParamConfig) *dvrip.Flag { return &c.RejectFlicker }, flag),
		field(cameraParam, "camera.irSwap", func(c *dvrip.CameraParamConfig) *dvrip.Flag { return &c.IrcutSwap }, flag),
		field(cameraParam, "camera.whiteBalance", func(c *dvrip.CameraParamConfig) *dvrip.WhiteBalanceMode { return &c.WhiteBalance },
			enum(map[string]dvrip.WhiteBalanceMode{"auto": dvrip.WhiteBalanceAuto, "indoor": dvrip.WhiteBalanceIndoor, "outdoor": dvrip.WhiteBalanceOutdoor})),
		field(cameraParamEx, "camera.style", func(c *dvrip.CameraParamExConfig) *dvrip.ImageStyle { return &c.Style },
			enum(map[string]dvrip.ImageStyle{"style1": dvrip.ImageStyle1, "style2": dvrip.ImageStyle2, "style3": dvrip.ImageStyle3})),
		field(cameraParamEx, "camera.wdr", func(c *dvrip.CameraParamExConfig) *dvrip.Flag { return &c.BroadTrends.AutoGain }, flag),
		field(cameraParamEx, "camera.wdrLimit", func(c *dvrip.CameraParamExConfig) *int { return &c.BroadTrends.Gain }, same[int]()),
		field(cameraParamEx, "camera.gearControl", func(c *dvrip.CameraParamExConfig) *int { return &c.AutomaticAdjustment }, same[int]()),
		field(cameraParamEx, "camera.noRedFace", func(c *dvrip.CameraParamExConfig) *dvrip.Flag { return &c.PreventOverExpo }, flag),
		field(clearFog, "camera.defog", func(c *[]dvrip.ClearFogConfig) *dvrip.Flag { return &(*c)[channel].Enable }, flag),
		field(clearFog, "camera.defogLevel", func(c *[]dvrip.ClearFogConfig) *int { return &(*c)[channel].Level }, same[int]()),

		// What the camera draws as its name is the title drawing renders.
		field(overlays, "overlay.name", func(c *dvrip.VideoWidgetConfig) *string { return &c.ChannelTitle.Name }, same[string]()).doing(drawing),
	},
	overlaid("overlay.showName", "name", func(c *dvrip.VideoWidgetConfig) *dvrip.Overlay { return &c.ChannelTitleAttribute }),
	overlaid("overlay.showTime", "time", func(c *dvrip.VideoWidgetConfig) *dvrip.Overlay { return &c.TimeTitleAttribute }),
	[]*binding{
		field(autoMaintain, "maintenance.rebootDay", func(c *dvrip.AutoMaintainConfig) *string { return &c.AutoRebootDay },
			enum(map[string]string{
				"never": "Never", "everyday": "Everyday", "monday": "Monday", "tuesday": "Tuesday", "wednesday": "Wednesday",
				"thursday": "Thursday", "friday": "Friday", "saturday": "Saturday", "sunday": "Sunday",
			})),
		field(autoMaintain, "maintenance.rebootHour", func(c *dvrip.AutoMaintainConfig) *int { return &c.AutoRebootHour }, same[int]()),
		field(autoMaintain, "maintenance.deleteAfterDays", func(c *dvrip.AutoMaintainConfig) *int { return &c.AutoDeleteFilesDays }, same[int]()),
		field(onlineUpgrade, "maintenance.onlineUpgrade", func(c *dvrip.OnlineUpgradeConfig) *dvrip.Flag { return &c.Enable }, flag),
		field(onlineUpgrade, "maintenance.upgradeCheck", func(c *dvrip.OnlineUpgradeConfig) *dvrip.Flag { return &c.AutoCheck }, flag),
		field(onlineUpgrade, "maintenance.upgradeImportant", func(c *dvrip.OnlineUpgradeConfig) *dvrip.Flag { return &c.AutoUpgradeImp }, flag),
	},
)
