// Package transcode has a media file — video, audio or both — transcoded by ffmpeg, typically on another machine:
// one with a hardware encoder. The master has the source and wants the output; the slave runs ffmpeg with the
// encoders its machine has. The source never travels whole: the slave reads from the master the byte ranges ffmpeg
// asks for, over the one connection the master opens, and sends the output back over it.
//
// This package is what both sides share: a job (Job) and what to make of it (Output), the Transcoder doing it, the
// messages of the protocol between master and slave, and AllowFrom, which admits masters by address. The master is
// in package master beneath it, the slave in package slave.
package transcode

import (
	"context"
	"io"
)

// Transcoder does a job, writing the output to dst; it answers the output's length. A job that cannot be done is a
// *Failure; any other error is the transport's or the caller's: the source's, dst's, ctx's or the connection's.
type Transcoder interface {
	Transcode(ctx context.Context, job Job, dst io.Writer) (int64, error)
}

// Job is a source and what to make of it.
type Job struct {
	Label    string         // names the job in logs (e.g. the source's file name)
	Source   io.ReaderAt    // the source's bytes
	Size     int64          // the source's length
	Output   Output         // what to make of it
	Progress func(Progress) // called as the job advances, one call at a time; nil for none
}

// Report tells j.Progress, where there is one, the progress s says.
func (j Job) Report(s Status) {
	if j.Progress != nil {
		j.Progress(s.Progress())
	}
}

// Progress is how far a job has come.
type Progress struct {
	Queued  int     // jobs ahead of it while it waits for a slot; 0 once running
	Running bool    // it has its slot
	Seconds float64 // how far into the source the output has come
}

// Failure is a job that cannot be done: its output is invalid (Output.Validate), or the slave could not make it — a
// codec its ffmpeg does not encode, ffmpeg unable to read or encode the source, … It is the protocol's failed
// message too, which is how a slave reports one to its master.
type Failure struct {
	Message string `json:"error"`
	Detail  string `json:"detail"` // the tail of ffmpeg's stderr, where there is one
}

// Error answers the Message, and the Detail after it where there is one.
func (f *Failure) Error() string {
	if f.Detail == "" {
		return f.Message
	}
	return f.Message + ": " + f.Detail
}
