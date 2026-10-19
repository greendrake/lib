package ipcam

import (
	"fmt"
	"slices"
)

// page is a group of settings, in the order they show.
type page struct {
	group Group
	// Shown whatever settings the camera has of it: it holds more than its settings.
	always   bool
	settings []Spec
}

// IntegerIn is a whole number from least to most.
func IntegerIn(least, most int) Kind {
	return Kind{Type: Integer, Min: &least, Max: &most}
}

func atLeast(least int) Kind {
	return Kind{Type: Integer, Min: &least}
}

// ChoiceOf is a choice of options by [value, label].
func ChoiceOf(options ...[2]string) Kind {
	kind := Kind{Type: Choice}
	for _, o := range options {
		kind.Options = append(kind.Options, Option{Value: o[0], Label: o[1]})
	}
	return kind
}

// textOf is a text of least to most characters, none of them excluded.
func textOf(least, most int, excluded string) Kind {
	return Kind{Type: Text, MinLength: &least, MaxLength: &most, Excluded: excluded}
}

var (
	whole    = Kind{Type: Integer}
	port     = IntegerIn(1, 65535)
	toggle   = Kind{Type: Switch}
	text     = Kind{Type: Text}
	address  = Kind{Type: IPv4}
	readOnly = Kind{Type: ReadOnly}
	share    = Kind{Type: Fraction}
	// A choice whose options the camera decides.
	offered = Kind{Type: Choice}
)

// placed is the settings of an overlay's Placement, label naming the overlay and what
// saying what it draws.
func placed(overlay, label, what string) []Spec {
	left, top := Placement(overlay)
	return []Spec{
		{left, label + " across (%)", "How far across the picture " + what + " starts: 0 at its left edge, 100 at its right.", share},
		{top, label + " down (%)", "How far down the picture " + what + " starts: 0 at its top, 100 at its bottom.", share},
	}
}

// zones are the time zones by the quarter hour, from UTC-12:00 to UTC+14:00.
func zones() Kind {
	kind := Kind{Type: Choice}
	for east := -12 * 60; east <= 14*60; east += 15 {
		kind.Options = append(kind.Options, Option{Value: ZoneOption(east), Label: "UTC" + ZoneOption(east)})
	}
	return kind
}

// dstPoint is when daylight saving starts or ends.
func dstPoint(point DSTPoint, label string) []Spec {
	key := func(name string) Key { return DSTSetting(point, name) }
	return []Spec{
		{key("month"), label + " month", "", IntegerIn(1, 12)},
		{key("week"), label + " week", "The week of the month, 1 to 4, or -1 for its last; 0 makes the day below the day of the month instead.", IntegerIn(-1, 4)},
		{key("day"), label + " day", "The day of the week, 0 being Sunday; with week 0, the day of the month.", IntegerIn(0, 31)},
		{key("hour"), label + " hour", "", IntegerIn(0, 23)},
		{key("minute"), label + " minute", "", IntegerIn(0, 59)},
	}
}

// encoding is a stream's encoding.
func encoding(stream StreamKey, name string) []Spec {
	key := func(setting string) Key { return StreamSetting(stream, setting) }
	return []Spec{
		{key("video"), name + " video", "", toggle},
		{key("audio"), name + " audio", "Whether the stream carries the camera’s sound.", toggle},
		{key("codec"), name + " compression", fmt.Sprintf("The codec. A stream is cast and recorded in %s only; a codec change takes effect once the camera restarts.", Cast), Kind{Type: Pinned, Only: string(Cast)}},
		{key("resolution"), name + " resolution", "The picture size, among those the camera can encode this stream at.", offered},
		{key("frameRate"), name + " frame rate", "Frames per second; a negative number is seconds per frame.", whole},
		{key("bitRateControl"), name + " bit rate type", "CBR keeps the bit rate constant whatever the picture; VBR lets it vary with the picture, at the quality set.", ChoiceOf([2]string{"cbr", "CBR"}, [2]string{"vbr", "VBR"})},
		{key("quality"), name + " quality", "The picture quality variable bit rate keeps to, 1 to 6, best last. Constant bit rate ignores it.", IntegerIn(1, 6)},
		{key("bitRate"), name + " bit rate (kbit/s)", "", atLeast(1)},
		{key("keyFrameInterval"), name + " I frame interval (s)", "Seconds between key frames. Shorter starts a viewer sooner and recovers from a lost packet sooner; longer takes less bandwidth.", atLeast(1)},
	}
}

