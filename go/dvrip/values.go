package dvrip

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/netip"

	"greendrake.nz/go/dvrip/internal/wire"
)

// HexOf is a number the device writes as a hex string, "0x00000100". C holds nothing:
// it tells the device's enumerations apart, each a HexOf of its own C with its own
// constants. It is written in eight digits; Config.Set writes it in as many as the
// device has.
type HexOf[C any] uint32

// Hex is a plain number written as hex.
type Hex = HexOf[struct{}]

func (h HexOf[C]) MarshalJSON() ([]byte, error) {
	return json.Marshal(wire.FormatHex(uint32(h), 8))
}

func (h *HexOf[C]) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	v, _, ok := wire.ParseHex(s)
	if !ok {
		return fmt.Errorf("dvrip: hex value %q", s)
	}
	*h = HexOf[C](v)
	return nil
}

// Flag is a switch, which configurations write as true or false, 1 or 0, or as a hex
// 1 or 0. It reads any; Config.Set writes it back in the form the device has.
type Flag bool

func (f *Flag) UnmarshalJSON(data []byte) error {
	v, err := wire.DecodeExact(data)
	if err != nil {
		return err
	}
	_, on, known := wire.FlagForm(v)
	if !known {
		return fmt.Errorf("dvrip: flag %s", data)
	}
	*f = Flag(on)
	return nil
}

// IPv4 is an address the device writes as a Hex whose little-endian bytes are the
// address's: "0x690200C0" is 192.0.2.105.
type IPv4 struct {
	netip.Addr
}

func (a IPv4) MarshalJSON() ([]byte, error) {
	if !a.Is4() {
		return nil, fmt.Errorf("dvrip: %v is not an IPv4 address", a.Addr)
	}
	b := a.As4()
	return Hex(binary.LittleEndian.Uint32(b[:])).MarshalJSON()
}

func (a *IPv4) UnmarshalJSON(data []byte) error {
	var h Hex
	if err := h.UnmarshalJSON(data); err != nil {
		return err
	}
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(h))
	a.Addr = netip.AddrFrom4(b)
	return nil
}
