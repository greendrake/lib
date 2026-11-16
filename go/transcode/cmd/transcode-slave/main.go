// Command transcode-slave is a dedicated transcode slave: it does the jobs of the masters at the addresses its
// configuration allows, with ffmpeg and the encoders the configuration chooses.
//
//	transcode-slave config.yaml
//
// The configuration is YAML; an unknown key, or a required one missing, fails the start:
//
//	Listen: 192.0.2.1:8486
//	Path: /transcode                  # the WebSocket endpoint; GET <Path>/health answers the slave's health as JSON
//	Allow: [192.0.2.0/24]             # the masters' addresses
//	FFmpeg: /opt/homebrew/bin/ffmpeg
//	Spool: /var/tmp/transcode-slave   # where the slave holds outputs while their jobs run
//	Jobs: 1                           # jobs run at once; others wait their turn
//	HWAccel: videotoolbox             # input decoding; empty for none
//	Encoders:                         # codecs made by the hardware; any other by ffmpeg's default encoder for it
//	  h264: {Name: h264_videotoolbox, Args: [-profile:v, high, -g, 250, -bf, 3]}
//	  hevc: {Name: hevc_videotoolbox, Args: [-g, 250, -bf, 3]}
//
// HWAccel and Encoders are optional. VideoToolbox's encoders want the GOP their Args give them (slave.Encoder says
// why). It stops on SIGTERM or SIGINT: the server shut down, then the running jobs ended.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

	"gopkg.in/yaml.v3"
	"greendrake.nz/go/transcode"
	"greendrake.nz/go/transcode/slave"
)

// config is the command's configuration: where it serves, to whom, and the slave.
type config struct {
	Listen       string         `yaml:"Listen"`
	Path         string         `yaml:"Path"`
	Allow        []netip.Prefix `yaml:"Allow"`
	slave.Config `yaml:",inline"`
}

// endpoint is a Path: an absolute one of plain segments, no slash at its end.
var endpoint = regexp.MustCompile(`^(/[A-Za-z0-9._~-]+)+$`)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: transcode-slave <config.yaml>")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	cfg, err := load(os.Args[1])
	var ln net.Listener
	if err == nil {
		ln, err = net.Listen("tcp", cfg.Listen)
	}
	if err == nil {
		err = serve(ctx, cfg, ln, logger)
	}
	if err != nil {
		logger.Error("transcode-slave", "err", err)
		os.Exit(1)
	}
}

// load reads the configuration at path, refusing an unknown key, and a required one missing or empty.
func load(path string) (config, error) {
	f, err := os.Open(path)
	if err != nil {
		return config{}, err
	}
	defer f.Close()
	var cfg config
	decoder := yaml.NewDecoder(f)
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return config{}, fmt.Errorf("%s: %w", path, err)
	}
	var missing []string
	for _, key := range []struct {
		name    string
		missing bool
	}{
		{"Listen", cfg.Listen == ""},
		{"Path", cfg.Path == ""},
		{"Allow", len(cfg.Allow) == 0},
		{"FFmpeg", cfg.FFmpeg == ""},
		{"Spool", cfg.Spool == ""},
		{"Jobs", cfg.Jobs == 0},
	} {
		if key.missing {
			missing = append(missing, key.name)
		}
	}
	switch {
	case len(missing) > 0:
		return config{}, fmt.Errorf("%s: missing %s", path, strings.Join(missing, ", "))
	case !endpoint.MatchString(cfg.Path):
		return config{}, fmt.Errorf("%s: Path %q is no path such as /transcode", path, cfg.Path)
	}
	return cfg, nil
}

// serve serves the slave of cfg on ln until ctx ends, then shuts the server down and closes the slave.
func serve(ctx context.Context, cfg config, ln net.Listener, logger *slog.Logger) error {
	cfg.Logger = logger
	s, err := slave.New(cfg.Config)
	if err != nil {
		ln.Close()
		return err
	}
	routes := http.NewServeMux()
	routes.Handle("GET "+cfg.Path, s)
	routes.HandleFunc("GET "+cfg.Path+"/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.Health()) // a failed write is the client gone
	})
	srv := &http.Server{Handler: transcode.AllowFrom(cfg.Allow, routes), ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelWarn)}
	health := s.Health()
	logger.Info("transcode-slave: serving", "listen", ln.Addr().String(), "allow", cfg.Allow, "ffmpeg", health.FFmpeg, "encoders", health.Encoders, "jobs", cfg.Jobs)
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	select {
	case err = <-served:
	case <-ctx.Done():
		logger.Info("transcode-slave: stopping")
		err = srv.Shutdown(context.Background())
	}
	return errors.Join(err, s.Close())
}
