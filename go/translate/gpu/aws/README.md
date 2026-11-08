# greendrake.nz/go/translate/gpu/aws

Renders runs of the [translator](../../README.md), `greendrake.nz/go/translate`,
on a GPU rented in AWS for their length — the recommended way to render: an
open model served on an instance that exists, and bills, only for the minutes
a run takes.

```
translate-gpu -run translate.yaml [-run lib/translate.yaml,translate.yaml]... -profile gemma4-31b [-profiles profiles.yaml] [-parallel N] [-name translate-gpu] [-locale TAG] [-retranslate PREFIXES] [-verbose]
translate-gpu setup [-regions eu-north-1,us-west-2] [-profiles profiles.yaml] [-account ID] [-user NAME -key-file FILE] [-name translate-gpu]
```

A project with a Go module of its own runs it as a tool beside the translator
(`tool greendrake.nz/go/translate/gpu/aws/cmd/translate-gpu` in its `go.mod`,
then `go tool translate-gpu`); any other installs it with `go install
greendrake.nz/go/translate/gpu/aws/cmd/translate-gpu@latest`. The module is apart
from the translator's so that the AWS SDK stays out of a project's module
graph unless it rents.

## The flow

1. **Check.** Every `-run` — a manifest, or a
   [library's](../../README.md#library-manifests) as
   `<manifest>,<project manifest>` — is checked as the translator's `--check`
   checks it, `-locale` and `-retranslate` scoping it. With nothing due,
   nothing launches. With files alone behind — removals, reordering, the
   derived outputs — every run renders where the command runs, without a
   model.
2. **Launch.** With strings to render, one instance of the profile's type is
   launched wherever it costs least at the time. The regions are those setup
   prepared, read back from the operator policy that confines the operator to
   them, all looked at at once: in each, every zone offering the type that has
   a default subnet, spot at its price now where the region's spot quota holds
   one instance, and on-demand at the Pricing API's price where its on-demand
   quota does. A region that cannot be read, or a market whose price cannot,
   is said and passed over. They are tried cheapest first, moving on from a
   zone with no capacity or not giving the type on that market, and from a
   region's market whose quota other instances have spent, so a run falls
   through to the next zone, region and market by itself. A spot launch waits
   out an earlier run's spot request still
   holding that region's quota, which EC2 closes minutes after its instance
   is gone; an earlier run's instance still running anywhere refuses the run.
   The instance boots the current Deep Learning AMI (resolved from its public
   SSM parameter), carries the deployment's `Project` tag and instance role,
   requires IMDSv2, and terminates when it shuts down.
3. **Serve.** The boot script, embedded in the command, moves Docker's stores
   to the instance-store NVMe, pulls the profile's pinned vLLM image, and has
   vLLM download the profile's model at its pinned revision and serve it on the
   instance's loopback. Its console lines are relayed as they come.
4. **Tunnel.** Once the instance's SSM agent is online, an SSM port-forwarding
   session reaches vLLM — the instance has no inbound rule, no SSH key and no
   public endpoint — and the command waits for its `/health`. While the runs
   render it keeps looking at `/health` through the tunnel every 10 seconds:
   Session Manager ends a session that has had no input from the client for
   its idle timeout (20 minutes by default), and an answer can take longer
   than that with nothing sent meanwhile.
5. **Render.** Every run renders in turn, in-process, against the tunnel: the
   profile's model on both tiers with its sampling, answers decoded against
   the grammar the translator's schema compiles to, `-parallel` requests in
   flight (the profile's by default), each bounded by the profile's request
   timeout. Nothing of the manifest's `llm` block — its timeout, the provider
   block it names in the environment — reaches the rented model: those are
   sized for the provider the manifest renders against otherwise. A run that
   fails because the tunnel closed runs
   again over a new one, resuming from its last batch; any other failure ends
   the lease.
6. **Terminate.** On every way out — the runs done, an error, Ctrl-C or
   SIGTERM, a second of either ignored — the instance is terminated at once,
   skipping the OS's shutdown, and the command reports its minutes and their
   approximate cost.

## Prerequisites

- An AWS account in AWS's commercial partition — not GovCloud, the European
  Sovereign Cloud or China: the command calls IAM and STS, lists the regions
  and reads prices in us-east-1 — and credentials as the AWS SDK finds them:
  the environment, a shared profile, SSO. Runs act as an operator `setup` can
  create, whose policy allows a run and nothing else; `setup` acts as the
  account's administrator.
- The AWS CLI's Session Manager plugin, `session-manager-plugin`, newer than
  1.2.497.0: the tunnel runs through it, and nothing launches without it.
- GPU quotas, which start at 0 in every region: *All G and VT Spot Instance
  Requests* (`L-3819A6DF`) and *Running On-Demand G and VT instances*
  (`L-DB2E81BA`), in vCPU — 8 for one g7e.2xlarge. `setup` requests them; a
  run passes over a region on a market until AWS has granted its quota.
- Go, to build the command.

## Setup

`translate-gpu setup`, as the account's administrator, converges what a run
needs in the regions named — by default every region the account has enabled
that offers one of the profiles' instance types — creating only what is
missing:

- the operator policy `<name>-operator`: launch only the profiles' instance
  types, tagged `Project=<name>` and carrying the instance role; tunnel into,
  read the console of and terminate only instances so tagged; read what a
  launch reads, and the policy itself — one statement confines all of it to
  the regions, and a run reads them from there. A policy that drifted — a
  region or a profile's instance type added — gets a new default version, the
  oldest dropped at IAM's limit of five;
- the instance role and profile `<name>-instance`, registration with Systems
  Manager and nothing else, and EC2's spot service-linked role;
- in each region, a security group `<name>` in the default VPC, with no
  inbound rule, and both G and VT quotas raised to one instance of the
  largest type it offers, requested where short and not again while a request
  is open;
- with `-user`, that IAM user, its key written to `-key-file` when the user is
  created, and the policy attached to it;
- with `-account`, nothing in any other account.

It ends with each region's zones offering the profiles' types and its quotas.
A region that cannot be reached to see what it offers is left out with a
warning, and a run rents in the others; run setup again to have it. Run it
again too after enabling a region or adding a profile on a new instance type. `-name` (default
`translate-gpu`) names the AWS side; a run takes the same.

## Profiles

A profile is a model on an instance type: the type, the pinned vLLM image, the
model and its revision, the name it is served as, vLLM's arguments, the fields
merged into every request (the model's sampling), the requests in flight, how
long one request may take, and the instance's lifetimes. The built-ins:

| Profile | Model | Instance |
|---|---|---|
| `gemma4-31b` | `google/gemma-4-31B-it`, dense | g7e.2xlarge (1× RTX PRO 6000, 96 GB) |
| `gemma4-26b` | `google/gemma-4-26B-A4B-it`, mixture-of-experts | g7e.2xlarge |

Both run under `vllm/vllm-openai:v0.30.0` with a 65,536-token model length,
sample as Gemma's generation config says (temperature 1.0, top-p 0.95, top-k
64, min-p 0.1), keep 22 requests in flight and give one request an hour. A
request's timeout outlasts the time its answer takes to reach its token cap —
for a page of ~12k characters, ~36k tokens, which the dense 31B decodes at
under ~29 tokens a second alone on this card and slower among other requests
— so an answer that runs long, a model looping, ends at the cap with every
finished item kept rather than at the timeout with none.
An unknown profile is refused.

`-profiles FILE` adds profiles, or overrides a built-in whole, from a YAML
mapping of name to profile — every field stated, validated on load:

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

`setup` takes the same file, so the policy admits every profile's type.

## Spot or on-demand

Spot usually costs well under on-demand, so a run takes spot wherever any zone
has it and on-demand only where none does — or where spot happens to cost
more. With no capacity in any zone, spot or on-demand, the run ends saying so;
run it again later. A spot instance can be reclaimed mid-run: the lease ends,
saying so, and the same command resumes where the translator stopped.

## The instance's own deadlines

The instance ends itself — a forced power-off its launch turns into a
termination — whatever happens to the command, so a machine that dies mid-run
leaves nothing billing:

- when vLLM is not serving `boot_deadline_min` after boot (30);
- after `idle_min` with no request running, waiting or finishing (20) — which
  is also what follows a command that died, or a machine that slept;
- after `max_lifetime_min` whatever it is doing (360);
- at once if the boot script fails.

The instance's leaving the running state ends the lease, with its last console
lines above the error; the same command resumes.

## What a run costs

Billing runs from launch to termination, at the price the command reports at
launch: for g7e.2xlarge, about $1–4.5 an hour on spot and $3.4–5.9 on-demand,
by region and day. Every run first spends several minutes pulling the image (~10 GB)
and downloading the weights before its first request, so a handful of strings
still costs that much; batch work into one run where you can.
