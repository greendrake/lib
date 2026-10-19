// Command dvrip talks to a camera over DVRIP: reads and writes its configuration,
// queries what it supports, and pulls its streams for inspection.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"greendrake.nz/go/dvrip"
)

const usage = `usage: dvrip -addr host[:port] [-user admin] [-pass ''] command

commands:
  info                       what the device reports about itself and supports
  config get <name>          a configuration, e.g. General.Location, Camera.Param.[0]
  config set <name> <json>   replace a configuration
  config sweep [dir]         every configuration, ability and report the library types,
                             and the device's clock; with dir, each reply is also saved
                             to <dir>/<name>.json, the form dvriptest keeps a recorded
                             device's replies in
  config watch [name...]     read the configurations named, every group the cameras
                             keep if none, each second, printing every value that
                             changes: what a settings page writes where
  config export <file>       the device's whole configuration, as the zip it exports
  ability <name>             one of the device's abilities, e.g. EncodeCapability
  time                       the device's clock
  settime                    set the device's clock to this machine's wall time
  reboot                     restart the device
  monitor [-stream main,extra[:duration]] [-channel n] [-for duration] [-record prefix]
                             statistics each second of every stream named, all on one
                             session; a stream with a duration is stopped after it while
                             the others go on; -record saves each stream's frames to
                             <prefix>.<stream>.bin, which dvrip.NewReplay plays back
  hash <password>            a password as it goes on the wire
`

// store is which of the device's queries a sweep item is read with.
type store int

const (
	configStore store = iota
	abilityStore
	infoStore
	clockStore
)

// sweep is what "config sweep" reads: the configurations, abilities and reports the
// library gives types to, by the names a single-channel camera answers to, and the
// clock, which a dvriptest camera starts from.
var sweep = []struct {
	name  string
	store store
}{
	{name: "SystemInfo", store: infoStore},
	{name: "OPTimeQuery", store: clockStore},
	{name: dvrip.General.Name},
	{name: dvrip.Location.Name},
	{name: dvrip.AutoMaintain.Name},
	{name: dvrip.OnlineUpgrade.Name},
	{name: dvrip.TimeZone.Name},
	{name: dvrip.NetCommon.Name},
	{name: dvrip.IPAdaptive.Name},
	{name: dvrip.NetDHCP.Name},
	{name: dvrip.NetDNS.Name},
	{name: dvrip.OnvifPwdCheckout.Name},
	{name: dvrip.Correspondent.Name},
	{name: dvrip.NetNTP.Name},
	{name: dvrip.SimplifyEncode.Name},
	{name: dvrip.SmartH264.All().Name},
	{name: dvrip.SmartH264V2.Channel(0).Name},
	{name: dvrip.CameraParam.Channel(0).Name},
	{name: dvrip.CameraParamEx.Channel(0).Name},
	{name: dvrip.ClearFog.All().Name},
	{name: dvrip.VideoWidget.Channel(0).Name},
	{name: dvrip.SystemFunction.Name, store: abilityStore},
	{name: dvrip.EncodeCapability.Name, store: abilityStore},
	{name: dvrip.MultiLanguage.Name, store: abilityStore},
	{name: dvrip.MultiVstd.Name, store: abilityStore},
}

// groups are the configuration groups the cameras keep: what "config watch" reads
// unless named configurations.
var groups = []string{"AVEnc", "Ability", "Alarm", "Camera", "ChannelTitle", "Detect", "General", "IPAdaptive", "NetWork", "OEMcfg", "Record", "Storage", "System", "Uart", "fVideo"}

