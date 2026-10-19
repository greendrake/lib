package wire

import (
	"bufio"
	"bytes"
	"net"
	"reflect"
	"strings"
	"testing"
)

func reader(s string) *bufio.Reader {
	return bufio.NewReader(strings.NewReader(s))
}

func TestMessages(t *testing.T) {
	req := &Message{Method: "DESCRIBE", URI: "rtsp://cam/0", Header: Header{{"CSeq", "2"}, {"Accept", "application/sdp"}}}
	if got := string(req.Bytes()); got != "DESCRIBE rtsp://cam/0 RTSP/1.0\r\nCSeq: 2\r\nAccept: application/sdp\r\n\r\n" {
		t.Fatalf("request %q", got)
	}
	res := &Message{Status: 200, Reason: "OK", Header: Header{{"CSeq", "2"}}, Body: []byte("v=0\r\n")}
	read, err := Read(reader(string(res.Bytes())))
	if err != nil {
		t.Fatal(err)
	}
	// The length goes out with the body, and comes back with it.
	if read.IsRequest() || read.Status != 200 || read.Reason != "OK" || read.Header.Get("cseq") != "2" || !bytes.Equal(read.Body, res.Body) {
		t.Fatalf("response %+v", read)
	}
	read, err = Read(reader(string(req.Bytes())))
	if err != nil || !read.IsRequest() || !reflect.DeepEqual(read, req) {
		t.Fatalf("request %+v, %v", read, err)
	}
}

func TestHeader(t *testing.T) {
	h := Header{{"WWW-Authenticate", "Digest"}, {"www-authenticate", "Basic"}, {"Session", "1"}}
	if h.Get("Www-Authenticate") != "Digest" || !reflect.DeepEqual(h.Values("WWW-AUTHENTICATE"), []string{"Digest", "Basic"}) || h.Get("Range") != "" {
		t.Fatalf("%q", h)
	}
}

func TestMalformedMessages(t *testing.T) {
	for _, message := range []string{
		"RTSP/1.0 200\r\n\r\n",
		"RTSP/1.0 OK fine\r\n\r\n",
		"GET / HTTP/1.1\r\n\r\n",
		"RTSP/1.0 200 OK\r\nno colon\r\n\r\n",
		"RTSP/1.0 200 OK\r\nContent-Length: -1\r\n\r\n",
		"RTSP/1.0 200 OK\r\nContent-Length: 9\r\n\r\nshort",
		"RTSP/1.0 200 OK\r\nCSeq: 1\r\n",
	} {
		if _, err := Read(reader(message)); err == nil {
			t.Errorf("%q read", message)
		}
	}
}

func TestInterleaved(t *testing.T) {
	b := AppendInterleaved(nil, 3, []byte{1, 2, 3})
	channel, packet, err := ReadInterleaved(bufio.NewReader(bytes.NewReader(b)))
	if err != nil || channel != 3 || !bytes.Equal(packet, []byte{1, 2, 3}) {
		t.Fatalf("%d % x, %v", channel, packet, err)
	}
	for _, b := range [][]byte{{'R', 0, 0, 0}, {'$', 0, 0, 9, 1}, {'$', 0}} {
		if _, _, err := ReadInterleaved(bufio.NewReader(bytes.NewReader(b))); err == nil {
			t.Errorf("% x read", b)
		}
	}
}

// RFC 2617 3.5's example: a qop of auth.
func TestDigestWithQop(t *testing.T) {
	if got := Digest("Mufasa", "testrealm@host.com", "Circle Of Life", "GET", "/dir/index.html", "dcd98b7102dd2f0e8b11d0f600bfb0c093", "auth", "00000001", "0a4f113b"); got != "6629fae49393a05397450978507c4ef1" {
		t.Fatalf("response %s", got)
	}
}

func TestParams(t *testing.T) {
	params := Params(`realm="a \"b\"", Nonce=token , qop="auth,auth-int", nc=00000001`)
	if !reflect.DeepEqual(params, map[string]string{"realm": `a "b"`, "nonce": "token", "qop": "auth,auth-int", "nc": "00000001"}) {
		t.Fatalf("%q", params)
	}
	if params := Params(`realm`); len(params) != 0 {
		t.Fatalf("%q", params)
	}
}

