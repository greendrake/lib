package aws

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Profile is a model on an instance type, and the instance's lifetimes.
type Profile struct {
	// InstanceType is what the operator policy admits a launch of: the
	// profiles' types alone.
	InstanceType string `yaml:"instance_type"`
	// Image is vllm/vllm-openai, pinned.
	Image string `yaml:"image"`
	// Model is the model's repository, and Revision its commit, pinned.
	Model    string `yaml:"model"`
	Revision string `yaml:"revision"`
	// ServedName is what vLLM serves the model as, and what both tiers ask for.
	ServedName string `yaml:"served_name"`
	// ServerArgs are vLLM's arguments beyond the model.
	ServerArgs []string `yaml:"server_args"`
	// RequestExtra is merged into every request: the model's sampling.
	RequestExtra map[string]any `yaml:"request_extra"`
	// Parallel is the requests in flight, unless a run states its own.
	Parallel int `yaml:"parallel"`
	// RequestTimeoutMin bounds one request, in minutes: longer than the
	// largest batch's answer takes to reach its token cap here, so that an
	// answer that runs long ends at the cap, keeping what it finished, rather
	// than at the timeout with nothing.
	RequestTimeoutMin int `yaml:"request_timeout_min"`
	// The instance's own deadlines, in minutes: the most it lives, how long it
	// may serve nothing, and how long it may take to start serving.
	MaxLifetimeMin  int `yaml:"max_lifetime_min"`
	IdleMin         int `yaml:"idle_min"`
	BootDeadlineMin int `yaml:"boot_deadline_min"`
}

// gemma4 is a Gemma 4 model on one RTX PRO 6000.
func gemma4(model, revision, servedName string) Profile {
	return Profile{
		InstanceType: "g7e.2xlarge",
		Image:        "vllm/vllm-openai:v0.30.0",
		Model:        model,
		Revision:     revision,
		ServedName:   servedName,
		// vLLM refuses a request whose prompt plus cap exceeds the model
		// length. The translator caps a batch's answer at ten times its
		// source's estimated tokens and Gemma reasons in none beyond that, so
		// a page of ~12k characters — a long privacy policy — is capped at
		// ~36k, its prompt a few thousand more; this leaves room for a model
		// that reasons first.
		ServerArgs: []string{"--max-model-len", "65536", "--gpu-memory-utilization", "0.92"},
		// Gemma's own sampling, as its generation config states it:
		// near-greedy (0.2) it falls into loops in the languages it knows least
		// — "Orin Orin Orin…", an accent stacked on an accent — until the cap.
		// min_p drops any token rated below a tenth of the top one, which is
		// where a word of another language slipped into a long page comes from
		// at this temperature, and keeps the close alternatives that break a
		// loop.
		RequestExtra: map[string]any{"temperature": 1.0, "top_p": 0.95, "top_k": 64, "min_p": 0.1},
		// At 16 in flight the KV cache sat at ~65% with nothing waiting; 22
		// fills most of the rest without vLLM having to evict and recompute
		// requests.
		Parallel: 22,
		// That ~36k cap is more than 20 minutes away for one request alone:
		// the dense 31B's 62.5 GB of weights over the card's ~1.8 TB/s decode
		// it at under ~29 tokens a second, and among 21 others it decodes
		// slower still. An hour reaches it down to ~10 a second.
		RequestTimeoutMin: 60,
		MaxLifetimeMin:    360,
		IdleMin:           20,
		BootDeadlineMin:   30,
	}
}

// builtins are the profiles every deployment has.
var builtins = map[string]Profile{
	"gemma4-31b": gemma4("google/gemma-4-31B-it", "842da3794eaa0b77d5f08bae87a17459d91ff475", "gemma-4-31b"),
	"gemma4-26b": gemma4("google/gemma-4-26B-A4B-it", "4d7ae4984b7db7de8f8457170b3f1a419ee76d52", "gemma-4-26b"),
}

// Profiles are the built-ins, with those a profile file — a YAML mapping of
// name to profile, every field stated — adds or overrides. An empty path is
// the built-ins alone.
func Profiles(path string) (map[string]Profile, error) {
	profiles := maps.Clone(builtins)
	if path == "" {
		return profiles, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var declared map[string]Profile
	if err := decoder.Decode(&declared); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, name := range slices.Sorted(maps.Keys(declared)) {
		if err := declared[name].validate(); err != nil {
			return nil, fmt.Errorf("%s: profile %s: %w", path, name, err)
		}
		profiles[name] = declared[name]
	}
	return profiles, nil
}

// Pick is one of profiles by name; a name none has is refused.
func Pick(profiles map[string]Profile, name string) (Profile, error) {
	profile, ok := profiles[name]
	if !ok {
		return Profile{}, fmt.Errorf("no profile %q: the profiles are %s", name, strings.Join(slices.Sorted(maps.Keys(profiles)), ", "))
	}
	return profile, nil
}

// InstanceTypes are the instance types of profiles, each once, sorted: what
// the operator policy admits.
func InstanceTypes(profiles map[string]Profile) []string {
	var types []string
	for _, profile := range profiles {
		if !slices.Contains(types, profile.InstanceType) {
			types = append(types, profile.InstanceType)
		}
	}
	slices.Sort(types)
	return types
}

func (p Profile) validate() error {
	for field, value := range map[string]string{"instance_type": p.InstanceType, "image": p.Image, "model": p.Model, "revision": p.Revision, "served_name": p.ServedName} {
		if value == "" {
			return fmt.Errorf("`%s` is required", field)
		}
	}
	for field, value := range map[string]int{"parallel": p.Parallel, "request_timeout_min": p.RequestTimeoutMin, "max_lifetime_min": p.MaxLifetimeMin, "idle_min": p.IdleMin, "boot_deadline_min": p.BootDeadlineMin} {
		if value <= 0 {
			return fmt.Errorf("`%s` must be positive", field)
		}
	}
	if p.BootDeadlineMin >= p.MaxLifetimeMin {
		return errors.New("`boot_deadline_min` must be shorter than `max_lifetime_min`")
	}
	return nil
}

//go:embed instance.sh
var instanceScript string

// userData is the instance's boot script with the fields it reads prepended
// as upper-cased shell variables.
func (p Profile) userData() string {
	args := make([]string, len(p.ServerArgs))
	for i, arg := range p.ServerArgs {
		args[i] = shellQuote(arg)
	}
	var out strings.Builder
	out.WriteString("#!/bin/bash\n")
	for _, v := range [][2]string{
		{"IMAGE", shellQuote(p.Image)},
		{"MODEL", shellQuote(p.Model)},
		{"REVISION", shellQuote(p.Revision)},
		{"SERVED_NAME", shellQuote(p.ServedName)},
		{"SERVER_ARGS", "(" + strings.Join(args, " ") + ")"},
		{"MAX_LIFETIME_MIN", strconv.Itoa(p.MaxLifetimeMin)},
		{"IDLE_MIN", strconv.Itoa(p.IdleMin)},
		{"BOOT_DEADLINE_MIN", strconv.Itoa(p.BootDeadlineMin)},
	} {
		out.WriteString(v[0] + "=" + v[1] + "\n")
	}
	out.WriteString(instanceScript)
	return out.String()
}

// shellQuote is a word as the shell reads it back exactly.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
