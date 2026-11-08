// Package command is what every command running the translator reads its
// command line with: the flags that narrow and log a run, and a flag given more
// than once.
package command

import (
	"flag"
	"log"
	"strings"

	"greendrake.nz/go/translate"
)

// Scope is a command's flags narrowing and logging a run.
type Scope struct {
	locale, retranslate *string
	verbose             *bool
}

// NewScope defines the flags on a flag set.
func NewScope(flags *flag.FlagSet) *Scope {
	return &Scope{
		locale:      flags.String("locale", "", "restrict the run to one shipped locale"),
		retranslate: flags.String("retranslate", "", "comma-separated unit-id prefixes to render again even if current (e.g. ui:common.)"),
		verbose:     flags.Bool("verbose", false, "log every prompt, completion and token count"),
	}
}

// Options are a run's options as the flags scope it, its lines logged.
func (s *Scope) Options() translate.Options {
	opts := translate.Options{Log: func(line string) { log.Println(line) }}
	if *s.locale != "" {
		opts.Only = []string{*s.locale}
	}
	opts.Retranslate = Split(*s.retranslate)
	return opts
}

// Split reads a comma-separated flag: each value trimmed, an empty one
// dropped.
func Split(list string) []string {
	var out []string
	for _, value := range strings.Split(list, ",") {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

// Verbose says every exchange with the model is to be logged.
func (s *Scope) Verbose() bool { return *s.verbose }

// List is a flag that may be given more than once.
type List []string

func (l *List) String() string { return strings.Join(*l, ",") }

func (l *List) Set(value string) error {
	*l = append(*l, value)
	return nil
}
