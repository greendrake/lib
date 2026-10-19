// Package rtspcam is the core's ports over RTSP (greendrake.nz/go/rtsp): cameras that
// serve their main and extra streams at paths of their own (BITVISION's at /0 and /1).
// They have streams alone, no settings.
package rtspcam

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"greendrake.nz/go/camstream"
	"greendrake.nz/go/ipcam"
	"greendrake.nz/go/rtsp"
)

// codecs are the codecs by their encoding names.
var codecs = map[string]ipcam.Codec{"H264": ipcam.H264, "H265": ipcam.H265}

// Driver logs in to RTSP cameras of one kind.
type Driver struct {
	// Where the cameras serve each stream.
	Paths map[ipcam.StreamKey]string
	// How the packets of a stream played come.
	Transport rtsp.Transport
	// How often a login is probed: a camera gone that long is offline.
	KeepAlive time.Duration
}

// BITVISION is BITVISION's cameras: the main stream at /0 and the extra at /1, sent over
// UDP alone, each login probed every 10 seconds.
var BITVISION = Driver{
	Paths:     map[ipcam.StreamKey]string{ipcam.Main: "/0", ipcam.Extra: "/1"},
	Transport: rtsp.UDP,
	KeepAlive: 10 * time.Second,
}

// Dial logs in: a connection on which every stream is described, which authenticates it
// and tells each stream's codec.
func (d Driver) Dial(ctx context.Context, address, user, password string) (ipcam.Camera, error) {
	conn, err := rtsp.Dial(ctx, address, user, password)
	if err != nil {
		return nil, failure(err)
	}
	l := &login{driver: d, address: address, user: user, password: password, conn: conn, streams: map[ipcam.StreamKey]ipcam.StreamFormat{}, done: make(chan struct{})}
	for key, path := range d.Paths {
		description, err := conn.Describe(ctx, path)
		if err == nil {
			l.streams[key], err = formatOf(description)
		}
		if err != nil {
			conn.Close()
			return nil, failure(err)
		}
	}
	go l.watch()
	return l, nil
}

// failure is why a login failed, as the core tells failures apart.
func failure(err error) error {
	var refused *rtsp.StatusError
	var unreachable *net.OpError
	switch {
	case errors.As(err, &refused) && refused.Status == 401:
		return fmt.Errorf("%w: %w", ipcam.ErrCredentials, err)
	case errors.As(err, &unreachable) || errors.Is(err, rtsp.ErrNoAnswer) || errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w: %w", ipcam.ErrUnreachable, err)
	}
	return fmt.Errorf("%w: %w", ipcam.ErrUnrecognised, err)
}

// formatOf is how a stream is encoded: its video's codec, by its common name where the
// core has one, and no audio, which the streams are not opened with.
func formatOf(d *rtsp.Description) (ipcam.StreamFormat, error) {
	media, has := d.Video()
	if !has {
		return ipcam.StreamFormat{}, errors.New("rtspcam: the stream has no video")
	}
	codec, known := codecs[media.Encoding]
	if !known {
		codec = ipcam.Codec(media.Encoding)
	}
	return ipcam.StreamFormat{Codec: codec}, nil
}

// login is an ipcam.Camera on a connection.
type login struct {
	driver         Driver
	address        string
	user, password string
	conn           *rtsp.Conn
	streams        map[ipcam.StreamKey]ipcam.StreamFormat
	done           chan struct{}
	end            sync.Once
	err            error
}

// watch probes the camera every keep-alive interval, at the main stream's path, and ends
// the login with the connection, or a probe that fails.
func (l *login) watch() {
	ticker := time.NewTicker(l.driver.KeepAlive)
	defer ticker.Stop()
	for {
		select {
		case <-l.conn.Done():
			l.finish(l.conn.Err())
			return
		case <-ticker.C:
			if err := l.conn.Options(context.Background(), l.driver.Paths[ipcam.Main]); err != nil {
				l.finish(err)
				l.conn.Close()
				return
			}
		}
	}
}

func (l *login) finish(err error) {
	l.end.Do(func() {
		l.err = err
		close(l.done)
	})
}

func (l *login) Streams() map[ipcam.StreamKey]ipcam.StreamFormat {
	return l.streams
}

// OpenStream plays the stream on a connection of its own, its video alone.
func (l *login) OpenStream(ctx context.Context, key ipcam.StreamKey) (camstream.Monitor, error) {
	return play(ctx, l.address, l.driver.Paths[key], l.user, l.password, l.driver.Transport)
}

func (l *login) Done() <-chan struct{} {
	return l.done
}

func (l *login) Err() error {
	return l.err
}

func (l *login) Close() {
	l.finish(errors.New("rtspcam: the login was closed"))
	l.conn.Close()
}
