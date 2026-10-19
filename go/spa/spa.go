// Package spa ships a single-page app inside a Go binary: the binary's main
// package embeds the app's build from its dist/ directory, which the bake
// command fills before compiling the binary, and Handler serves it.
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
// compiles before the app has been built.
package spa

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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

type datum struct {
	id    string
	value any
}

// Data hands the app a value of the binary's own at runtime — what the binary
// knows only once it runs, such as its configuration. Handler writes v's JSON
// into the index's data element of that id, which the app's build leaves in
// exactly this form for the binary to fill:
//
//	<script type="application/json" id="<id>"></script>
func Data(id string, v any) Option {
	return func(o *options) { o.data = append(o.data, datum{id, v}) }
}

// Handler serves the app in fsys's Dir to GET and HEAD (any other method is
// 405): a regular file that exists as itself, and any other path — a
// directory's among them, which is never listed — the app's index.html, so
// that the app's own routes work on a full page load. A build
// without an index.html — the binary compiled before the app was baked in —
// serves 404s; an index.html without an element some Data fills is an error.
func Handler(fsys fs.FS, opts ...Option) (http.Handler, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	app, err := fs.Sub(fsys, Dir)
	if err != nil {
		return nil, err
	}
	index, err := fs.ReadFile(app, "index.html")
	switch {
	case errors.Is(err, fs.ErrNotExist):
		index = nil
	case err != nil:
		return nil, err
	default:
		for _, d := range o.data {
			if index, err = fill(index, d); err != nil {
				return nil, err
			}
		}
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
		if index == nil {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
	}), nil
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
	value, err := json.Marshal(d.value)
	if err != nil {
		return nil, fmt.Errorf("spa: data %s: %w", d.id, err)
	}
	return slices.Concat(index[:start], value, index[start+end:]), nil
}