func main() {
	address := flag.String("addr", "", "the device, host or host:port")
	user := flag.String("user", "admin", "user name")
	password := flag.String("pass", "", "password")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()
	args := flag.Args()
	if len(args) == 2 && args[0] == "hash" {
		fmt.Println(dvrip.SofiaHash(args[1]))
		return
	}
	if *address == "" || len(args) == 0 {
		flag.Usage()
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	s, err := dvrip.Dial(dialCtx, *address, *user, *password)
	if err != nil {
		fail(err)
	}
	defer s.Close()
	if err := run(ctx, s, args); err != nil {
		fail(err)
	}
}

func run(ctx context.Context, s *dvrip.Session, args []string) error {
	switch {
	case args[0] == "info":
		info, err := s.SystemInfo(ctx)
		if err != nil {
			return err
		}
		functions, err := dvrip.SystemFunction.Get(ctx, s)
		if err != nil {
			return err
		}
		return printJSON(map[string]any{"SystemInfo": info, "SystemFunction": functions})
	case len(args) == 3 && args[0] == "config" && args[1] == "get":
		return printRaw(s.Get(ctx, args[2]))
	case len(args) == 4 && args[0] == "config" && args[1] == "set":
		if !json.Valid([]byte(args[3])) {
			return fmt.Errorf("not JSON: %s", args[3])
		}
		restart, err := s.Set(ctx, args[2], json.RawMessage(args[3]))
		if err == nil && restart {
			fmt.Println("written; the device applies it once restarted")
		}
		return err
	case (len(args) == 2 || len(args) == 3) && args[0] == "config" && args[1] == "sweep":
		clock := func(ctx context.Context, _ string) (json.RawMessage, error) {
			t, err := s.Time(ctx)
			if err != nil {
				return nil, err
			}
			return json.Marshal(t.Format(time.DateTime))
		}
		reads := map[store]func(context.Context, string) (json.RawMessage, error){configStore: s.Get, abilityStore: s.Ability, infoStore: s.Info, clockStore: clock}
		for _, item := range sweep {
			fmt.Printf("== %s\n", item.name)
			value, err := reads[item.store](ctx, item.name)
			if err != nil {
				fmt.Println(err)
				continue
			}
			text, err := indent(value)
			if err != nil {
				return err
			}
			fmt.Print(text)
			if len(args) == 3 {
				if err := os.WriteFile(filepath.Join(args[2], item.name+".json"), []byte(text), 0o644); err != nil {
					return err
				}
			}
		}
		return nil
	case len(args) >= 2 && args[0] == "config" && args[1] == "watch":
		return watch(ctx, s, args[2:])
	case len(args) == 3 && args[0] == "config" && args[1] == "export":
		archive, err := s.ExportConfig(ctx)
		if err != nil {
			return err
		}
		return os.WriteFile(args[2], archive, 0o644)
	case len(args) == 2 && args[0] == "ability":
		return printRaw(s.Ability(ctx, args[1]))
	case args[0] == "time":
		clock, err := s.Time(ctx)
		if err != nil {
			return err
		}
		fmt.Println(clock.Format(time.DateTime))
		return nil
	case args[0] == "settime":
		return s.SetTime(ctx, time.Now())
	case args[0] == "reboot":
		return s.Reboot(ctx)
	case args[0] == "monitor":
		return monitor(ctx, s, args[1:])
	}
	return fmt.Errorf("unknown command: %v", args)
}

var streamTypes = map[string]dvrip.StreamType{"main": dvrip.Main, "extra": dvrip.Extra}

// stream is one of the streams monitor runs.
type stream struct {
	name    string
	monitor *dvrip.Monitor
	// When to stop it while the others go on; 0 runs it to the end.
	stopAfter time.Duration
	// Set once it has been stopped on purpose, after which its end is no failure.
	stopped atomic.Bool
	stats   statistics
}

// event is what a stream's reader reports: a frame, or the end of the stream.
type event struct {
	stream *stream
	frame  dvrip.Frame
	err    error
}

// monitor runs every stream named on the one session, printing each second what each
// delivered in it.
func monitor(ctx context.Context, s *dvrip.Session, args []string) error {
	flags := flag.NewFlagSet("monitor", flag.ExitOnError)
	names := flags.String("stream", "main", "streams, comma-separated, each optionally :duration")
	channel := flags.Int("channel", 0, "channel")
	duration := flags.Duration("for", 0, "how long to run; until interrupted if 0")
	record := flags.String("record", "", "prefix of the files to save each stream's frames to")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *duration)
		defer cancel()
	}
	var streams []*stream
	for _, spec := range strings.Split(*names, ",") {
		name, after, timed := strings.Cut(spec, ":")
		typ, known := streamTypes[name]
		if !known {
			return fmt.Errorf("unknown stream %q", name)
		}
		st := &stream{name: name}
		if timed {
			d, err := time.ParseDuration(after)
			if err != nil {
				return fmt.Errorf("stream %s: %w", name, err)
			}
			st.stopAfter = d
		}
		m, err := s.Monitor(ctx, *channel, typ)
		if err != nil {
			return fmt.Errorf("stream %s: %w", name, err)
		}
		st.monitor = m
		if *record != "" {
			file, err := os.Create(*record + "." + name + ".bin")
			if err != nil {
				return err
			}
			defer file.Close()
			m.Record(file)
		}
		streams = append(streams, st)
	}
	events := make(chan event)
	stop := func(st *stream) error {
		st.stopped.Store(true)
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return st.monitor.Close(stopCtx)
	}
	started := time.Now()
	for _, st := range streams {
		go func() {
			for {
				f, err := st.monitor.Next()
				events <- event{stream: st, frame: f, err: err}
				if err != nil {
					return
				}
			}
		}()
		if st.stopAfter > 0 {
			time.AfterFunc(st.stopAfter, func() {
				if err := stop(st); err != nil {
					events <- event{stream: st, err: fmt.Errorf("stopping: %w", err)}
				}
			})
		}
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	running := len(streams)
	for running > 0 {
		select {
		case <-ctx.Done():
			// The device closes each media connection once its stream is stopped, which
			// ends the readers.
			for _, st := range streams {
				if !st.stopped.Load() {
					if err := stop(st); err != nil {
						return fmt.Errorf("stream %s: %w", st.name, err)
					}
				}
			}
			return nil
		case e := <-events:
			if e.err == nil {
				e.stream.stats.add(e.frame)
				continue
			}
			if !e.stream.stopped.Load() {
				return fmt.Errorf("stream %s: %w", e.stream.name, e.err)
			}
			fmt.Printf("%s stopped; session %v\n", e.stream.name, sessionState(s))
			running--
		case <-ticker.C:
			wall := time.Since(started).Truncate(time.Millisecond)
			for _, st := range streams {
				if !st.stopped.Load() {
					fmt.Printf("%7v %-5s %v\n", wall, st.name, &st.stats)
					st.stats.second()
				}
			}
		}
	}
	return nil
}

