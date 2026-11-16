package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"greendrake.nz/go/transcode/internal/transcodetest"
	"greendrake.nz/go/transcode/master"
	"greendrake.nz/go/transcode/slave"
)

// configFile is the path of a configuration file holding content.
func configFile(t *testing.T, content string) string {
	return transcodetest.Write(t, "config.yaml", []byte(content))
}

const full = `
Listen: 192.0.2.1:8486
Path: /transcode
Allow: [192.0.2.0/24, "2001:db8::/32"]
FFmpeg: /opt/homebrew/bin/ffmpeg
Spool: /var/tmp/transcode-slave
Jobs: 2
HWAccel: videotoolbox
Encoders:
  h264: {Name: h264_videotoolbox, Args: [-profile:v, high, -g, 250, -bf, 3]}
  hevc: {Name: hevc_videotoolbox, Args: [-g, 250, -bf, 3]}
`

func TestLoad(t *testing.T) {
	cfg, err := load(configFile(t, full))
	if err != nil {
		t.Fatal(err)
	}
	want := config{
		Listen:  "192.0.2.1:8486",
		Path:    "/transcode",
		Allow:   []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("2001:db8::/32")},
		FFmpeg:  "/opt/homebrew/bin/ffmpeg",
		Spool:   "/var/tmp/transcode-slave",
		Jobs:    2,
		HWAccel: "videotoolbox",
		Encoders: map[string]slave.Encoder{
			"h264": {Name: "h264_videotoolbox", Args: []string{"-profile:v", "high", "-g", "250", "-bf", "3"}},
			"hevc": {Name: "hevc_videotoolbox", Args: []string{"-g", "250", "-bf", "3"}},
		},
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("loaded %+v, want %+v", cfg, want)
	}

	minimal := strings.Join(slices.DeleteFunc(strings.Split(full, "\n"), func(line string) bool {
		return strings.HasPrefix(line, "HWAccel") || strings.HasPrefix(line, "Encoders") || strings.HasPrefix(line, "  ")
	}), "\n")
	if cfg, err := load(configFile(t, minimal)); err != nil || cfg.HWAccel != "" || cfg.Encoders != nil {
		t.Errorf("without the optional keys: %+v, %v", cfg, err)
	}

	for name, c := range map[string]struct{ content, want string }{
		"an unknown key":    {full + "Port: 8486\n", "field Port not found"},
		"missing keys":      {"Jobs: 1\n", "missing Listen, Path, Allow, FFmpeg, Spool"},
		"an empty key":      {strings.Replace(full, "Listen: 192.0.2.1:8486", `Listen: ""`, 1), "missing Listen"},
		"a relative path":   {strings.Replace(full, "Path: /transcode", "Path: transcode", 1), `Path "transcode" is no path`},
		"a trailing slash":  {strings.Replace(full, "Path: /transcode", "Path: /transcode/", 1), `Path "/transcode/" is no path`},
		"a space in a path": {strings.Replace(full, "Path: /transcode", `Path: "/trans code"`, 1), "is no path"},
		"a bad prefix":      {strings.Replace(full, "192.0.2.0/24", "192.0.2.0/33", 1), "192.0.2.0/33"},
	} {
		if _, err := load(configFile(t, c.content)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
	if _, err := load(filepath.Join(t.TempDir(), "none.yaml")); err == nil {
		t.Error("an absent file loaded")
	}
}

// running is the command serving cfg on a port of the loopback's, until the test stops it.
type running struct {
	addr string
	stop func() error // ends serve's context, answering what serve returned
}

func start(t *testing.T, cfg config) running {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg, ln, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	r := running{addr: ln.Addr().String(), stop: sync.OnceValue(func() error {
		cancel()
		return <-done
	})}
	t.Cleanup(func() { r.stop() })
	return r
}

func slaveConfig(t *testing.T, allow string) config {
	return config{
		Path:   "/transcode",
		Allow:  []netip.Prefix{netip.MustParsePrefix(allow)},
		FFmpeg: transcodetest.Tool(t, "ffmpeg"), Spool: t.TempDir(), Jobs: 1,
	}
}

func TestServe(t *testing.T) {
	cfg := slaveConfig(t, "127.0.0.0/8")
	r := start(t, cfg)

	m := master.Master{URL: "ws://" + r.addr + "/transcode"}
	out, err := transcodetest.Run(t, m, transcodetest.Clip(t, 160, 120), transcodetest.AVC)
	if err != nil {
		t.Fatal(err)
	}
	if video := transcodetest.Probe(t, out).Streams[0]; video.Codec != "h264" || video.Height != 120 {
		t.Errorf("the output's video: %+v", video)
	}

	resp, err := http.Get("http://" + r.addr + "/transcode/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var health map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	version, _ := health["ffmpeg"].(string)
	encoders, isObject := health["encoders"].(map[string]any) // none configured: an empty object
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(version, "ffmpeg version") || !isObject || len(encoders) != 0 || health["running"] != 0.0 || health["waiting"] != 0.0 {
		t.Errorf("health: %d %v", resp.StatusCode, health)
	}

	if err := r.stop(); err != nil {
		t.Errorf("stopping: %v", err)
	}
	if left, err := filepath.Glob(filepath.Join(cfg.Spool, "*")); err != nil || len(left) != 0 {
		t.Errorf("the spool holds %v, %v", left, err)
	}
}

// A master from outside Allow is refused, at both routes.
func TestServeRefuses(t *testing.T) {
	r := start(t, slaveConfig(t, "192.0.2.0/24"))
	for _, path := range []string{"/transcode", "/transcode/health"} {
		resp, err := http.Get("http://" + r.addr + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: %d", path, resp.StatusCode)
		}
	}
}

// A slave the configuration cannot make fails the start, and closes the listener.
func TestServeFails(t *testing.T) {
	cfg := slaveConfig(t, "127.0.0.0/8")
	cfg.Jobs = 0
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := serve(context.Background(), cfg, ln, slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil {
		t.Error("served a slave of no jobs")
	}
	if _, err := ln.Accept(); err == nil {
		t.Error("the listener is open")
	}
}
