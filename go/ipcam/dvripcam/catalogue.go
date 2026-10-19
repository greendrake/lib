package dvripcam

import "greendrake.nz/go/ipcam"

// smartEncode is the tip of a stream's SmartEncode.
const smartEncode = "The vendor’s smart encoding, which saves bit rate where the scene does not change."

var onOff = ipcam.Kind{Type: ipcam.Switch}

// xmeye is the settings XMEye cameras have beyond the core's catalogue: the vendor's, in
// the core's groups.
var xmeye = ipcam.Extension{Settings: []ipcam.Spec{
	{Key: "general.whenFull", Label: "HDD full", Tip: "What recording does when the camera’s storage is full: overwrite the oldest recordings, or stop.",
		Kind: ipcam.ChoiceOf([2]string{"overwrite", "Overwrite"}, [2]string{"stop", "Stop record"})},
	{Key: "general.deviceNumber", Label: "DVR No.", Tip: "The device’s number, 0 to 998, which tells it apart from others.", Kind: ipcam.IntegerIn(0, 998)},
	{Key: ipcam.StreamSetting(ipcam.Main, "smartEncode"), Label: "Main stream SmartEncode", Tip: smartEncode, Kind: onOff},
	{Key: ipcam.StreamSetting(ipcam.Extra, "smartEncode"), Label: "Extra stream SmartEncode", Tip: smartEncode, Kind: onOff},
	{Key: "camera.irSwap", Label: "IRSwap", Tip: "Reverses the infrared-cut filter, for a camera that switches it the wrong way round.", Kind: onOff},
	{Key: "camera.style", Label: "Image Style", Tip: "The vendor’s presets of colour and contrast.",
		Kind: ipcam.ChoiceOf([2]string{"style1", "Style 1"}, [2]string{"style2", "Style 2"}, [2]string{"style3", "Style 3"})},
	{Key: "camera.gearControl", Label: "Gear Control", Kind: ipcam.IntegerIn(1, 5)},
	{Key: "maintenance.onlineUpgrade", Label: "Online Upgrade", Tip: "Firmware upgrades from the vendor’s server.", Kind: onOff},
	{Key: "maintenance.upgradeCheck", Label: "Check automatically", Tip: "Whether the camera looks for upgrades by itself.", Kind: onOff},
	{Key: "maintenance.upgradeImportant", Label: "Install important upgrades", Tip: "Whether it installs those the vendor marks important by itself.", Kind: onOff},
}}
