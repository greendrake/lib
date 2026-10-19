// Command bake builds a binary with its single-page app inside. It builds the
// app (the -build command, `bun run build` unless given, in its directory,
// which leaves the build in its dist/; the environment passes through, so an
// app's build settings, such as its API base, ride it), puts the build into
// the main package's dist/ for the embed, keeping the tracked placeholder,
// then compiles the package into a static binary: CGO off, paths trimmed, no
// symbol tables, for this machine or the -target given, with the -version
// given stamped into the -version-var string variable, `main.version` unless
// given.
//
//	go run greendrake.nz/go/spa/cmd/bake -app ../web -o ipcam-app ./cmd/ipcam-app
//
// The target is a flag, not GOOS/GOARCH in the environment: those would have
// `go run` build bake itself for the target, and fail to run it here.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"greendrake.nz/go/spa"
)

// placeholder is the tracked file that lets a main package's embed compile
// before the app has been built.
const placeholder = ".gitkeep"

const usage = "usage: bake -app <app dir> -o <binary> [-build <command>] [-target <os>/<arch>] [-version <version> [-version-var <package>.<variable>]] <main package dir>"

func main() {
	app := flag.String("app", "", "the app's directory")
	out := flag.String("o", "", "the binary to write")
	buildCommand := flag.String("build", "bun run build", "the command building the app in its directory, words split on spaces")
	target := flag.String("target", "", "the os/arch to build for; this machine's when empty")
	version := flag.String("version", "", "the version to stamp into the binary")
	versionVar := flag.String("version-var", "main.version", "the string variable the version is stamped into")
	flag.Parse()
	build := strings.Fields(*buildCommand)
	if *app == "" || *out == "" || len(build) == 0 || flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	env, err := targetEnv(*target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bake: %v\n%s\n", err, usage)
		os.Exit(2)
	}
	if err := bake(*app, *out, flag.Arg(0), build, env, ldflags(*versionVar, *version)); err != nil {
		fmt.Fprintln(os.Stderr, "bake:", err)
		os.Exit(1)
	}
}

// targetEnv is the compile's environment for an <os>/<arch> target (none for
// this machine's).
func targetEnv(target string) ([]string, error) {
	if target == "" {
		return nil, nil
	}
	goos, goarch, ok := strings.Cut(target, "/")
	if !ok || goos == "" || goarch == "" {
		return nil, fmt.Errorf("target [%s] is not <os>/<arch>", target)
	}
	return []string{"GOOS=" + goos, "GOARCH=" + goarch}, nil
}

// ldflags strips the symbol tables and stamps the version into versionVar,
// where one is given.
func ldflags(versionVar, version string) string {
	if version == "" {
		return "-s -w"
	}
	return "-s -w -X " + versionVar + "=" + version
}

func bake(app, out, pkg string, buildCommand, env []string, ldflags string) error {
	build := exec.Command(buildCommand[0], buildCommand[1:]...)
	build.Dir = app
	if err := run(build); err != nil {
		return fmt.Errorf("building the app: %w", err)
	}
	if err := place(filepath.Join(app, spa.Dir), filepath.Join(pkg, spa.Dir)); err != nil {
		return err
	}
	compile := exec.Command("go", "build", "-trimpath", "-ldflags="+ldflags, "-o", out, pkg)
	compile.Env = append(append(os.Environ(), "CGO_ENABLED=0"), env...)
	if err := run(compile); err != nil {
		return fmt.Errorf("compiling %s: %w", pkg, err)
	}
	return nil
}

func run(cmd *exec.Cmd) error {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// place replaces the embed directory's contents with the app's build, keeping
// the placeholder.
func place(build, embed string) error {
	if _, err := os.Stat(filepath.Join(embed, placeholder)); err != nil {
		return fmt.Errorf("%s holds no %s: not an embed directory: %w", embed, placeholder, err)
	}
	entries, err := os.ReadDir(embed)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name() == placeholder {
			continue
		}
		if err := os.RemoveAll(filepath.Join(embed, e.Name())); err != nil {
			return err
		}
	}
	if err := os.CopyFS(embed, os.DirFS(build)); err != nil {
		return fmt.Errorf("placing the app's build: %w", err)
	}
	return nil
}
