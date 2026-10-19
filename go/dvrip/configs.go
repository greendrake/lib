package dvrip

import "fmt"

// The configurations a camera's settings pages edit, and the abilities that bound
// them. Field names are the device's own keys. Each type declares the fields its
// settings need; Config.Set keeps every other field as the device has it.

// General is the "General" page's "HDD Full", "DVR No" and "Auto Logout".
var General = Config[GeneralConfig]{Name: "General.General"}

type GeneralConfig struct {
	// The device's number, 0-998.
	LocalNo   int
	OverWrite OverWrite
	// Minutes before the device's own interface logs its user out; 0 never.
	AutoLogout int
}

// OverWrite is what recording does when the disk is full.
type OverWrite string

const (
	OverWriteOldest OverWrite = "OverWrite"
	OverWriteStop   OverWrite = "StopRecord"
)

// Location is the "General" page's date and time formats, language, video standard
// and daylight saving.
var Location = Config[LocationConfig]{Name: "General.Location"}

type LocationConfig struct {
	DateFormat DateFormat
	// ".", "-" or "/".
	DateSeparator string
	TimeFormat    TimeFormat
	// One of the MultiLanguage ability's.
	Language string
	// One of the MultiVstd ability's.
	VideoFormat VideoFormat
	DSTRule     DSTRule
	DSTStart    DSTPoint
	DSTEnd      DSTPoint
}

type DateFormat string

const (
	DateYYMMDD DateFormat = "YYMMDD"
	DateMMDDYY DateFormat = "MMDDYY"
	DateDDMMYY DateFormat = "DDMMYY"
)

type TimeFormat string

const (
	Time12Hour TimeFormat = "12"
	Time24Hour TimeFormat = "24"
)

type VideoFormat string

const (
	PAL  VideoFormat = "PAL"
	NTSC VideoFormat = "NTSC"
)

type DSTRule string

const (
	DSTOn  DSTRule = "On"
	DSTOff DSTRule = "Off"
)

// DSTPoint is when daylight saving starts or ends.
type DSTPoint struct {
	Year  int
	Month int
	// The week of the month, 1-4, or -1 for the last; 0 makes Day the day of the month
	// rather than the day of the week (0 is Sunday).
	Week   int
	Day    int
	Hour   int
	Minute int
}

// AutoMaintain is the "AutoMaintain" page's scheduled reboot and deletion of old
// recordings.
var AutoMaintain = Config[AutoMaintainConfig]{Name: "General.AutoMaintain"}

type AutoMaintainConfig struct {
	// The day to reboot on, as the device names it ("Tuesday"), or "Never".
	AutoRebootDay  string
	AutoRebootHour int
	// 0 keeps recordings until the disk needs the space.
	AutoDeleteFilesDays int
}

// OnlineUpgrade is the "AutoMaintain" page's firmware upgrade from the vendor's server.
var OnlineUpgrade = Config[OnlineUpgradeConfig]{Name: "NetWork.OnlineUpgrade"}

type OnlineUpgradeConfig struct {
	Enable         Flag
	AutoCheck      Flag
	AutoUpgradeImp Flag
}

// TimeZone is the "General" page's time zone.
var TimeZone = Config[TimeZoneConfig]{Name: "System.TimeZone"}

type TimeZoneConfig struct {
	// Minutes west of UTC: -720 is UTC+12:00.
	MinutesWest int `json:"timeMin"`
}

// NetCommon is the "Network" page's addressing, ports, transfer policy and high-speed
// download.
var NetCommon = Config[NetCommonConfig]{Name: "NetWork.NetCommon"}

type NetCommonConfig struct {
	HostIP  IPv4
	Submask IPv4
	GateWay IPv4
	// Read only.
	MAC string
	// The port this protocol is served on: the page's "Media Port".
	TCPPort       int
	HttpPort      int
	TransferPlan  TransferPlan
	UseHSDownLoad bool
}

type TransferPlan string

