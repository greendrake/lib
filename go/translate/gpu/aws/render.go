package aws

import (
	"context"
	"fmt"
	"strings"
	"time"

	"greendrake.nz/go/llm"
	"greendrake.nz/go/translate"
)

// Run is one translator run a lease serves: a manifest, and for a library's
// the manifest of the project it runs for.
type Run struct {
	Manifest, Project string
}

// ParseRun reads a run as written on the command line:
// <manifest>[,<project manifest>].
func ParseRun(s string) (Run, error) {
	manifest, project, _ := strings.Cut(s, ",")
	if manifest == "" || strings.Contains(project, ",") {
		return Run{}, fmt.Errorf("run %q: want <manifest>[,<project manifest>]", s)
	}
	return Run{Manifest: manifest, Project: project}, nil
}

func (r Run) String() string {
	if r.Project == "" {
		return r.Manifest
	}
	return r.Manifest + " for " + r.Project
}

// Renderer renders runs, one lease serving all of them. Each is checked
// first: with nothing due nothing launches; with files alone behind every run
// renders here, without a model; with strings to render the lease is taken
// and every run renders in turn against the model it serves.
type Renderer struct {
	Runs []Run
	// Options narrow and log every run's check and render.
	Options translate.Options
	Profile Profile
	// Parallel bounds a run's requests in flight on the rented GPU; zero is
	// the profile's.
	Parallel int
	Verbose  bool
	// Lease takes the lease for a job.
	Lease func(ctx context.Context, job Job) error
}

// Render renders every run as far as it is due.
func (r Renderer) Render(ctx context.Context) error {
	log := r.Options.Logger()
	manifests := make([]*translate.Manifest, len(r.Runs))
	due := translate.Current
	for i, run := range r.Runs {
		m, err := translate.Open(run.Manifest, run.Project)
		if err != nil {
			return err
		}
		manifests[i] = m
		log(fmt.Sprintf("check %s", run))
		verdict, err := translate.Check(ctx, m, r.Options)
		if err != nil {
			return fmt.Errorf("%s: %w", run, err)
		}
		due = max(due, verdict)
	}
	switch due {
	case translate.Current:
		log("nothing to render — no instance launched")
		return nil
	case translate.FilesBehind:
		log("nothing needs a model — rendering here, without one")
		return r.each(ctx, manifests, r.Options)
	}
	parallel := r.Parallel
	if parallel == 0 {
		parallel = r.Profile.Parallel
	}
	return r.Lease(ctx, func(ctx context.Context, baseURL string) error {
		opts := r.Options
		opts.Parallel = parallel
		var err error
		if opts.Client, err = translate.NewClient(served(r.Profile, baseURL), r.Verbose); err != nil {
			return err
		}
		return r.each(ctx, manifests, opts)
	})
}

// each renders every run in turn, stopping at the first that fails.
func (r Renderer) each(ctx context.Context, manifests []*translate.Manifest, opts translate.Options) error {
	log := r.Options.Logger()
	for i, m := range manifests {
		log(fmt.Sprintf("render %s", r.Runs[i]))
		result, err := translate.Run(ctx, m, opts)
		if err != nil {
			return fmt.Errorf("%s: %w", r.Runs[i], err)
		}
		log(fmt.Sprintf("%s: %d file(s) written", r.Runs[i], len(result.Written)))
	}
	return nil
}

// served is the model every run renders with on the rented GPU: vLLM's
// OpenAI-compatible endpoint at the tunnel, the profile's model on both tiers
// with its sampling and its request timeout, answers decoded against the
// grammar the translator's schema compiles to. Nothing comes from a
// manifest's llm block — neither its timeout nor the environment block it
// names: they are sized for the host's own provider, not this model on this
// GPU.
func served(profile Profile, baseURL string) llm.Config {
	return llm.Config{
		Provider:    llm.ProviderOpenAICompat,
		BaseURL:     baseURL + "/v1",
		ModelFast:   profile.ServedName,
		ModelStrong: profile.ServedName,
		Extra:       profile.RequestExtra,
		Grammar:     true,
		Timeout:     time.Duration(profile.RequestTimeoutMin) * time.Minute,
	}
}
