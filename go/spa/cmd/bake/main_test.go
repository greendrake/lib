package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// files lists every file under dir, relative and slash-separated.
func files(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		out = append(out, filepath.ToSlash(rel))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

// The embed directory ends up holding the new build and the placeholder:
// whatever an earlier build left goes.
func TestPlace(t *testing.T) {
	build, embed := t.TempDir(), t.TempDir()
	write(t, filepath.Join(build, "index.html"), "<!doctype html>")
	write(t, filepath.Join(build, "assets/app-new.js"), "app()")
	write(t, filepath.Join(embed, placeholder), "")
	write(t, filepath.Join(embed, "assets/app-old.js"), "old()")
	write(t, filepath.Join(embed, "stale.txt"), "stale")

	if err := place(build, embed); err != nil {
		t.Fatal(err)
	}
	if got, want := files(t, embed), []string{".gitkeep", "assets/app-new.js", "index.html"}; !slices.Equal(got, want) {
		t.Fatalf("embed holds %v, want %v", got, want)
	}
	if data, err := os.ReadFile(filepath.Join(embed, "assets/app-new.js")); err != nil || string(data) != "app()" {
		t.Fatalf("app-new.js = %q, %v", data, err)
	}
}

// A directory without the placeholder is not a main package's embed: placing
// a build there would empty a directory the binary may not even embed.
func TestPlaceRefusesDirectoryWithoutPlaceholder(t *testing.T) {
	build, embed := t.TempDir(), t.TempDir()
	write(t, filepath.Join(build, "index.html"), "<!doctype html>")
	write(t, filepath.Join(embed, "keep.txt"), "keep")
	if err := place(build, embed); err == nil {
		t.Fatal("place must refuse a directory without the placeholder")
	}
	if got := files(t, embed); !slices.Equal(got, []string{"keep.txt"}) {
		t.Fatalf("embed was touched: %v", got)
	}
}

func TestTargetEnv(t *testing.T) {
	if env, err := targetEnv(""); err != nil || env != nil {
		t.Fatalf("no target: %v, %v", env, err)
	}
	if env, err := targetEnv("linux/arm64"); err != nil || !slices.Equal(env, []string{"GOOS=linux", "GOARCH=arm64"}) {
		t.Fatalf("linux/arm64: %v, %v", env, err)
	}
	for _, bad := range []string{"linux", "linux/", "/arm64"} {
		if _, err := targetEnv(bad); err == nil {
			t.Errorf("target %q must be refused", bad)
		}
	}
}

// A version given is stamped into the variable named; none is stamped
// otherwise.
func TestLdflags(t *testing.T) {
	if got := ldflags("main.version", ""); got != "-s -w" {
		t.Errorf("no version: %q", got)
	}
	if got := ldflags("main.version", "0.1.0"); got != "-s -w -X main.version=0.1.0" {
		t.Errorf("0.1.0: %q", got)
	}
	if got := ldflags("example.com/app/build.Version", "0.1.0"); got != "-s -w -X example.com/app/build.Version=0.1.0" {
		t.Errorf("another variable: %q", got)
	}
}

// A build that produced nothing fails the bake.
func TestPlaceFailsWithoutBuild(t *testing.T) {
	embed := t.TempDir()
	write(t, filepath.Join(embed, placeholder), "")
	if err := place(filepath.Join(t.TempDir(), "missing"), embed); err == nil {
		t.Fatal("place must fail when the app produced no build")
	}
}
