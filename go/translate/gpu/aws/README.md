# greendrake.nz/go/translate/gpu/aws

Runs the [translator](../../README.md), `greendrake.nz/go/translate`, against
an open model on a GPU rented in AWS for the length of the runs. It is the
recommended way to run it: the model is served on an instance that exists, and
bills, only for the minutes a run takes.

```sh
go install greendrake.nz/go/translate/gpu/aws/cmd/translate-gpu@latest
```

## Why

The translator needs a model only when strings have changed. Serving an open
model takes a GPU large enough to hold it, and a team without one rents it.
But a rented GPU bills for as long as it runs: left running between runs of
the translator, it bills for the days it does nothing. Starting one each time
is a procedure: find a zone that has the instance type at a price worth
paying, launch it, download the model, and wait until it answers. And the
model has to be reached without opening a port to everyone.

This command does all of that for the length of the runs. It launches an
instance only when strings need a model, where that costs least, reaches the
model without an open port, and always terminates the instance.

## Trade-offs

- **Every launch pays for the boot.** Nothing is kept between launches. Each
  instance pulls the image and downloads the weights before its first request,
  which takes several minutes. A handful of strings costs that much too. For a
  small job, running the translator against a provider's API has no such wait.
- **The command is the client, and has to stay up.** The translator runs in
  the command's own process, and reaches the model through a forwarded port.
  The machine running the command has to stay awake and connected until the
  runs finish. If it does not, the instance shuts itself down at its idle
  limit, and the command has to be run again.
- **Both tiers are one model.** The translator's fast and strong tiers both
  get the one model that the instance serves. So its retry at the strong tier
  is asked of the same model whose answer was rejected.

## Overview

- **Nothing launches without need.** Every run is checked first. An instance
  is launched only when strings need a model.
- **It rents where it costs least.** In the regions that `translate-gpu setup`
  prepared, every zone that offers the instance type is priced, spot and
  on-demand. They are tried cheapest first, and a zone with no capacity is
  skipped.
- **The instance has no way in.** It has no inbound rule and no SSH key. The
  model listens on the instance's loopback, and the command reaches it through
  a Session Manager port-forwarding session.
- **The instance always ends.** The command terminates it, however the command
  ends. The instance also shuts itself down: when it fails to start serving,
  when it has been idle, and at a limit on its lifetime. So a command that
  dies leaves no instance billing.
- **An interrupted run resumes.** The translator writes every batch as it
  finishes. After a reclaimed spot instance, or a command that died, the same
  command continues from there.
- **The model is pinned.** A model on an instance type is a *profile*. The
  profile names the model by revision, and the vLLM image by version.

## The command

```
translate-gpu -run translate.yaml [-run lib/translate.yaml,translate.yaml]... -profile gemma4-31b [-profiles profiles.yaml] [-parallel N] [-name translate-gpu] [-locale TAG] [-retranslate PREFIXES] [-verbose]
translate-gpu setup [-regions eu-north-1,us-west-2] [-profiles profiles.yaml] [-account ID] [-user NAME -key-file FILE] [-name translate-gpu]
```

A project with a Go module of its own runs it as a tool beside the translator:
`tool greendrake.nz/go/translate/gpu/aws/cmd/translate-gpu` in its `go.mod`,
then `go tool translate-gpu`. Any other project installs it with `go install
greendrake.nz/go/translate/gpu/aws/cmd/translate-gpu@latest`. The module is
separate from the translator's, so that the AWS SDK stays out of a project's
module graph unless the project uses this command.

## The flow

