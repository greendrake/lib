package dvrip

import (
	"encoding/json"
	"fmt"

	"greendrake.nz/go/dvrip/internal/wire"
)

// Ret is the status code every reply carries.
type Ret int

// retMeanings is the vendor's table of status codes.
var retMeanings = map[Ret]string{
	100: "OK",
	101: "unknown error",
	102: "version not supported",
	103: "illegal request",
	104: "user already logged in",
	105: "user not logged in",
	106: "wrong user name or password",
	107: "no permission",
	108: "timeout",
	109: "search found no file",
	110: "search returned all files",
	111: "search returned some files",
	112: "user already exists",
	113: "user does not exist",
	114: "group already exists",
	115: "group does not exist",
	117: "malformed message",
	118: "no PTZ protocol set",
	119: "no file found",
	120: "configured to enable",
	121: "digital channel not connected",
	150: "OK, restart required",
	202: "user not logged in",
	203: "wrong password",
	204: "illegal user",
	205: "user locked or unknown",
	206: "user blacklisted",
	207: "user already logged in",
	208: "illegal input",
	209: "duplicate index",
	210: "object not found",
	211: "object does not exist",
	212: "account in use",
	213: "subset out of range",
	214: "illegal password",
	215: "password mismatch",
	216: "reserved account",
	502: "illegal command",
	503: "talk already open",
	504: "talk not open",
	511: "upgrade started",
	512: "upgrade not started",
	513: "upgrade data error",
	514: "upgrade failed",
	515: "upgrade successful",
	521: "restoring defaults failed",
	522: "device restart required",
	523: "illegal default configuration",
	602: "application restart required",
	603: "system restart required",
	604: "file write error",
	605: "feature not supported",
	606: "verification failed",
	607: "configuration does not exist",
	608: "configuration parse error",
}

// ok reports a success. 515 reports a successful upgrade, and firmwares answer a login
// made after one with it.
func (r Ret) ok() bool {
	return r == 100 || r == 515 || r.restart()
}

// restart reports a success that takes effect only once the device restarts.
func (r Ret) restart() bool {
	return r == 150 || r == 522 || r == 602 || r == 603
}

// RetError is a reply whose status code reports a failure.
type RetError struct {
	// What was asked: the command or configuration name.
	Request string
	Ret     Ret
}

func (e *RetError) Error() string {
	meaning, known := retMeanings[e.Ret]
	if !known {
		meaning = "unknown status"
	}
	return fmt.Sprintf("dvrip: %s: %d %s", e.Request, e.Ret, meaning)
}

// IsCredentials reports that the device refused the user name or password, which no
// retry will change. Devices answer an unknown user with 205 and a wrong password
// with 203; 106 and 204 are the vendor's codes for the same.
func (e *RetError) IsCredentials() bool {
	switch e.Ret {
	case 106, 203, 204, 205:
		return true
	}
	return false
}

// IsAbsent reports that the device has no configuration of the name asked for: its
// firmware does not keep it.
func (e *RetError) IsAbsent() bool {
	return e.Ret == 607
}

// reply is a successful JSON reply.
type reply struct {
	request string
	ret     Ret
	fields  map[string]json.RawMessage
}

// decodeReply parses a JSON reply to request and checks its status code.
func decodeReply(data []byte, request string) (reply, error) {
	r := reply{request: request}
	if err := json.Unmarshal(wire.TrimJSON(data), &r.fields); err != nil {
		return reply{}, fmt.Errorf("dvrip: %s: reply: %w", request, err)
	}
	if err := json.Unmarshal(r.fields["Ret"], &r.ret); err != nil {
		return reply{}, fmt.Errorf("dvrip: %s: reply status: %w", request, err)
	}
	if !r.ret.ok() {
		return reply{}, &RetError{Request: request, Ret: r.ret}
	}
	return r, nil
}

// field returns what the reply carries under name.
func (r reply) field(name string) (json.RawMessage, error) {
	v, ok := r.fields[name]
	if !ok {
		return nil, fmt.Errorf("dvrip: %s: reply carries no %q", r.request, name)
	}
	return v, nil
}

// decodeAs decodes what a reply carried, passing an error that came instead straight
// through, so that a call returning both composes with it directly.
func decodeAs[T any](raw json.RawMessage, err error) (T, error) {
	var v T
	if err != nil {
		return v, err
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, fmt.Errorf("dvrip: decoding %T: %w", v, err)
	}
	return v, nil
}
