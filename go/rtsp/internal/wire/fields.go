package wire

import (
	"fmt"
	"strconv"
	"strings"
)

// Params reads an authentication field's comma separated key=value pairs, past its
// scheme: a value a token, or a quoted string, in which a comma is the string's.
func Params(s string) map[string]string {
	params := map[string]string{}
	for s = strings.TrimSpace(s); s != ""; s = strings.TrimLeft(s, ", \t") {
		key, rest, found := strings.Cut(s, "=")
		if !found {
			break
		}
		key = strings.ToLower(strings.TrimSpace(key))
		var value strings.Builder
		if strings.HasPrefix(rest, `"`) {
			i := 1
			for ; i < len(rest) && rest[i] != '"'; i++ {
				if rest[i] == '\\' && i+1 < len(rest) {
					i++
				}
				value.WriteByte(rest[i])
			}
			s = rest[min(i+1, len(rest)):]
		} else {
			token, after, _ := strings.Cut(rest, ",")
			value.WriteString(strings.TrimSpace(token))
			s = after
		}
		params[key] = value.String()
	}
	return params
}

// Pair reads the pair a Transport field's parameter name gives, "<name>=<rtp>[-<rtcp>]":
// RTP's and RTCP's ports, or interleaved channels. An RTCP one left out is the one after
// RTP's (RFC 3550 11).
func Pair(transport, name string) (int, int, error) {
	for _, param := range strings.Split(transport, ";") {
		if value, found := strings.CutPrefix(strings.TrimSpace(param), name+"="); found {
			first, second, paired := strings.Cut(value, "-")
			rtp, err := strconv.ParseUint(first, 10, 16)
			if err != nil {
				break
			}
			if !paired {
				return int(rtp), int(rtp) + 1, nil
			}
			rtcp, err := strconv.ParseUint(second, 10, 16)
			if err != nil {
				break
			}
			return int(rtp), int(rtcp), nil
		}
	}
	return 0, 0, fmt.Errorf("rtsp: no %s in %q", name, transport)
}

// InterleavedChannel reads the channel RTP is interleaved on from a Transport field's
// "interleaved=<rtp>-<rtcp>".
func InterleavedChannel(transport string) (byte, error) {
	channel, _, err := Pair(transport, "interleaved")
	if err == nil && channel > 0xff {
		err = fmt.Errorf("rtsp: interleaved channel %d", channel)
	}
	return byte(channel), err
}

// NALType is the type of an H.265 NAL unit, or of an RTP payload of one, by its header.
func NALType(nalu []byte) byte {
	return (nalu[0] >> 1) & 0x3f
}
