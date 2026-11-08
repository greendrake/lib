// Command rtsp talks to a camera over RTSP: describes its streams, and plays their video
// for inspection.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"time"

	"greendrake.nz/go/rtsp"
)

const usage = `usage: rtsp -addr host[:port] [-user ''] [-pass ''] command

commands:
  describe <path>                         the stream's description: each media's type,
                                          control, payload type, encoding, clock rate,
                                          parameters and frame rate
  play [-tcp] [-for duration] <path>...   statistics each second of each stream's H.265
                                          video, every stream played at once on the one
                                          connection, over UDP, or interleaved on it with
                                          -tcp, until interrupted or for the duration
`

func main() {
	address := flag.String("addr", "", "the camera, host or host:port")
	user := flag.String("user", "", "user name")
	password := flag.String("pass", "", "password")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()
	args := flag.Args()
	if *address == "" || len(args) == 0 {
		flag.Usage()
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c, err := rtsp.Dial(dialCtx, *address, *user, *password)
	if err != nil {
		fail(err)
	}
	defer c.Close()
	if err := run(ctx, c, args); err != nil {
		fail(err)
	}
}

func run(ctx context.Context, c *rtsp.Conn, args []string) error {
	switch {
	case len(args) == 2 && args[0] == "describe":
		d, err := c.Describe(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Println(d.Control)
		for _, m := range d.Medias {
			fmt.Printf("%s %s %d %s/%d %v, %g frames a second\n", m.Type, m.Control, m.PayloadType, m.Encoding, m.ClockRate, m.Params, m.FrameRate)
		}
		return nil
	case args[0] == "play":
		return play(ctx, c, args[1:])
	}
	return fmt.Errorf("unknown command: %v", args)
}

// play plays the H.265 video of every stream args name, each in a session of its own on
// c, printing each second what each delivered in it.
func play(ctx context.Context, c *rtsp.Conn, args []string) error {
	flags := flag.NewFlagSet("play", flag.ContinueOnError)
	tcp := flags.Bool("tcp", false, "interleaved on the connection, not over UDP")
	duration := flags.Duration("for", 0, "how long to play; until interrupted if 0")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() == 0 {
		return fmt.Errorf("play takes a path at least: %v", args)
	}
	transport := rtsp.UDP
	if *tcp {
		transport = rtsp.TCP
	}
	players := make([]*player, flags.NArg())
	for i, path := range flags.Args() {
		d, err := c.Describe(ctx, path)
		if err != nil {
			return err
		}
		media, has := d.Video()
		if !has || media.Encoding != "H265" {
			return fmt.Errorf("%s has no H.265 video: %+v", path, d.Medias)
		}
		session, err := c.Play(ctx, d, media, transport)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		players[i] = &player{path: path, session: session, clock: media.Clock()}
	}
	if *duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *duration)
		defer cancel()
	}
	// Each player's reading ends with its session, which closes with the connection.
	failed := make(chan error, len(players))
	for _, p := range players {
		go func() { failed <- p.read() }()
	}
	started := time.Now()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-failed:
			return err
		case <-ticker.C:
			for _, p := range players {
				fmt.Printf("%7v %s %v\n", time.Since(started).Truncate(time.Millisecond), p.path, p.second())
			}
		}
	}
}

// player is a stream played, and what it delivered in the current second.
type player struct {
	path    string
	session *rtsp.Session
	clock   rtsp.Clock
	units   rtsp.H265
	mu      sync.Mutex
	stats   statistics
}

// read counts the session's packets and units until it ends, answering why, prefixed by
// the stream's path.
func (p *player) read() error {
	for packet := range p.session.Packets() {
		p.mu.Lock()
		p.stats.count(packet)
		var err error
		if nalus, timestamp := p.units.Push(packet); nalus != nil {
			err = p.stats.add(nalus, timestamp, &p.clock)
		}
		p.mu.Unlock()
		if err != nil {
			return fmt.Errorf("%s: %w", p.path, err)
		}
	}
	return fmt.Errorf("%s: %w", p.path, p.session.Err())
}

// second is what the stream delivered in the second now ended, from which the next
// starts counting afresh.
func (p *player) second() *statistics {
	p.mu.Lock()
	defer p.mu.Unlock()
	ended := p.stats
	p.stats = statistics{at: ended.at, next: ended.next, counting: ended.counting}
	return &ended
}

// statistics is what the video delivered in the current second, and where its clocks
// stand.
type statistics struct {
	packets, lost, units, random, bytes int
	// The time of the latest unit.
	at time.Duration
	// The sequence number the next packet carries, once a packet has come.
	next     uint16
	counting bool
}

// count counts p, and the packets that went missing ahead of it: a packet that comes late
// counts for none.
func (s *statistics) count(p rtsp.Packet) {
	s.packets++
	gap := p.Sequence - s.next
	switch {
	case !s.counting:
		gap = 0
	case gap >= 0x8000:
		return
	}
	s.lost += int(gap)
	s.next, s.counting = p.Sequence+1, true
}

// add counts an access unit, of timestamp, timed by clock if it is a frame.
func (s *statistics) add(nalus [][]byte, timestamp uint32, clock *rtsp.Clock) error {
	s.units++
	frame, random := false, false
	for _, nalu := range nalus {
		s.bytes += len(nalu)
		// Types below 32 are a picture's, 16 to 21 a random access point's (H.265 7.4.2.2).
		kind := rtsp.NALType(nalu)
		frame = frame || kind < 32
		random = random || (kind >= 16 && kind <= 21)
	}
	if random {
		s.random++
	}
	if !frame {
		return nil
	}
	at, err := clock.Time(timestamp)
	if err != nil {
		return err
	}
	s.at = at
	return nil
}

func (s *statistics) String() string {
	return fmt.Sprintf("%4d packets (%d lost) %3d units (%d random access) %8d B at %v", s.packets, s.lost, s.units, s.random, s.bytes, s.at.Truncate(time.Millisecond))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
