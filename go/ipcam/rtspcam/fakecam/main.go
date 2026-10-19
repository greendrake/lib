// Command fakecam runs an RTSP camera whose main and extra streams are where BITVISION's
// cameras serve theirs (rtspcam.BITVISION), each playing a recorded camera's stream at its
// frame rate over and over, for a client to be developed and tested against without the
// hardware.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"greendrake.nz/go/ipcam/rtspcam"
	"greendrake.nz/go/ipcam/rtspcam/internal/recording"
	"greendrake.nz/go/rtsp/rtsptest"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8554", "the address to serve on")
	user := flag.String("user", "admin", "the user it takes")
	password := flag.String("pass", "", "the user's password")
	flag.Parse()
	units, err := recording.Units()
	if err != nil {
		log.Fatal(err)
	}
	streams := map[string]rtsptest.Stream{}
	for _, path := range rtspcam.BITVISION.Paths {
		streams[path] = rtsptest.Video("H265", units)
	}
	camera, err := rtsptest.Listen(*listen, rtsptest.Options{User: *user, Password: *password, Streams: streams})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("fakecam: on %s", camera.Address())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	camera.Close()
}
