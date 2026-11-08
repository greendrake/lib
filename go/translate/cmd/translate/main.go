// Command translate renders a host's master into every locale its manifest
// ships.
//
// It is a build tool, not part of a running stack: a build calls it with
// --check, which reports what has fallen behind and writes nothing, and a
// person calls it without to actually render. That split is deliberate — a
// release ships files named by a tag, and text generated after the tag was
// cut would be text the tag does not contain and a redeploy could not
// reproduce.
//
// A check exits 0 when the tree is current, 3 when strings need rendering (a
// run needs a model) and 4 when only files are behind (a run settles them
// without asking a model anything); 1 is a failure, and 2 a flag it does not
// know.
//
//	translate -manifest translate.yaml [-project host/translate.yaml] [--check | --adopt | --settle] [--locale de] [--retranslate ui:common.] [--parallel 4] [--verbose]
//
// A library's manifest, which declares its master locale and no others, is run
// on behalf of a project with the same master, named by -project: in its
// locales, on its model.
//
// `carry` hands each locale's translations of catalogue keys to other keys —
// another product's, or other names — once the masters have changed, recording
// each as current where it still holds, for the project whose locales are
// carried:
//
//	translate carry -project host/translate.yaml -from host/translate.yaml -source ui -to lib/translate.yaml -target auth [-prefix auth.]... [-key to=from]...
//
// `prune` removes a library's files of every locale none of the projects
// serving it ships:
//
//	translate prune -manifest lib/translate.yaml -project host/translate.yaml [-project other/translate.yaml]...
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"greendrake.nz/go/translate"
	"greendrake.nz/go/translate/internal/command"
)

// checkExits are a check's exit statuses (the package comment has them all).
var checkExits = map[translate.Verdict]int{translate.Current: 0, translate.StringsDue: 3, translate.FilesBehind: 4}

// subcommands are the commands beside a run, by the word naming them.
var subcommands = map[string]func(args []string) error{"carry": carry, "prune": prune}

func main() {
	log.SetFlags(0)
	if len(os.Args) > 1 {
		if subcommand, ok := subcommands[os.Args[1]]; ok {
			if err := subcommand(os.Args[2:]); err != nil {
				log.Fatalf("translate %s: %v", os.Args[1], err)
			}
			return
		}
	}
	manifest := flag.String("manifest", "", "the host's manifest")
	project := flag.String("project", "", "the project manifest a library manifest is run on behalf of")
	check := flag.Bool("check", false, "report what is out of date and write nothing; exits 3 when strings need rendering, 4 when only files are behind")
	adopt := flag.Bool("adopt", false, "record the current policy as what the translations on disk were rendered under, rendering nothing")
	settle := flag.Bool("settle", false, "write every file and record as a run would from what the locales hold, rendering nothing")
	parallel := flag.Int("parallel", 0, "concurrent requests (default: the manifest's); 1 for a local model")
	scope := command.NewScope(flag.CommandLine)
	flag.Parse()
	if *manifest == "" {
		log.Fatal("translate: -manifest is required")
	}

	opts := scope.Options()
	opts.Check, opts.Adopt, opts.Settle, opts.Parallel = *check, *adopt, *settle, *parallel
	status, err := run(*manifest, *project, opts, scope.Verbose())
	if err != nil {
		log.Fatalf("translate: %v", err)
	}
	os.Exit(status)
}

func run(path, project string, opts translate.Options, verbose bool) (int, error) {
	m, err := translate.Open(path, project)
	if err != nil {
		return 0, err
	}
	if opts.Check {
		verdict, err := translate.Check(context.Background(), m, opts)
		if err != nil {
			return 0, err
		}
		if verdict == translate.Current {
			log.Println("translations are current")
		} else {
			log.Printf("translations are behind the %s master: run `%s` and commit the result", m.Default, m.Command)
		}
		return checkExits[verdict], nil
	}

	// The client is built only when there may be rendering to do: adopting
	// and settling must work on a machine with no provider.
	if !opts.Adopt && !opts.Settle {
		if opts.Client, err = m.Client(verbose); err != nil {
			return 0, err
		}
	}
	result, err := translate.Run(context.Background(), m, opts)
	if err != nil {
		return 0, err
	}
	switch {
	case len(result.Written) == 0:
		log.Println("translations are current; nothing written")
	default:
		log.Printf("%d file(s) written", len(result.Written))
		if len(opts.Only) > 0 {
			// The host's verification reads the whole tree, and a run
			// restricted to one locale has deliberately left the others as
			// they were.
			log.Println("one locale only — run without --locale before building, which is what verifies the tree as a whole")
		}
	}
	return 0, nil
}

func carry(args []string) error {
	flags := flag.NewFlagSet("carry", flag.ExitOnError)
	project := flags.String("project", "", "the project manifest the carry is for: a library manifest among -from and -to is read on its behalf, in its locales")
	from := flags.String("from", "", "the manifest the translations come from")
	source := flags.String("source", "", "its catalogue source")
	to := flags.String("to", "", "the manifest the translations go to")
	target := flags.String("target", "", "its catalogue source")
	var prefixes, keys command.List
	flags.Var(&prefixes, "prefix", "every target key under this prefix, from the same key (repeatable; '' is every key)")
	flags.Var(&keys, "key", "a target key from a source key, as to=from (repeatable)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	for name, value := range map[string]string{"project": *project, "from": *from, "source": *source, "to": *to, "target": *target} {
		if value == "" {
			return fmt.Errorf("-%s is required", name)
		}
	}
	carriage := translate.Carriage{Prefixes: prefixes, Keys: map[string]string{}}
	for _, pair := range keys {
		toKey, fromKey, ok := strings.Cut(pair, "=")
		if !ok || toKey == "" || fromKey == "" {
			return fmt.Errorf("-key %q: want to=from", pair)
		}
		carriage.Keys[toKey] = fromKey
	}
	fromManifest, err := translate.Open(*from, *project)
	if err != nil {
		return err
	}
	toManifest, err := translate.Open(*to, *project)
	if err != nil {
		return err
	}
	written, err := translate.Carry(fromManifest, *source, toManifest, *target, carriage)
	if err != nil {
		return err
	}
	log.Printf("%d file(s) written", len(written))
	return nil
}

func prune(args []string) error {
	flags := flag.NewFlagSet("prune", flag.ExitOnError)
	manifest := flags.String("manifest", "", "the library's manifest")
	var projects command.List
	flags.Var(&projects, "project", "the manifest of a project serving the library, whose locales it keeps (repeatable)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *manifest == "" {
		return errors.New("-manifest is required")
	}
	var serving []*translate.Manifest
	for _, path := range projects {
		project, err := translate.Open(path, "")
		if err != nil {
			return err
		}
		serving = append(serving, project)
	}
	removed, err := translate.Prune(*manifest, serving)
	if err != nil {
		return err
	}
	log.Printf("%d file(s) removed", len(removed))
	return nil
}
