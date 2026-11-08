package translate

import (
	"context"
	"fmt"

	"greendrake.nz/go/llm"
)

// What a command running the translator decides, for every command that runs
// it: the translator's own, and one renting a model to run it against.

// Open reads the manifest at path: a project's, or — given the manifest of
// the project it runs for — a library's.
func Open(path, project string) (*Manifest, error) {
	if project == "" {
		return Load(path, nil)
	}
	p, err := Load(project, nil)
	if err != nil {
		return nil, err
	}
	return Load(path, p)
}

// Verdict is what a check found.
type Verdict int

// Verdicts, each outranking the one before it.
const (
	// Current: the tree is as a run would leave it.
	Current Verdict = iota
	// FilesBehind: files alone are behind — removals, reordering, the derived
	// outputs — which a run settles without asking a model anything.
	FilesBehind
	// StringsDue: strings need rendering, which takes a model.
	StringsDue
)

// Check checks the manifest as opts scope a run of it, reporting through
// opts.Log each file not as a run would write it.
func Check(ctx context.Context, m *Manifest, opts Options) (Verdict, error) {
	opts.Check = true
	result, err := Run(ctx, m, opts)
	if err != nil {
		return Current, err
	}
	for _, path := range result.Files {
		opts.Logger()(fmt.Sprintf("%s is not what a run would write", path))
	}
	switch {
	case result.Clean():
		return Current, nil
	case result.ToRender() > 0:
		return StringsDue, nil
	}
	return FilesBehind, nil
}

// Client is the model the manifest renders with as its host configures one:
// the provider the environment block its llm.env_prefix names, with its
// llm.timeout where the environment states none. A provider left off is no
// client, which a run with nothing to render does not need.
func (m *Manifest) Client(verbose bool) (llm.Client, error) {
	cfg, err := llm.ConfigFromEnv(m.LLM.EnvPrefix)
	if err != nil {
		return nil, err
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = m.LLM.Timeout
	}
	return NewClient(cfg, verbose)
}

// NewClient is a client of a provider for a run, logging every prompt,
// completion and token count when verbose.
func NewClient(cfg llm.Config, verbose bool) (llm.Client, error) {
	return llm.New(cfg, func(context.Context) bool { return verbose })
}