const (
	TransferQuality   TransferPlan = "Quality"
	TransferFluency   TransferPlan = "Fluency"
	TransferAutoAdapt TransferPlan = "AutoAdapt"
)

// IPAdaptive is the "Network" page's "Adaptive IP".
var IPAdaptive = Config[IPAdaptiveConfig]{Name: "NetWork.IPAdaptive"}

type IPAdaptiveConfig struct {
	IPAdaptive bool
}

// NetDHCP is DHCP per network interface.
var NetDHCP = Config[[]DHCPInterface]{Name: "NetWork.NetDHCP"}

type DHCPInterface struct {
	Enable    bool
	Interface string
}

// NetDNS is the "Network" page's DNS servers.
var NetDNS = Config[NetDNSConfig]{Name: "NetWork.NetDNS"}

type NetDNSConfig struct {
	Address      IPv4
	SpareAddress IPv4
}

// OnvifPwdCheckout is the "Network" page's "Onvif check".
var OnvifPwdCheckout = Config[OnvifPwdCheckoutConfig]{Name: "NetWork.OnvifPwdCheckout"}

type OnvifPwdCheckoutConfig struct {
	Enable bool
}

// Correspondent is how the device presents itself to ONVIF clients, and its port: the
// "Network" page's "Onvif port".
var Correspondent = Config[CorrespondentConfig]{Name: "OEMcfg.Correspondent"}

type CorrespondentConfig struct {
	Port int
}

// NetNTP is the "NetService" page's NTP.
var NetNTP = Config[NetNTPConfig]{Name: "NetWork.NetNTP"}

// NetNTPConfig leaves out the device's TimeZone: an index the CMS writes along with
// System.TimeZone, matching no known table. The page's "Auto Select Server" is the
// CMS's own and not kept on the device.
type NetNTPConfig struct {
	Enable bool
	Server NTPServer
	// Minutes between synchronisations.
	UpdatePeriod int
}

type NTPServer struct {
	Name string
	Port int
}

// SimplifyEncode is the "Encode" page's video and audio encoding of each channel's two
// streams, an entry per channel. Devices refuse to address one channel's alone.
var SimplifyEncode = Config[[]EncodeChannel]{Name: "Simplify.Encode"}

type EncodeChannel struct {
	MainFormat  EncodeFormat
	ExtraFormat EncodeFormat
}

// Format is how the channel's stream is encoded.
func (e *EncodeChannel) Format(stream StreamType) *EncodeFormat {
	switch stream {
	case Main:
		return &e.MainFormat
	case Extra:
		return &e.ExtraFormat
	}
	panic(fmt.Sprintf("dvrip: no stream %q", stream))
}

type EncodeFormat struct {
	VideoEnable bool
	AudioEnable bool
	Video       VideoEncoding
}

type VideoEncoding struct {
	// "H.265", the one codec Monitor streams.
	Compression string
	// "D1", "1080P", "5M", "4K", …; the EncodeCapability ability has the stream's choices.
	Resolution string
	// Frames per second; negative is seconds per frame.
	FPS            int
	BitRateControl BitRateControl
	// 1-6, best last; variable bit rate only.
	Quality int
	// Kbit/s.
	BitRate int
	// Seconds between key frames: the page's "I Frame Interval".
	GOP int
}

type BitRateControl string

const (
	CBR BitRateControl = "CBR"
	VBR BitRateControl = "VBR"
)

// SmartH264 and SmartH264V2 are the "Encode" page's "SmartEncode"; the SystemFunction
// ability's EncodeFunction group says which of the two a device has.
var SmartH264 = ChannelConfig[SmartH264Config]{Name: "AVEnc.SmartH264"}

type SmartH264Config struct {
	SmartH264 bool
}

var SmartH264V2 = ChannelConfig[SmartH264V2Config]{Name: "AVEnc.SmartH264V2"}

// SmartH264V2Config has an entry per stream.
type SmartH264V2Config struct {
	Smart264V2     []SmartH264Config
	Smart264PlusV2 []SmartH264Plus
}

type SmartH264Plus struct {
	SmartH264Plus int
}