// core is the catalogue: the settings any IP camera has, by page, which an adapter's
// Extension adds those of its protocol's cameras to.
var core = []page{
	{Group{"general", "General"}, false, slices.Concat(
		[]Spec{
			{"general.timeZone", "Time zone", "The camera’s offset from UTC, which its clock and the time it shows follow.", zones()},
			{"general.dateFormat", "Date format", "", ChoiceOf([2]string{"ymd", "YYMMDD"}, [2]string{"mdy", "MMDDYY"}, [2]string{"dmy", "DDMMYY"})},
			{"general.dateSeparator", "Date separator", "", ChoiceOf([2]string{".", "."}, [2]string{"-", "-"}, [2]string{"/", "/"})},
			{"general.timeFormat", "Time format", "", ChoiceOf([2]string{"24h", "24-hour"}, [2]string{"12h", "12-hour"})},
			{"general.language", "Language", "The language of the camera’s own interface.", offered},
			{"general.videoStandard", "Video standard", "PAL (50 Hz) or NTSC (60 Hz): the family of frame rates the camera works in.", offered},
			{"general.dst", "DST", "Daylight saving time: whether the camera’s clock moves for it, between the start and end below.", toggle},
		},
		dstPoint(DSTStart, "DST start"),
		dstPoint(DSTEnd, "DST end"),
		[]Spec{
			{"general.autoLogout", "Auto logout (min)", "Minutes before the camera’s own interface logs its user out; 0 never.", IntegerIn(0, 120)},
		},
	)},
	{Group{"encode", "Encode"}, false, slices.Concat(
		encoding(Main, "Main stream"),
		encoding(Extra, "Extra stream"),
	)},
	{Group{"network", "Network"}, false, []Spec{
		{"network.dhcp", "DHCP", "Take the address from the network’s DHCP server rather than the one set here.", toggle},
		{"network.address", "IP address", "Changing it moves the camera there, and its connection follows it.", address},
		{"network.mask", "Subnet mask", "", address},
		{"network.gateway", "Gateway", "Where the camera sends what is for other networks.", address},
		{"network.port", "Media port", "The port the camera serves its streams and settings on.", port},
		{"network.httpPort", "HTTP port", "The port of the camera’s own web interface.", port},
		{"network.mac", "MAC", "The camera’s hardware address.", readOnly},
		{"network.transferPolicy", "Transfer policy", "What the camera favours when the network cannot carry everything: picture quality, smooth motion, or adapting between the two.", ChoiceOf([2]string{"quality", "Quality"}, [2]string{"fluency", "Fluency"}, [2]string{"autoAdapt", "Auto adapt"})},
		{"network.highSpeedDownload", "High speed download", "Faster downloading of recordings over the network.", toggle},
		{"network.dns", "Primary DNS", "The name server the camera asks first, for an NTP server given by name, say.", address},
		{"network.spareDNS", "Secondary DNS", "The name server it asks when the primary does not answer.", address},
		{"network.adaptiveIP", "Adaptive IP", "", toggle},
		{"network.onvifPort", "Onvif port", "The port ONVIF clients, such as other vendors’ recorders, reach the camera on.", port},
		{"network.onvifCheck", "Onvif check", "Whether ONVIF clients must log in with the camera’s user and password.", toggle},
	}},
	{Group{"ntp", "NTP"}, false, []Spec{
		{"ntp.enable", "NTP", "Set the camera’s clock from a time server over the network.", toggle},
		{"ntp.server", "Server", "The time server, by name or address.", text},
		{"ntp.port", "Port", "The time server’s port: 123 for NTP.", port},
		{"ntp.updatePeriod", "Update period (min)", "Minutes between settings of the clock from the server.", atLeast(1)},
	}},
	{Group{"camera", "Camera"}, false, []Spec{
		{"camera.dayNight", "Day/Night mode", "Star IR: colour by day, infrared black and white by night. Full Color and Black and White keep to one. Smart Alert also turns on motion and human detection.",
			ChoiceOf([2]string{"auto", "Star IR"}, [2]string{"color", "Full Color"}, [2]string{"monochrome", "Black and White"}, [2]string{"smartAlert", "Smart Alert"}, [2]string{"smartLight", "Intelligent Variable Light"}, [2]string{"smartInfrared", "Intelligent Infrared"})},
		{"camera.exposure", "Exposure (0 auto)", "The exposure: 0 sets it automatically within the range below, any other a fixed one.", atLeast(0)},
		{"camera.exposureMin", "Exposure min (µs)", "The shortest exposure automatic exposure uses, in microseconds.", atLeast(0)},
		{"camera.exposureMax", "Exposure max (µs)", "The longest exposure automatic exposure uses, in microseconds.", atLeast(0)},
		{"camera.autoIris", "Auto Iris", "Lets the camera drive the aperture of a lens with a motorised iris.", toggle},
		{"camera.aeReference", "AE Reference", "The brightness automatic exposure aims for.", atLeast(0)},
		{"camera.backlight", "BLC", "Backlight compensation: brightens a subject in front of a bright background.", ChoiceOf([2]string{"off", "Off"}, [2]string{"on", "On"}, [2]string{"auto", "Auto"})},
		{"camera.aeSensitivity", "AE Sensitivity", "How strongly automatic exposure responds to a change of light.", atLeast(0)},
		{"camera.autoGain", "AGC", "Automatic gain control: amplifies the picture in low light, at the cost of noise.", toggle},
		{"camera.gainLimit", "AGC limit", "The most gain automatic gain control may apply.", atLeast(0)},
		{"camera.slowShutter", "Slow shutter", "Longer exposures in low light: a brighter picture, in which moving things blur.", ChoiceOf([2]string{"off", "Off"}, [2]string{"low", "Low"}, [2]string{"middle", "Middle"}, [2]string{"strong", "Strong"})},
		{"camera.irCut", "IR_CUT", "What switches the infrared-cut filter between day and night: the infrared lights going on and off, or the camera on its own.", ChoiceOf([2]string{"lights", "IR Synchronous Switch"}, [2]string{"automatic", "Automatic"})},
		{"camera.dayNightLevel", "Day/Night NT level", "The light level at which the camera switches between day and night.", atLeast(0)},
		{"camera.dayNoiseFilter", "Day noise filter", "Noise reduction by day, 0 to 5: more smooths the picture, and blurs detail with the noise.", IntegerIn(0, 5)},
		{"camera.nightNoiseFilter", "Night noise filter", "Noise reduction by night, 0 to 5.", IntegerIn(0, 5)},
		{"camera.mirror", "Mirror", "Flips the picture left to right.", toggle},
		{"camera.flip", "Flip", "Flips the picture upside down, for a camera mounted that way.", toggle},
		{"camera.antiFlicker", "Anti flicker", "Keeps the picture from flickering under lights the mains drives.", toggle},
		{"camera.whiteBalance", "White Balance", "How colours are corrected for the light: automatically, or for indoor or outdoor light.", ChoiceOf([2]string{"auto", "Auto"}, [2]string{"indoor", "Indoor"}, [2]string{"outdoor", "Outdoor"})},
		{"camera.wdr", "DWDR", "Digital wide dynamic range: brings out both the shadows and the highlights of a high-contrast scene.", toggle},
		{"camera.wdrLimit", "DWDR limit", "How strongly digital wide dynamic range applies.", atLeast(0)},
		{"camera.noRedFace", "No Red Face Exposure", "Keeps a subject near the camera from being overexposed. Turning it on also turns on human detection.", toggle},
		{"camera.defog", "Defogging", "Clears up a picture that fog or haze has washed out.", toggle},
		{"camera.defogLevel", "Defogging rating", "How strongly defogging applies.", atLeast(0)},
	}},
	{Group{"overlay", "Overlay"}, false, slices.Concat(
		[]Spec{
			{"overlay.name", "Camera name", "The name written over the picture.", textOf(1, 42, `\"`)},
			{"overlay.showName", "Show the name", "", toggle},
		},
		placed("name", "Name", "the name"),
		[]Spec{
			{"overlay.showTime", "Show the date and time", "The camera’s date and time over the picture, in the date and time formats General sets.", toggle},
		},
		placed("time", "Date and time", "the date and time"),
	)},
	// It also holds the camera's clock and its reboot, which every camera has.
	{Group{"maintenance", "Maintenance"}, true, []Spec{
		{"maintenance.rebootDay", "Auto-Reboot", "When the camera reboots itself, at the hour below.",
			ChoiceOf([2]string{"never", "Never"}, [2]string{"everyday", "Everyday"}, [2]string{"monday", "Monday"}, [2]string{"tuesday", "Tuesday"}, [2]string{"wednesday", "Wednesday"},
				[2]string{"thursday", "Thursday"}, [2]string{"friday", "Friday"}, [2]string{"saturday", "Saturday"}, [2]string{"sunday", "Sunday"})},
		{"maintenance.rebootHour", "Auto-Reboot hour", "", IntegerIn(0, 23)},
		{"maintenance.deleteAfterDays", "Auto-Delete Old Files (days)", "Days the camera keeps its recordings; 0 keeps them until it needs the space.", IntegerIn(0, 365)},
	}},
}
