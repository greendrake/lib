package wire

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestPacketRoundTrip(t *testing.T) {
	h := Header{Session: 0x57, Sequence: 9, Total: 2, Current: 1, Code: ConfigGet}
	data := AppendPacket(nil, h, []byte("body"))
	if len(data) != HeaderSize+4 {
		t.Fatalf("packet is %d bytes, want %d", len(data), HeaderSize+4)
	}
	if data[0] != 0xFF || data[1] != 0 {
		t.Fatalf("head flag and version % x", data[:2])
	}
	if session := binary.LittleEndian.Uint32(data[4:]); session != 0x57 {
		t.Fatalf("session %#x", session)
	}
	p, err := ReadPacket(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if p.Header != h || string(p.Data) != "body" {
		t.Fatalf("read back %+v", p)
	}
}

func TestReadPacketRefusesWhatIsNoPacket(t *testing.T) {
	keepAlive := Header{Session: 1, Code: KeepAlive}
	tests := map[string]struct {
		data []byte
		want string
	}{
		"wrong head flag": {
			data: func() []byte {
				b := AppendPacket(nil, keepAlive, nil)
				b[0] = 0xFE
				return b
			}(),
			want: "head flag",
		},
		"implausible length": {
			data: func() []byte {
				b := AppendPacket(nil, keepAlive, nil)
				binary.LittleEndian.PutUint32(b[16:], MaxPacketData+1)
				return b
			}(),
			want: "announces",
		},
		"cut short": {
			data: AppendPacket(nil, keepAlive, []byte("body"))[:HeaderSize+2],
			want: "unexpected EOF",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ReadPacket(bytes.NewReader(test.data))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want an error about %q", err, test.want)
			}
		})
	}
}

// multiPacket splits data into packets numbered as a multi-packet message is.
func multiPacket(c Code, parts ...string) []byte {
	var out []byte
	for i, part := range parts {
		out = AppendPacket(out, Header{Session: 1, Sequence: uint32(i), Total: uint8(len(parts)), Current: uint8(i), Code: c}, []byte(part))
	}
	return out
}

func TestReadMessageJoinsItsPackets(t *testing.T) {
	p, err := ReadMessage(bytes.NewReader(multiPacket(ConfigGet+1, "one ", "two ", "three")))
	if err != nil {
		t.Fatal(err)
	}
	if string(p.Data) != "one two three" {
		t.Fatalf("joined %q", p.Data)
	}
}

func TestReadMessageRefusesBrokenSequences(t *testing.T) {
	tests := map[string][]byte{
		"starting part way": multiPacket(ConfigGet+1, "one", "two")[HeaderSize+3:],
		"interrupted":       append(multiPacket(ConfigGet+1, "one", "two")[:HeaderSize+3], multiPacket(Ability+1, "x", "y")...),
		"cut short":         multiPacket(ConfigGet+1, "one", "two")[:HeaderSize+3],
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ReadMessage(bytes.NewReader(data)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestJSONBodies(t *testing.T) {
	body, err := EncodeJSON(map[string]int{"a": 1})
	if err != nil || string(body) != "{\"a\":1}\n\x00" {
		t.Fatalf("encoded %q, %v", body, err)
	}
	if _, err := EncodeJSON(make(chan int)); err == nil {
		t.Fatal("a value with no JSON form should not encode")
	}
	for _, body := range []string{"{}\n\x00", "{}\n", "{}"} {
		if got := string(TrimJSON([]byte(body))); got != "{}" {
			t.Fatalf("%q trimmed to %q", body, got)
		}
	}
}

func TestSofiaHash(t *testing.T) {
	// The vendor's documents give these two.
	for password, want := range map[string]string{"": "tlJwpbo6", "admin": "6QNMIQGe"} {
		if got := SofiaHash(password); got != want {
			t.Fatalf("SofiaHash(%q) = %q, want %q", password, got, want)
		}
	}
}

func TestKeyframeRate(t *testing.T) {
	if rate := KeyframeRate([]byte{0, 0, 1, FrameVideoI, 0x03, 0xE0 | 15}); rate != 15 {
		t.Fatalf("rate %d", rate)
	}
}