func TestPair(t *testing.T) {
	for transport, want := range map[string][2]int{
		"RTP/AVP;unicast;client_port=5000-5001;server_port=32773-32774;ssrc=A047": {32773, 32774},
		"RTP/AVP;unicast; server_port=6970":                                       {6970, 6971},
	} {
		if rtp, rtcp, err := Pair(transport, "server_port"); err != nil || [2]int{rtp, rtcp} != want {
			t.Errorf("%q: %d-%d, %v", transport, rtp, rtcp, err)
		}
	}
	for _, transport := range []string{"RTP/AVP;unicast;client_port=5000-5001", "RTP/AVP;server_port=x-1", "RTP/AVP;server_port=1-65536"} {
		if _, _, err := Pair(transport, "server_port"); err == nil {
			t.Errorf("transport %q read", transport)
		}
	}
}

func TestInterleavedChannel(t *testing.T) {
	if channel, err := InterleavedChannel("RTP/AVP/TCP;unicast;interleaved=2-3"); err != nil || channel != 2 {
		t.Fatalf("%d, %v", channel, err)
	}
	for _, transport := range []string{"RTP/AVP;unicast;client_port=5000-5001", "RTP/AVP/TCP;interleaved=x-1", "RTP/AVP/TCP;interleaved=256-257"} {
		if _, err := InterleavedChannel(transport); err == nil {
			t.Errorf("transport %q read", transport)
		}
	}
}

func TestListenPair(t *testing.T) {
	ip := net.IPv4(127, 0, 0, 1)
	rtp, rtcp, err := ListenPair(ip)
	if err != nil {
		t.Fatal(err)
	}
	defer rtp.Close()
	defer rtcp.Close()
	first, second := rtp.LocalAddr().(*net.UDPAddr), rtcp.LocalAddr().(*net.UDPAddr)
	if first.Port%2 != 0 || second.Port != first.Port+1 || !first.IP.Equal(ip) || !second.IP.Equal(ip) {
		t.Fatalf("%v and %v", first, second)
	}
	if _, _, err := ListenPair(net.IPv4(192, 0, 2, 1)); err == nil {
		t.Fatal("listened at an address the host has not")
	}
}

// Each datagram comes whole, its own, with its sender, until the taker stops, or the port
// closes.
func TestReceive(t *testing.T) {
	rtp, rtcp, err := ListenPair(net.IPv4(127, 0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	defer rtcp.Close()
	for _, b := range [][]byte{{1}, {2, 2}} {
		if _, err := rtcp.WriteToUDP(b, rtp.LocalAddr().(*net.UDPAddr)); err != nil {
			t.Fatal(err)
		}
	}
	var got [][]byte
	Receive(rtp, func(datagram []byte, from *net.UDPAddr) bool {
		if from.Port != rtcp.LocalAddr().(*net.UDPAddr).Port {
			t.Errorf("from %v", from)
		}
		got = append(got, datagram)
		return len(got) < 2
	})
	if !reflect.DeepEqual(got, [][]byte{{1}, {2, 2}}) {
		t.Fatalf("% x", got)
	}
	rtp.Close()
	Receive(rtp, func([]byte, *net.UDPAddr) bool {
		t.Fatal("a closed port read")
		return false
	})
}

func TestPackets(t *testing.T) {
	p := Packet{PayloadType: 96, Marker: true, Sequence: 65535, Timestamp: 4294967295, Payload: []byte{1, 2}}
	if got, err := ParsePacket(AppendPacket(nil, p, 7)); err != nil || !reflect.DeepEqual(got, p) {
		t.Fatalf("%+v, %v", got, err)
	}
	// Two CSRCs, an extension of a word, and two bytes of padding around the payload.
	b := []byte{0xb2, 96, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3, 1, 1, 1, 1, 2, 2, 2, 2, 0xbe, 0xde, 0, 1, 9, 9, 9, 9, 0xaa, 0, 2}
	if got, err := ParsePacket(b); err != nil || !bytes.Equal(got.Payload, []byte{0xaa}) || got.Marker {
		t.Fatalf("%+v, %v", got, err)
	}
	for _, b := range [][]byte{
		{0x80, 96},
		{0x40, 96, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3},
		{0x81, 96, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3},
		{0x90, 96, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3, 0xbe},
		{0x90, 96, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3, 0xbe, 0xde, 0, 2},
		{0xa0, 96, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3, 5},
	} {
		if _, err := ParsePacket(b); err == nil {
			t.Errorf("% x parsed", b)
		}
	}
}
