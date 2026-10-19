package dvrip

import (
	"context"
	"net"

	"greendrake.nz/go/dvrip/internal/wire"
)

// mediaConn is a connection claimed for one of a session's media operations. It never
// logs in: the claim it opens with names the session, and the device starts and stops
// what it carries on the session's control connection.
type mediaConn struct {
	session *Session
	conn    net.Conn
}

// openMedia opens a media connection and claims it with the named command, waiting for
// the claim to be granted before the operation can be started.
func (s *Session) openMedia(ctx context.Context, claim wire.Code, name string, payload any) (*mediaConn, error) {
	var m *mediaConn
	err := connect(ctx, s.address, func(conn net.Conn) error {
		// Tracked from the start, so that the session ending aborts the claim.
		s.mediaMutex.Lock()
		ended := s.media == nil
		if !ended {
			s.media[conn] = struct{}{}
		}
		s.mediaMutex.Unlock()
		if ended {
			return s.err
		}
		m = &mediaConn{session: s, conn: conn}
		return m.claim(claim, name, payload)
	})
	if err != nil {
		if m != nil {
			m.close()
		}
		return nil, err
	}
	return m, nil
}

func (m *mediaConn) claim(c wire.Code, name string, payload any) error {
	body, err := m.session.commandBody(name, payload)
	if err != nil {
		return err
	}
	if _, err := m.conn.Write(wire.AppendPacket(nil, wire.Header{Session: m.session.id, Code: c}, body)); err != nil {
		return err
	}
	p, err := m.read()
	if err != nil {
		return err
	}
	_, err = decodeReply(p.Data, name)
	return err
}

// read reads the next packet, failing once the connection has been silent for the
// session's stall limit.
func (m *mediaConn) read() (wire.Packet, error) {
	p, err := readWithin(m.conn, m.session.stall, "media", wire.ReadPacket)
	if err != nil {
		return wire.Packet{}, m.session.failure(err)
	}
	return p, nil
}

func (m *mediaConn) close() {
	m.conn.Close()
	m.session.mediaMutex.Lock()
	defer m.session.mediaMutex.Unlock()
	if m.session.media != nil {
		delete(m.session.media, m.conn)
	}
}
