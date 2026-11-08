// Command translate-gpu renders translator runs on a GPU rented in AWS for
// their length.
//
//	translate-gpu -run translate.yaml [-run lib/translate.yaml,translate.yaml]... -profile gemma4-31b [-profiles profiles.yaml] [-parallel 22] [-name translate-gpu] [-locale de] [-retranslate ui:common.] [-verbose]
//
// Every run is checked first: with nothing due nothing launches, with files
// alone behind every run renders here without a model, and with strings to
// render an instance is launched wherever it costs least at the time — of the
// regions setup prepared, the cheapest zone with capacity, spot or on demand
// as the quotas allow — serving the profile's model, and every run renders
// against it in turn. The instance is terminated on every way out: the runs
// done, an error, an interrupt or SIGTERM, a second of either ignored. AWS is
// reached as the standard credential chain says, and the tunnel through the
// AWS CLI's session-manager-plugin, which must be installed.
//
// `setup` converges, as the account's administrator, what a run needs in the
// regions named — by default every region the account has enabled that offers
// a profile's instance type — requesting the GPU quotas one instance needs
// where they are short:
//
//	translate-gpu setup [-regions eu-north-1,us-west-2] [-profiles profiles.yaml] [-account 123456789012] [-user translate-gpu -key-file operator.env] [-name translate-gpu]
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/aws/aws-sdk-go-v2/config"

	"greendrake.nz/go/translate/gpu/aws"
	"greendrake.nz/go/translate/internal/command"
)

const (
	nameHelp     = "what the AWS side is named after: the operator policy <name>-operator, the instance role and profile <name>-instance, the security groups and the Project tag"
	profilesHelp = "a YAML file of profiles, by name, adding to or overriding the built-in ones"
)

func main() {
	log.SetFlags(0)
	if len(os.Args) > 1 && os.Args[1] == "setup" {
		if err := setup(os.Args[2:]); err != nil {
			log.Fatalf("translate-gpu setup: %v", err)
		}
		return
	}
	var runs command.List
	flag.Var(&runs, "run", "a run to render: a project's manifest, or a library's as <manifest>,<project manifest> (repeatable)")
	profileName := flag.String("profile", "", "the profile to rent: a model on an instance type")
	profilesFile := flag.String("profiles", "", profilesHelp)
	parallel := flag.Int("parallel", 0, "concurrent requests (default: the profile's)")
	name := flag.String("name", "translate-gpu", nameHelp)
	scope := command.NewScope(flag.CommandLine)
	flag.Parse()
	if err := render(runs, *profileName, *profilesFile, *parallel, aws.Names(*name), scope); err != nil {
		log.Fatalf("translate-gpu: %v", err)
	}
}

func render(runs []string, profileName, profilesFile string, parallel int, names aws.Names, scope *command.Scope) error {
	if len(runs) == 0 {
		return errors.New("-run names no run")
	}
	renderer := aws.Renderer{Options: scope.Options(), Parallel: parallel, Verbose: scope.Verbose()}
	for _, run := range runs {
		parsed, err := aws.ParseRun(run)
		if err != nil {
			return err
		}
		renderer.Runs = append(renderer.Runs, parsed)
	}
	profiles, err := aws.Profiles(profilesFile)
	if err != nil {
		return err
	}
	if renderer.Profile, err = aws.Pick(profiles, profileName); err != nil {
		return err
	}
	ctx := interruptible()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}
	renderer.Lease = aws.NewLease(cfg, names, renderer.Profile, logLine).Run
	return renderer.Render(ctx)
}

func setup(args []string) error {
	flags := flag.NewFlagSet("setup", flag.ExitOnError)
	regions := flags.String("regions", "", "the regions a run may rent in, comma-separated (default: every region the account has enabled that offers a profile's instance type)")
	profilesFile := flags.String("profiles", "", profilesHelp+": the policy admits every profile's instance type")
	account := flags.String("account", "", "the only account to act in")
	user := flags.String("user", "", "the operator user: created where missing, its key written to -key-file, the policy attached to it")
	keyFile := flags.String("key-file", "", "where a created operator user's key goes")
	name := flags.String("name", "translate-gpu", nameHelp)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if (*user == "") != (*keyFile == "") {
		return errors.New("-user and -key-file go together")
	}
	profiles, err := aws.Profiles(*profilesFile)
	if err != nil {
		return err
	}
	ctx := interruptible()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}
	s := aws.NewSetup(cfg, aws.Names(*name), command.Split(*regions), aws.InstanceTypes(profiles), logLine)
	s.Account, s.User, s.KeyFile = *account, *user, *keyFile
	return s.Converge(ctx)
}

// interruptible is a context an interrupt or SIGTERM ends. Every later one is
// ignored: a teardown under way is not cut short.
func interruptible() context.Context {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		received := <-signals
		log.Printf("%v: ending", received)
		cancel()
		for range signals {
		}
	}()
	return ctx
}

func logLine(line string) { log.Println(line) }
