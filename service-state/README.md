# @greendrake/service-state

Backend services an operator can turn on and off, each a state machine both halves of a dashboard share, any number of them over one socket. The root entry is the contract — the states, the status, the method and push names, the transition table — and is pure types and data with no runtime dependency, so the frontend importing it pulls in nothing. `./server` is the machine behind it.

```
OFF --start--> STARTING --> ON     ON --stop--> STOPPING --> OFF
                       \                                \
                        --> ERROR                        --> ERROR
```

`refresh` runs from any settled state and adopts what the service actually reports.

## Install

```sh
bun add @greendrake/service-state
```

The `./server` entry needs `@greendrake/rpc-server`, which is an optional peer dependency: a dashboard importing only the contract installs neither it nor bun's types.

## The contract

```ts
import { SERVICE_STATE_EVENT, type ServiceStateMethods, type ServiceStatePushes, type ServiceStatus } from '@greendrake/service-state'

interface ServiceStatus {
    service: string // which service this is the status of
    state: 'OFF' | 'STARTING' | 'ON' | 'STOPPING' | 'ERROR'
    error: string | null // what went wrong, in ERROR; null everywhere else
    changed_at: number // epoch ms
}
```

`ServiceStateMethods` is the four methods (`service.status`, `service.start`, `service.stop`, `service.refresh`), each taking a `ServiceRef` — `{ service }`, the name of the service it is about — and answering its `ServiceStatus`; hand it to a typed client. `ServiceStatePushes` types the `service.state` event, which carries a status to **every** connected socket: a service has one state, and everyone watching it is watching the same thing. The status names its service, so a socket watching several tells their pushes apart by it.

## The machine

```ts
import { createServiceStateMachine, serviceStateMethods } from '@greendrake/service-state/server'

const unit = (name: string) => ({
    start: () => systemd.start(name),
    stop: () => systemd.stop(name),
    check: () => systemd.isActive(name).then(on => (on ? 'ON' : 'OFF'))
})

// Each machine is named for the wire; the registry — any EventSink that is an
// Audience too, and @greendrake/rpc-server's ConnRegistry is both — is where
// its pushes go and who is there to hear them; and the last argument is how
// old a reading may get before the machine takes another, in milliseconds.
const machines = await Promise.all([createServiceStateMachine('web', unit('web.service'), registry, 30_000), createServiceStateMachine('worker', unit('worker.service'), registry, 30_000)])

const methods = { ...serviceStateMethods(machines), ...whateverElse }

// On shutdown, stop the re-reads and the listening for watchers.
machines.forEach(machine => machine.close())
```

The name is what a call's `{ service }` reaches the machine by and what its every status carries. The factory reads the service before returning, so the machine never reports a state it has not verified — one that assumed `OFF` at boot would have every dashboard showing `OFF` for a service that is up.

Nor does it keep reporting one it verified long ago, to anybody who needs to know. Nothing tells the machine when a service is changed by other hands — a unit stopped from a shell, a schedule that turns it off overnight — so it reads the service again when that is needed, and only then:

- **Asked.** `status` answers from the latest reading while it is younger than `maxAgeMs`; one that old is taken again first, and the answer is what it found. A dashboard opened after a quiet spell is shown what the service is, not what it was.
- **Watched.** While the sink's audience has anybody in it — a socket open to be pushed to — the machine re-reads whenever its latest reading turns `maxAgeMs` old, and pushes what changed, so a dashboard left open follows the service. The first to arrive has a stale reading taken again at once; the last to leave stops the re-reads.

With nobody asking and nobody watching, the service is left alone. A transition that lands counts as a reading, and so does a `refresh`; one asked while a read is under way joins it, unless a transition has begun since that read went out — which makes it a reading of the past, so a fresh one goes out instead. A transition under way and `ERROR` are the machine's own word rather than a reading, and are not read past — `ERROR` until somebody asks for a `refresh`, because reading past it would take the why away before anyone saw it. A transition begun while a read is out goes ahead, and the read's answer, which describes the service before it, is dropped. `close()` stops the re-reads and the listening for watchers, for a process shutting down.

`check` must settle promptly: a `status` or a `refresh` can wait on it, and the next re-read is timed from when it settles, so a check that never settles leaves the state as it last stood.

`start` and `stop` **return as soon as the transition has begun**, with the in-flight status. Where it ends up is the machine's next state, pushed to everyone — not the caller's answer, because everyone watching needs it equally and one of them happening to have asked changes nothing. A hook that rejects lands the machine in `ERROR` carrying its message.

One transition at a time, and each only from the state it is defined for. Anything else — `start` while already `ON`, `refresh` mid-transition — is refused with `SERVICE_BUSY`. A client showing the state never triggers it, because the state it is showing offers no such button.

`serviceStateMethods(machines, auth = 'admin')` returns the four `MethodDef`s over every machine given, so one dispatch table — and one socket — serves them all. A call's argument is checked for its shape before any machine is reached (`INVALID_ARGUMENT` on `service`); one naming a service the table does not serve is refused with `SERVICE_NOT_FOUND` — a `_NOT_FOUND` code, which clients following `@greendrake/rpc-server`'s convention treat as not found. Two machines under one name are refused as the table is built. Admin-only by default: turning a service off is not a read.
