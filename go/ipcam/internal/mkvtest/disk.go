package mkvtest

import (
	"errors"
	"io"
	"os"
)

// ErrFull is what a full Disk answers a write past its file's end with.
var ErrFull = errors.New("mkvtest: the disk is full")

// Disk is a file in memory, on a disk that can fill up: once Full, a write past the file's
// end fails, while one over what it holds goes in.
type Disk struct {
	Full bool
	// Set once the file is closed, after which it takes no writes.
	Closed bool
	b      []byte
	at     int
}

// Bytes is what the file holds.
func (d *Disk) Bytes() []byte {
	return d.b
}

func (d *Disk) Write(p []byte) (int, error) {
	if d.Closed {
		return 0, os.ErrClosed
	}
	if end := d.at + len(p); end > len(d.b) {
		if d.Full {
			return 0, ErrFull
		}
		d.b = append(d.b, make([]byte, end-len(d.b))...)
	}
	d.at += copy(d.b[d.at:], p)
	return len(p), nil
}

// Seek seeks from the file's start alone, which is what the writer does.
func (d *Disk) Seek(offset int64, whence int) (int64, error) {
	if whence != io.SeekStart {
		return 0, errors.New("mkvtest: a seek from other than the start")
	}
	d.at = int(offset)
	return offset, nil
}

func (d *Disk) Close() error {
	d.Closed = true
	return nil
}
