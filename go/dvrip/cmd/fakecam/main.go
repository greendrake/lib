// Command fakecam runs a recorded DVRIP camera from dvriptest, for a client to be
// developed and tested against without the hardware.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"greendrake.nz/go/dvrip/dvriptest"
)

// systemRestart is the status a camera answers a write it applies only once restarted.
const systemRestart = 603

func main() {
	device := flag.String("device", "techage", "the recorded device to play")
	listen := flag.String("listen", "127.0.0.1:34567", "the address to serve on")
	reboot := flag.Duration("reboot", 0, "how long a reboot keeps the camera unreachable")
	realtime := flag.Bool("realtime", false, "play the recordings at their frame rate, over and over")
	restart := flag.String("restart", "", "configurations whose writes take a restart, comma-separated")
	control := flag.String("control", "", "the address to serve the control endpoint on, if any: PATCH /configs/{name} lays the body over that configuration, GET /commands/{name} counts the commands received under the name, PUT and DELETE /holds/{name} hold those commands unanswered and release them, GET /title answers the size of the title bitmap drawn last")
	flag.Parse()
	setStatus := map[string]int{}
	for _, name := range strings.FieldsFunc(*restart, func(r rune) bool { return r == ',' }) {
		setStatus[name] = systemRestart
	}
	camera, err := dvriptest.Listen(*listen, *device, log.Printf, dvriptest.Options{RebootPause: *reboot, Realtime: *realtime, SetStatus: setStatus})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("fakecam: %s on %s", *device, camera.Address())
	if *control != "" {
		ln, err := net.Listen("tcp", *control)
		if err != nil {
			log.Fatal(err)
		}
		go serveControl(ln, camera)
		log.Printf("fakecam: control on %s", ln.Addr())
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	camera.Close()
}

// serveControl serves the endpoint a test changes the camera through while it runs, and
// asks it what it was sent.
func serveControl(ln net.Listener, camera *dvriptest.Camera) {
	mux := http.NewServeMux()
	answer := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("GET /commands/{name}", func(w http.ResponseWriter, r *http.Request) {
		answer(w, len(camera.Commands(r.PathValue("name"))))
	})
	mux.HandleFunc("GET /title", func(w http.ResponseWriter, r *http.Request) {
		width, height, _, drawn := camera.Title()
		if !drawn {
			http.Error(w, "no title drawn", http.StatusNotFound)
			return
		}
		answer(w, map[string]int{"Width": width, "Height": height})
	})
	for method, act := range map[string]func(name string){"PUT": camera.Hold, "DELETE": camera.Release} {
		mux.HandleFunc(method+" /holds/{name}", func(w http.ResponseWriter, r *http.Request) {
			act(r.PathValue("name"))
			w.WriteHeader(http.StatusNoContent)
		})
	}
	mux.HandleFunc("PATCH /configs/{name}", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err == nil {
			err = camera.Overlay(r.PathValue("name"), body)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	log.Fatalf("fakecam: control: %v", http.Serve(ln, mux))
}
