package dvrip

import (
	"context"
	"fmt"
	"io"

	"greendrake.nz/go/dvrip/internal/wire"
)

// StreamType names one of a channel's encoded streams.
type StreamType string

const (
	// The full-resolution stream.
	Main StreamType = "Main"
	// The secondary, low-resolution stream. Some firmwares know it as "Extra1".
	Extra StreamType = "Extra"
)

const opMonitor = "OPMonitor"

type monitorAction struct {
	Action    string           `json:"Action"`
	Parameter monitorParameter `json:"Parameter"`
}

type monitorParameter struct {
	Channel    int        `json:"Channel"`
	CombinMode string     `json:"CombinMode"`
	StreamType StreamType `json:"StreamType"`
	TransMode  string     `json:"TransMode"`
}

// Monitor is a live stream of one channel, on a media connection of its own.
type Monitor struct {
	session   *Session
	media     *mediaConn
	parameter monitorParameter
	stream    frameStream
}

// Monitor starts streaming a channel's stream: claimed on a new media connection, then
// started on the control connection, as the vendor's protocol orders it.
func (s *Session) Monitor(ctx context.Context, channel int, stream StreamType) (*Monitor, error) {
	p := monitorParameter{Channel: channel, CombinMode: "NONE", StreamType: stream, TransMode: "TCP"}
	media, err := s.openMedia(ctx, wire.MonitorClaim, opMonitor, monitorAction{Action: "Claim", Parameter: p})
	if err != nil {
		return nil, err
	}
	if _, err := s.command(ctx, wire.Monitor, opMonitor, monitorAction{Action: "Start", Parameter: p}); err != nil {
		media.close()
		return nil, err
	}
	m := &Monitor{session: s, media: media, parameter: p}
	m.stream.read = func() ([]byte, error) {
		message, err := media.read()
		if err != nil {
			return nil, err
		}
		if message.Code != wire.MonitorData {
			return nil, fmt.Errorf("dvrip: message %d on a monitor's connection", message.Code)
		}
		return message.Data, nil
	}
	return m, nil
}

// Record copies every frame to w as the device sent it, whole frames only, before
// anything is made of it: a recording NewReplay plays back.
func (m *Monitor) Record(w io.Writer) {
	m.stream.record = w
}

// Next returns the stream's next frame, waiting for it to arrive.
func (m *Monitor) Next() (Frame, error) {
	return m.stream.next()
}

// Close stops the stream. The device acknowledges on the control connection and then
// closes the media connection, which is closed here regardless.
func (m *Monitor) Close(ctx context.Context) error {
	defer m.media.close()
	_, err := m.session.command(ctx, wire.Monitor, opMonitor, monitorAction{Action: "Stop", Parameter: m.parameter})
	return err
}