// CameraParam is the "Camera param." page's image sensor settings. Its choices' values
// are the vendor SDK's and the CMS's.
var CameraParam = ChannelConfig[CameraParamConfig]{Name: "Camera.Param"}

type CameraParamConfig struct {
	ExposureParam Exposure
	DayNightColor DayNightMode
	// "Auto Iris".
	ApertureMode Flag
	// The automatic exposure's reference level.
	ElecLevel     int
	BLCMode       BacklightMode
	AeSensitivity int
	// Automatic gain, and its limit.
	GainParam Gain
	EsShutter SlowShutter
	IRCUTMode IRCutMode
	// The day/night switching threshold.
	DncThr int
	// Noise filtering by day and by night, 0-5.
	DayNfLevel    int `json:"Day_nfLevel"`
	NightNfLevel  int `json:"Night_nfLevel"`
	PictureMirror Flag
	PictureFlip   Flag
	// "Anti flicker".
	RejectFlicker Flag
	// "IRSwap": reverses the IR cut filter.
	IrcutSwap    Flag
	WhiteBalance WhiteBalanceMode
}

// DayNightMode is the "Day/Night mode", by the CMS's names.
type DayNightMode = HexOf[dayNightMode]

type dayNightMode struct{}

const (
	// Colour by day, infrared black and white by night: the vendor SDK's automatic mode.
	DayNightStarIR DayNightMode = iota
	DayNightFullColor
	DayNightBlackWhite
	// Turns on motion and human detection.
	DayNightSmartAlert
	// "Intelligent Variable Light".
	DayNightIntelligentVariable
	DayNightIntelligentInfrared
)

// BacklightMode is the "BLC": backlight compensation.
type BacklightMode = HexOf[backlightMode]

type backlightMode struct{}

const (
	BacklightOff BacklightMode = iota
	BacklightOn
	BacklightAuto
)

// WhiteBalanceMode is the "White Balance".
type WhiteBalanceMode = HexOf[whiteBalanceMode]

type whiteBalanceMode struct{}

const (
	WhiteBalanceAuto WhiteBalanceMode = iota
	WhiteBalanceIndoor
	WhiteBalanceOutdoor
)

// SlowShutter is the "Slow shutter".
type SlowShutter = HexOf[slowShutter]

type slowShutter struct{}

const (
	SlowShutterOff    SlowShutter = 0
	SlowShutterLow    SlowShutter = 2
	SlowShutterMiddle SlowShutter = 4
	SlowShutterStrong SlowShutter = 6
)

// IRCutMode is the "IR_CUT": what switches the IR cut filter.
type IRCutMode int

const (
	// The IR lights: "IR Synchronous Switch".
	IRCutSynchronous IRCutMode = iota
	IRCutAutomatic
)

type Exposure struct {
	// 0 is automatic.
	Level int
	// In µs: the automatic exposure's range, or LeastTime the manual exposure.
	LeastTime Hex
	MostTime  Hex
}

type Gain struct {
	AutoGain Flag
	Gain     int
}

// CameraParamEx is the "Camera param." page's image style, wide dynamic range, "Gear
// Control" and "No Red Face Exposure".
var CameraParamEx = ChannelConfig[CameraParamExConfig]{Name: "Camera.ParamEx"}

type CameraParamExConfig struct {
	Style ImageStyle
	// Digital wide dynamic range, and its limit.
	BroadTrends Gain
	// The page's "Gear Control", 1-5.
	AutomaticAdjustment int
	// The page's "No Red Face Exposure". Turning it on turns on human detection.
	PreventOverExpo Flag
}

// ImageStyle is the "Image Style".
type ImageStyle string

const (
	ImageStyle1 ImageStyle = "typedefault"
	ImageStyle2 ImageStyle = "type1"
	ImageStyle3 ImageStyle = "type2"
)

// ClearFog is the "Camera param." page's defogging, and its rating.
var ClearFog = ChannelConfig[ClearFogConfig]{Name: "Camera.ClearFog"}

