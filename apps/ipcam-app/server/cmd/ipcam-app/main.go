// Command ipcam-app connects to one camera, over DVRIP or RTSP, and serves a browser app
// to watch its streams and, over DVRIP, hear them and change the camera's settings.
package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"greendrake.nz/go/apiserver"
	"greendrake.nz/go/ipcam"
	"greendrake.nz/go/ipcam/connection"
	"greendrake.nz/go/ipcam/fleet"
	"greendrake.nz/go/ipcam/protocols"
	"greendrake.nz/go/ipcamrpc"
	"greendrake.nz/go/rpcserver"
	"greendrake.nz/go/spa"
	"greendrake.nz/go/webcast"
)

// dist is the app's build, which ../../build bakes in (greendrake.nz/go/spa).
//
//go:embed all:dist
var dist embed.FS

const usage = `usage: ipcam-app [-protocol dvrip] [-user admin] [-pass ''] [-listen 127.0.0.1:0] [-origin <origin>]... <host[:port]>
       ipcam-app -version

Connects to the camera at host over -protocol, and serves an app to watch its streams,
printing where:

  dvrip  DVRIP (Sofia), the default: the streams with their sound, and the camera's
         settings; port 34567 unless given
  rtsp   RTSP: the streams alone, at /0 and /1 as BITVISION's cameras serve them;
         port 554 unless given

-listen picks the address to serve on; by default a free port on this machine only.
The password can be given as $IPCAM_PASS instead of -pass. The app's sockets admit
pages of the app's own origin, and of each -origin given, and each of $IPCAM_ORIGIN's,
comma-separated: an origin ("http://192.0.2.5:5173"), or a pattern of them
("http://*:5173"). -version prints the version this binary was built as.
`

// version is what a release build stamps in (bake -version).
var version = "dev"

// name is the camera's in the fleet of one it is: what the app finds it by.
const name = "camera"

// drainDeadline bounds stopping: the streams' Stops, the session, the listener.
const drainDeadline = 5 * time.Second

func main() {
	protocol := flag.String("protocol", "dvrip", "the camera's protocol: dvrip or rtsp")
	user := flag.String("user", "admin", "user name")
	password := flag.String("pass", os.Getenv("IPCAM_PASS"), "password ($IPCAM_PASS unless given)")
	listen := flag.String("listen", "127.0.0.1:0", "the address to serve the app on")
	origins := strings.FieldsFunc(os.Getenv("IPCAM_ORIGIN"), func(r rune) bool { return r == ',' })
	flag.Func("origin", "an origin, or pattern of origins, whose pages may use the app (repeatable)", func(origin string) error {
		origins = append(origins, origin)
		return nil
	})
	printVersion := flag.Bool("version", false, "print the version and exit")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()
	if *printVersion {
		fmt.Println(version)
		return
	}
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	address := flag.Arg(0)
	driver, err := protocols.Driver(*protocol)
	if err != nil {
		fail("%v", err)
	}

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	conn, err := connection.Dial(ctx, driver, address, *user, *password)
	switch {
	case errors.Is(err, ipcam.ErrCredentials):
		fail("%s refused user %q's password", address, *user)
	case errors.Is(err, ipcam.ErrUnreachable):
		fail("%s is unreachable: %v", address, err)
	case err != nil:
		fail("%s does not answer over %s: %v", address, *protocol, err)
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		fail("serving on %s: %v", *listen, err)
	}

	broadcast := rpcserver.NewBroadcast()
	cameras := fleet.New(ctx, map[string]fleet.Camera{name: {Conn: conn}}, "")
	cameras.Start(ipcamrpc.Cameras(broadcast, cameras))
	mux := http.NewServeMux()
	// The camera is on the LAN, and so is whoever uses the app: anyone may call it, from a
	// page of an origin it admits.
	mux.Handle("/api", broadcast.Handler(rpcserver.NewDispatcher(ipcamrpc.Methods(cameras, ipcamrpc.Access{})), rpcserver.WithWSOriginPatterns(origins...)))
	mux.Handle("GET /streams", ipcamrpc.Streams(ctx, cameras, webcast.WithOriginPatterns(origins...)))
	app, err := spa.Handler(dist)
	if err != nil {
		fail("serving the app: %v", err)
	}
	mux.Handle("/", app)

	p := apiserver.NewProcess("ipcam-app", drainDeadline, 0)
	p.Serve("app", ln, mux)
	// The fleet closes the login and stops the streams' monitors on its way out.
	p.Go(cameras.Wait)
	fmt.Printf("http://%s\n", ln.Addr())
	if err := p.Run(stop, nil); err != nil {
		fail("%v", err)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ipcam-app: "+format+"\n", args...)
	os.Exit(1)
}
