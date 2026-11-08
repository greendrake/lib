package spa

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// get serves one GET through h.
func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

// The app's own routes serve its index on a full page load, not a 404; its files serve
// as themselves.
func TestHandler(t *testing.T) {
	h, err := Handler(fstest.MapFS{
		"dist/index.html":    {Data: []byte("<!doctype html><title>app</title>")},
		"dist/assets/app.js": {Data: []byte("app()")},
	})
	if err != nil {
		t.Fatal(err)
	}
	index := get(h, "/")
	if index.Code != 200 || index.Body.String() != "<!doctype html><title>app</title>" || index.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("GET /: %d %q %q", index.Code, index.Body, index.Header().Get("Content-Type"))
	}
	// A directory is no file: it is never listed.
	for _, path := range []string{"/switchboard", "/index.html", "/assets", "/assets/"} {
		if route := get(h, path); route.Code != 200 || route.Body.String() != index.Body.String() {
			t.Fatalf("GET %s: %d %q, want the index", path, route.Code, route.Body)
		}
	}
	if file := get(h, "/assets/app.js"); file.Code != 200 || file.Body.String() != "app()" {
		t.Fatalf("GET /assets/app.js: %d %q", file.Code, file.Body)
	}
	// Only reads: a POST is no page load.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/", nil))
	if rec.Code != 405 || rec.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST /: %d, Allow %q", rec.Code, rec.Header().Get("Allow"))
	}
}

// Data fills the index's element of its id with the value's JSON, escaped so
// nothing in it ends the script; the app's routes get the filled index too.
func TestData(t *testing.T) {
	h, err := Handler(fstest.MapFS{
		"dist/index.html": {Data: []byte(`<head><script type="application/json" id="env"></script></head>`)},
	}, Data("env", map[string]string{"key": "</script><b>"}))
	if err != nil {
		t.Fatal(err)
	}
	want := `<head><script type="application/json" id="env">{"key":"` + "\\u003c/script\\u003e\\u003cb\\u003e" + `"}</script></head>`
	for _, path := range []string{"/", "/route"} {
		if got := get(h, path); got.Code != 200 || got.Body.String() != want {
			t.Fatalf("GET %s: %d %q, want %q", path, got.Code, got.Body, want)
		}
	}
}

func TestDataRefuses(t *testing.T) {
	for _, tc := range []struct {
		name, index, want string
	}{
		{"no element", `<head></head>`, `index.html has no <script type="application/json" id="env"> element to fill`},
		{"an unclosed element", `<head><script type="application/json" id="env">`, "is not closed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Handler(fstest.MapFS{"dist/index.html": {Data: []byte(tc.index)}}, Data("env", 1))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

// A value that cannot be the index's JSON is the binary's bug.
func TestDataPanicsOnAValueThatDoesNotMarshal(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "spa: data env") {
			t.Fatalf("recovered %v", r)
		}
	}()
	Data("env", func() {})
}

// build writes a build of the app into dir/name: its index, and a file only it has.
func build(t *testing.T, dir, name, index string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, name, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	for file, data := range map[string]string{"index.html": index, "assets/" + name + ".js": name + "()"} {
		if err := os.WriteFile(filepath.Join(dir, name, file), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// point makes dir/dist the build name, as a deploy does: a symlink put over it.
func point(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.Symlink(name, filepath.Join(dir, "dist.next")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "dist.next"), filepath.Join(dir, Dir)); err != nil {
		t.Fatal(err)
	}
}

// A build on disk is served as it is when asked for: the next request after the
// deploy points dist at another build gets that build, with no new handler.
func TestABuildReplacedUnderTheHandler(t *testing.T) {
	dir := t.TempDir()
	build(t, dir, "one", `<title>one</title><script type="application/json" id="env"></script>`)
	build(t, dir, "two", `<title>two</title><script type="application/json" id="env"></script>`)
	point(t, dir, "one")
	h, err := Handler(os.DirFS(dir), Data("env", 1))
	if err != nil {
		t.Fatal(err)
	}
	for _, served := range []string{"one", "two"} {
		point(t, dir, served)
		want := `<title>` + served + `</title><script type="application/json" id="env">1</script>`
		if got := get(h, "/route"); got.Code != 200 || got.Body.String() != want {
			t.Fatalf("GET /route on %s: %d %q", served, got.Code, got.Body)
		}
		if got := get(h, "/assets/"+served+".js"); got.Code != 200 || got.Body.String() != served+"()" {
			t.Fatalf("GET its file on %s: %d %q", served, got.Code, got.Body)
		}
	}

	// A build whose index has lost the element is not served without the data.
	build(t, dir, "three", `<title>three</title>`)
	point(t, dir, "three")
	if got := get(h, "/"); got.Code != 500 {
		t.Fatalf("GET / on an index without the element: %d %q", got.Code, got.Body)
	}
}

// Nothing deployed yet is no build: the pages 404 until one is.
func TestNoBuildOnDisk(t *testing.T) {
	dir := t.TempDir()
	h, err := Handler(os.DirFS(dir), Data("env", 1))
	if err != nil {
		t.Fatal(err)
	}
	if got := get(h, "/"); got.Code != 404 {
		t.Fatalf("GET / before a build: %d", got.Code)
	}
	build(t, dir, "one", `<script type="application/json" id="env"></script>`)
	point(t, dir, "one")
	if got := get(h, "/"); got.Code != 200 {
		t.Fatalf("GET / once built: %d", got.Code)
	}
}

// Before the app is baked in, there is no index: its pages 404, Data or not.
func TestUnbaked(t *testing.T) {
	h, err := Handler(fstest.MapFS{"dist/.gitkeep": {}}, Data("env", 1))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/route"} {
		if got := get(h, path); got.Code != 404 {
			t.Fatalf("GET %s: %d, want 404", path, got.Code)
		}
	}
}
