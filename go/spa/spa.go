// Package spa serves a single-page app's build from its dist/ directory:
// embedded in a Go binary, whose main package embeds it and which the bake
// command fills before compiling the binary, or on disk, where a deploy
// replaces it under the running binary.
//
// The convention, in the binary's main package:
//
//	//go:embed all:dist
//	var dist embed.FS
//	...
//	app, err := spa.Handler(dist)
//	...
//	mux.Handle("/", app)
//
// with dist/.gitkeep tracked (and the rest of dist/ ignored), so the package
// compiles before the app has been built. On disk, the handler is over the
// directory holding dist: spa.Handler(os.DirFS(dir)).
package spa

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"slices"
	"time"
)

// Dir is where a binary's main package keeps the app's build, and where its
// embed of it finds it.
const Dir = "dist"

// Option configures Handler.
type Option func(*options)

type options struct {
	data []datum
}

// datum is a value for the index, as its JSON.
type datum struct {
	id    string
	value []byte
}

// Data hands the app a value of the binary's own at runtime — what the binary
// knows only once it runs, such as its configuration. Handler writes v's JSON
// into the index's data element of that id, which the app's build leaves in
// exactly this form for the binary to fill:
//
//	<script type="application/json" id="<id>"></script>
//
// A value that does not marshal is the binary's bug, and panics.
func Data(id string, v any) Option {
	value, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("spa: data %s: %v", id, err))
	}
	return func(o *options) { o.data = append(o.data, datum{id, value}) }
}

// Handler serves the app in fsys's Dir to GET and HEAD (any other method is
// 405): a regular file that exists as itself, and any other path — a
// directory's among them, which is never listed — the app's index.html, so
// that the app's own routes work on a full page load.
//
// Everything is read through fsys as it is asked for, so a build replaced
// under the handler — dist a symlink a deploy points at another build — is
// served from the next request on. A build without an index.html — the
// binary compiled before the app was baked in, or nothing deployed yet —
// serves 404s. An index.html without an element some Data fills is an error
// when Handler is called, and a 500, logged, on a request that finds one
// after: the app is never served without what it was meant to be handed.
func Handler(fsys fs.FS, opts ...Option) (http.Handler, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	app, err := fs.Sub(fsys, Dir)
	if err != nil {
		return nil, err
	}
	if _, err := o.index(app); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	files := http.FileServer(http.FS(app))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if p := r.URL.Path; p != "/" && p != "/index.html" {
			if info, err := fs.Stat(app, p[1:]); err == nil && info.Mode().IsRegular() {
				files.ServeHTTP(w, r)
				return
			}
		}
		index, err := o.index(app)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			http.NotFound(w, r)
		case err != nil:
			slog.Error("spa: serving the index", "err", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		default:
			http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
		}
	}), nil
}

// index is the app's index.html as it is now, every Data value written in.
func (o *options) index(app fs.FS) ([]byte, error) {
	index, err := fs.ReadFile(app, "index.html")
	if err != nil {
		return nil, err
	}
	for _, d := range o.data {
		if index, err = fill(index, d); err != nil {
			return nil, err
		}
	}
	return index, nil
}

// fill writes d's JSON into its element of index. JSON escapes <, > and &,
// so nothing in it can end the script element.
func fill(index []byte, d datum) ([]byte, error) {
	open := []byte(`<script type="application/json" id="` + d.id + `">`)
	start := bytes.Index(index, open)
	if start < 0 {
		return nil, fmt.Errorf("spa: index.html has no %s element to fill", open)
	}
	start += len(open)
	end := bytes.Index(index[start:], []byte("</script>"))
	if end < 0 {
		return nil, fmt.Errorf("spa: index.html's %s element is not closed", open)
	}
	return slices.Concat(index[:start], d.value, index[start+end:]), nil
}