1. **Check.** Every `-run` is checked as the translator's `--check` checks it,
   within what `-locale` and `-retranslate` select. A `-run` is a manifest, or
   a [library's](../../README.md#library-manifests) as
   `<manifest>,<project manifest>`. With nothing to do, nothing launches. When
   only files are out of date — removals, reordering, the derived outputs —
   every run rewrites them on the machine the command runs on, without a
   model.
2. **Launch.** With strings to translate, one instance of the profile's type
   is launched wherever it costs least at the time, on one of two *markets*:
   spot or on-demand. A run acts as the *operator*: an AWS identity whose
   policy allows a run and nothing else. `setup` writes that policy. The
   regions are those that `setup` prepared. They are read back from the
   policy, which confines the operator to them, and are all looked at at once.
   In each region, the zones looked at are those that offer the type and have
   a default subnet. Such a zone is an option on spot, at its price now, where
   the region's spot quota allows one instance. It is an option on-demand, at
   the Pricing API's price, where the region's on-demand quota does. A region
   that cannot be read is reported and skipped. So is a market whose price
   cannot be read. The options are tried cheapest first. A launch moves on
   from a zone that has no capacity, or that does not give the type on that
   market. It also moves on from a region's market whose quota other instances
   have used up. So a run moves on to the next zone, region and market by
   itself. An earlier run's spot request may still count against a region's
   quota: EC2 closes it minutes after its instance is gone. A spot launch
   waits for that. There is one instance at a time under a name: a new run is
   refused while an instance of the same `-name` is still pending or running,
   in any region that `setup` prepared. The instance launches into the zone's
   default subnet, under the security group that `setup` created in the
   default VPC. It boots the current x86-64 Deep Learning AMI (resolved from
   its public SSM parameter). It has the tag `Project=<name>` and the instance
   role that `setup` created, requires IMDSv2, and terminates when it shuts
   down.
3. **Serve.** The boot script is embedded in the command. It moves Docker's
   storage to the instance-store NVMe, which it requires for the image and the
   weights. It pulls the profile's pinned vLLM image. It then has vLLM
   download the profile's model at its pinned revision and serve it on the
   instance's loopback. The command prints the script's console lines as they
   come.
4. **Tunnel.** Once the instance's SSM agent is online, the command opens an
   SSM port-forwarding session to vLLM: the tunnel. It then waits for vLLM's
   `/health` to answer. The instance has no inbound rule, no SSH key and no
   public endpoint. While the runs are in progress, the command keeps
   requesting `/health` through the tunnel every 10 seconds. Session Manager
   ends a session that has had no input from the client for its idle timeout
   (20 minutes by default), and an answer can take longer than that with
   nothing sent meanwhile.
5. **Translate.** The runs are translated one after another, in the command's
   own process, through the tunnel. Both tiers get the profile's model, with
   its sampling settings. Answers are constrained to the grammar that the
   translator's schema compiles to. `-parallel` requests are in flight (the
   profile's number by default), each bounded by the profile's request
   timeout. Nothing of the manifest's `llm` block applies to the rented model:
   neither its timeout, nor the provider settings that it names in the
   environment. Those are sized for the provider that the manifest is
   translated with otherwise. A run that fails because the tunnel closed runs
   again over a new tunnel, and resumes from its last batch. Any other failure
   ends the command, which terminates the instance.
6. **Terminate.** However the command ends, the instance is terminated at
   once, skipping the OS's shutdown: with the runs done, on an error, on
   Ctrl-C or on SIGTERM. A second Ctrl-C or SIGTERM is ignored. The command
   then reports how many minutes the instance ran, and their approximate cost.

## Prerequisites

- An AWS account in AWS's commercial partition, not GovCloud, the European
  Sovereign Cloud or China: the command calls IAM and STS, lists the regions
  and reads prices in us-east-1. Credentials are taken as the AWS SDK finds
  them: the environment, a shared profile, SSO. Runs act as the operator,
  which `setup` can create. `setup` acts as the account's administrator.
- The AWS CLI's Session Manager plugin, `session-manager-plugin`, newer than
  1.2.497.0: the tunnel runs through it, and nothing launches without it.
- GPU quotas, which start at 0 in every region: *All G and VT Spot Instance
  Requests* (`L-3819A6DF`) and *Running On-Demand G and VT instances*
  (`L-DB2E81BA`), in vCPU — 8 for one g7e.2xlarge. `setup` requests them.
  Until AWS has granted a region's quota for a market, a run skips that region
  on that market.
- Go, to build the command.

## Setup

`translate-gpu setup`, run as the account's administrator, puts in place what
a run needs in the regions named, and creates only what is missing. By default
the regions are every region the account has enabled that offers one of the
profiles' instance types. What it puts in place:

- the operator policy `<name>-operator`. It allows launching only the
  profiles' instance types, tagged `Project=<name>` and given the instance
  role. It allows opening a tunnel into, reading the console of and
  terminating only instances with that tag. It allows reading what a launch
  reads, and the policy itself. One statement confines all of it to the
  regions, and a run reads the regions from there. A policy that differs from
  what `setup` would write, with a region or a profile's instance type added
  say, gets a new default version. The oldest version is dropped at IAM's
  limit of five;
- the instance role and profile `<name>-instance`, which allow registration
  with Systems Manager and nothing else, and EC2's spot service-linked role;
- in each region, a security group `<name>` in the default VPC, with no
  inbound rule, and both G and VT quotas raised to one instance of the largest
  type the region offers. A raise is requested where a quota is short, and not
  again while a request is open;
- with `-user`, that IAM user, its key written to `-key-file` when the user is
  created, and the policy attached to it;
- with `-account`, nothing in any other account.

It ends by reporting, for each region, the zones that offer the profiles'
types, and the region's quotas. A region that cannot be reached to see what it
offers is left out with a warning, and a run rents in the others. Run setup
again to have that region. Run it again too after enabling a region or adding
a profile on a new instance type. `-name` (default `translate-gpu`) is the
`<name>` that all of the above is created under, and a run takes the same
name.

## Profiles

A profile is a model on an instance type: the type, the pinned vLLM image, the
model and its revision, the name it is served as, vLLM's arguments, the fields
merged into every request (the model's sampling settings), the number of
requests in flight, how long one request may take, and the instance's time
limits. The built-ins:

| Profile | Model | Instance |
|---|---|---|
| `gemma4-31b` | `google/gemma-4-31B-it`, dense | g7e.2xlarge (1× RTX PRO 6000, 96 GB) |
| `gemma4-26b` | `google/gemma-4-26B-A4B-it`, mixture-of-experts | g7e.2xlarge |

Both run under `vllm/vllm-openai:v0.30.0` with a 65,536-token model length,
sample as Gemma's generation config says (temperature 1.0, top-p 0.95, top-k
64, min-p 0.1), keep 22 requests in flight and give one request an hour. A
request's timeout is longer than the time its answer takes to reach its token
cap. For a page of ~12k characters the cap is ~36k tokens. The dense 31B
generates under ~29 tokens a second when alone on this card, and fewer among
other requests. So an answer that runs long, as when a model repeats itself
without end, stops at the cap with every finished item kept, rather than at
the timeout with none. An unknown profile is refused.

`-profiles FILE` adds profiles, or replaces a built-in entirely, from a YAML
mapping of name to profile. Every field has to be stated, and the file is
validated on load:

```yaml
gemma4-31b-overnight:                     # the built-in, living longer
  instance_type: g7e.2xlarge
  image: vllm/vllm-openai:v0.30.0
  model: google/gemma-4-31B-it
  revision: 842da3794eaa0b77d5f08bae87a17459d91ff475
  served_name: gemma-4-31b
  server_args: [--max-model-len, "65536", --gpu-memory-utilization, "0.92"]
  request_extra: {temperature: 1.0, top_p: 0.95, top_k: 64, min_p: 0.1}
  parallel: 22
  request_timeout_min: 60
  max_lifetime_min: 720
  idle_min: 20
  boot_deadline_min: 30
```

`setup` takes the same file, so that the policy allows every profile's
instance type.

A profile of your own has to fit how the instance is launched. It boots the
x86-64 Deep Learning AMI, and its boot script requires instance-store NVMe for
the image and the weights.

## Spot or on-demand

Spot usually costs well under on-demand. So a run takes spot wherever any zone
has it, and on-demand only where none does, or where spot happens to cost
more. With no capacity in any zone, spot or on-demand, the run ends saying so:
run it again later. A spot instance can be reclaimed mid-run. The command then
ends, saying so. Run again, the same command resumes where the translator
stopped.

## The instance's own deadlines

The instance shuts itself down, whatever happens to the command, so a machine
that dies mid-run leaves no instance billing. It does so by a forced
power-off, and it is launched so that a shutdown terminates it. It powers off:

- when vLLM is not serving `boot_deadline_min` after boot (30);
- after `idle_min` with no request running, waiting or finishing (20) — which
  is also what follows a command that died, or a machine that slept;
- after `max_lifetime_min` whatever it is doing (360);
- at once if the boot script fails.

When the instance leaves the running state, the command ends with an error,
below the instance's last console lines. Run again, the same command resumes.

## What a run costs

Billing runs from launch to termination, at the price the command reports at
launch: for g7e.2xlarge, about $1–4.5 an hour on spot and $3.4–5.9 on-demand,
by region and day. Every run first spends several minutes pulling the image
(~10 GB) and downloading the weights, before its first request. So a handful
of strings still costs that much: put work into one run where you can.