type ClearFogConfig struct {
	Enable Flag `json:"enable"`
	Level  int  `json:"level"`
}

// VideoWidget is the "DISPLAY" page's overlays on the picture: the channel title and the
// time title, each drawn or not, and where. The title's text is kept here, but what the
// device draws as the title is the bitmap DrawTitle sends; the time title's text is the
// device's own, in Location's formats.
var VideoWidget = ChannelConfig[VideoWidgetConfig]{Name: "AVEnc.VideoWidget"}

type VideoWidgetConfig struct {
	ChannelTitle          ChannelTitle
	ChannelTitleAttribute Overlay
	TimeTitleAttribute    Overlay
}

type ChannelTitle struct {
	Name string
}

// Overlay is one thing drawn over the picture.
type Overlay struct {
	// Whether it is drawn into the streams.
	EncodeBlend Flag
	// Its left and top, from 0 to 8192 across and down the picture. The device keeps
	// the other two as written, and draws nothing by them.
	RelativePos [4]int
}

// SystemFunction is what the device supports, as flags by group:
// functions["EncodeFunction"]["SmartH264V2"].
var SystemFunction = AbilityOf[Functions]{Name: "SystemFunction"}

type Functions map[string]map[string]bool

// EncodeCapability is what each stream can be encoded as.
var EncodeCapability = AbilityOf[EncodeCapabilities]{Name: "EncodeCapability"}

type EncodeCapabilities struct {
	EncodeInfo []StreamCapability
	// Kbit/s.
	MaxBitrate int
}

// capabilityStreams names the EncodeInfo entry that describes each stream.
var capabilityStreams = map[StreamType]string{Main: "MainStream", Extra: "ExtraStream2"}

// Stream is what stream can be encoded as.
func (c EncodeCapabilities) Stream(stream StreamType) (StreamCapability, error) {
	name := capabilityStreams[stream]
	for _, capability := range c.EncodeInfo {
		if capability.StreamType == name {
			return capability, nil
		}
	}
	return StreamCapability{}, fmt.Errorf("dvrip: the device describes no %s stream", stream)
}

type StreamCapability struct {
	// "MainStream", "ExtraStream2", …, "JPEGStream".
	StreamType string
	Enable     bool
	HaveAudio  bool
	// A bit per resolution: Resolutions names them.
	ResolutionMask Hex
}

// resolutionBits names a resolution mask's bits: the vendor SDK's sizes, in its order
// and as devices name them.
var resolutionBits = []string{"D1", "HD1", "BCIF", "CIF", "QCIF", "VGA", "QVGA", "SVCD", "QQVGA", "ND1", "650TVL", "720P", "1_3M", "UXGA", "1080P", "WUXGA", "2_5M", "3M", "5M", "1080N", "4M", "6M", "8M", "12M", "4K", "720N"}

// Resolutions are the sizes the stream can be encoded at, as VideoEncoding.Resolution
// names them.
func (c StreamCapability) Resolutions() ([]string, error) {
	set := []string{}
	for bit := range 32 {
		if c.ResolutionMask&(1<<bit) == 0 {
			continue
		}
		if bit >= len(resolutionBits) {
			return nil, fmt.Errorf("dvrip: no name for resolution mask bit %d", bit)
		}
		set = append(set, resolutionBits[bit])
	}
	return set, nil
}

// MultiLanguage is the languages the device's interface comes in.
var MultiLanguage = AbilityOf[[]string]{Name: "MultiLanguage"}

// MultiVstd is the video standards the device supports, as "PAL|NTSC".
var MultiVstd = AbilityOf[string]{Name: "MultiVstd"}

// SystemInfo is what the device reports about itself.
type SystemInfo struct {
	DeviceModel     string
	HardWare        string
	HardWareVersion string
	SoftWareVersion string
	BuildTime       string
	SerialNo        string
	VideoInChannel  int
	AudioInChannel  int
	TalkInChannel   int
	TalkOutChannel  int
	// Minutes the device has run, over all its starts.
	DeviceRunTime Hex
}