func sessionState(s *dvrip.Session) string {
	if err := s.Err(); err != nil {
		return "ended: " + err.Error()
	}
	return "alive"
}

// statistics is what a stream delivered in the current second, and where its clocks
// stand.
type statistics struct {
	video, keyframes, audio int
	videoBytes, audioBytes  int
	// The latest frame of each kind.
	lastVideo, lastAudio dvrip.Frame
	// Where the next audio frame is due by the samples so far, once there has been one,
	// and how many frames arrived somewhere else: the timeline pulling audio back onto
	// the video's clock.
	audioSeen bool
	nextAudio time.Duration
	reanchors int
}

func (s *statistics) add(f dvrip.Frame) {
	if f.Kind == dvrip.Video {
		s.video++
		s.videoBytes += len(f.Data)
		if f.Keyframe {
			s.keyframes++
		}
		s.lastVideo = f
		return
	}
	if s.audioSeen && (f.PTS-s.nextAudio).Abs() > time.Microsecond {
		s.reanchors++
	}
	s.audioSeen = true
	s.audio++
	s.audioBytes += len(f.Data)
	s.lastAudio = f
	s.nextAudio = f.PTS + time.Duration(len(f.Data))*time.Second/time.Duration(f.SampleRate)
}

// second starts the next second's counts, keeping the clocks.
func (s *statistics) second() {
	*s = statistics{lastVideo: s.lastVideo, lastAudio: s.lastAudio, audioSeen: s.audioSeen, nextAudio: s.nextAudio}
}

func (s *statistics) String() string {
	v, a := s.lastVideo, s.lastAudio
	return fmt.Sprintf("video %2d frames (%d key) %7d B %v %dx%d@%d at %v | audio %2d frames %5d B %v %d Hz at %v (%+v), pulled back %d",
		s.video, s.keyframes, s.videoBytes, v.Codec, v.Width, v.Height, v.FPS, v.PTS.Truncate(time.Millisecond),
		s.audio, s.audioBytes, a.Codec, a.SampleRate, a.PTS.Truncate(time.Millisecond), (a.PTS - v.PTS).Truncate(time.Millisecond), s.reanchors)
}

func printJSON(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func printRaw(value json.RawMessage, err error) error {
	if err != nil {
		return err
	}
	text, err := indent(value)
	fmt.Print(text)
	return err
}

// indent lays out a reply as the device wrote it: its keys in its order, its numbers
// as written.
func indent(value json.RawMessage) (string, error) {
	var out bytes.Buffer
	if err := json.Indent(&out, value, "", "  "); err != nil {
		return "", err
	}
	out.WriteByte('\n')
	return out.String(), nil
}

// watch reads the configurations named each second and prints every value that
// changes, by its path: Camera.Param.[0].DayNightColor.
func watch(ctx context.Context, s *dvrip.Session, names []string) error {
	if len(names) == 0 {
		names = groups
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var last map[string]string
	for {
		values := map[string]string{}
		for _, name := range names {
			value, err := s.Get(ctx, name)
			if ctx.Err() != nil {
				// Interrupted.
				return nil
			}
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			decoder := json.NewDecoder(bytes.NewReader(value))
			decoder.UseNumber()
			var v any
			if err := decoder.Decode(&v); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			if err := flatten(name, v, values); err != nil {
				return err
			}
		}
		changed := map[string]bool{}
		if last == nil {
			fmt.Printf("watching %d values\n", len(values))
		} else {
			for _, m := range []map[string]string{last, values} {
				for path := range m {
					if shown(last, path) != shown(values, path) {
						changed[path] = true
					}
				}
			}
		}
		for _, path := range slices.Sorted(maps.Keys(changed)) {
			fmt.Printf("%s %s: %s -> %s\n", time.Now().Format(time.TimeOnly), path, shown(last, path), shown(values, path))
		}
		last = values
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// flatten puts every scalar in v into values, as JSON, by its path from path.
func flatten(path string, v any, values map[string]string) error {
	switch v := v.(type) {
	case map[string]any:
		for key, e := range v {
			if err := flatten(path+"."+key, e, values); err != nil {
				return err
			}
		}
	case []any:
		for i, e := range v {
			if err := flatten(fmt.Sprintf("%s.[%d]", path, i), e, values); err != nil {
				return err
			}
		}
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		values[path] = string(data)
	}
	return nil
}

func shown(values map[string]string, path string) string {
	if v, known := values[path]; known {
		return v
	}
	return "(none)"
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
