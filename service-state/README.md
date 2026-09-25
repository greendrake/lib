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

// Each machine is named for the wire; the registry — any EventSink, and
// @greendrake/rpc-server's ConnRegistry is one — is where its pushes go.
const machines = await Promise.all([createServiceStateMachine('web', unit('web.service'), registry), createServiceStateMachine('worker', unit('worker.service'), registry)])

const methods = { ...serviceStateMethods(machines), ...whateverElse }
```

The name is what a call's `{ service }` reaches the machine by and what its every status carries. The factory reads the service before returning, so the machine never reports a state it has not verified — one that assumed `OFF` at boot would have every dashboard showing `OFF` for a service that is up.

`start` and `stop` **return as soon as the transition has begun**, with the in-flight status. Where it ends up is the machine's next state, pushed to everyone — not the caller's answer, because everyone watching needs it equally and one of them happening to have asked changes nothing. A hook that rejects lands the machine in `ERROR` carrying its message.

One transition at a time, and each only from the state it is defined for. Anything else — `start` while already `ON`, `refresh` mid-transition — is refused with `SERVICE_BUSY`. A client showing the state never triggers it, because the state it is showing offers no such button.

`serviceStateMethods(machines, auth = 'admin')` returns the four `MethodDef`s over every machine given, so one dispatch table — and one socket — serves them all. A call's argument is checked for its shape before any machine is reached (`INVALID_ARGUMENT` on `service`); one naming a service the table does not serve is refused with `SERVICE_NOT_FOUND` — a `_NOT_FOUND` code, which clients following `@greendrake/rpc-server`'s convention treat as not found. Two machines under one name are refused as the table is built. Admin-only by default: turning a service off is not a read.
