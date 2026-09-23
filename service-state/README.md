# @greendrake/service-state

A backend service an operator can turn on and off, as a state machine both halves of a dashboard share. The root entry is the contract — the states, the status, the method and push names, the transition table — and is pure types and data with no runtime dependency, so the frontend importing it pulls in nothing. `./server` is the machine behind it.

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
    state: 'OFF' | 'STARTING' | 'ON' | 'STOPPING' | 'ERROR'
    error: string | null // what went wrong, in ERROR; null everywhere else
    changed_at: number // epoch ms
}
```

`ServiceStateMethods` is the four methods (`service.status`, `service.start`, `service.stop`, `service.refresh`), each answering a `ServiceStatus` — hand it to an `ApiClient`. `ServiceStatePushes` types the `service.state` event, which carries the same status to **every** connected socket: a service has one state, and everyone watching it is watching the same thing.

`@greendrake/dash`'s `ServiceStateControl` renders all of this; a dashboard that wants the control needs no code of its own beyond wiring it to its client and transport.

## The machine

```ts
import { createServiceStateMachine, serviceStateMethods } from '@greendrake/service-state/server'

const machine = await createServiceStateMachine(
    {
        start: () => systemd.start('thing.service'),
        stop: () => systemd.stop('thing.service'),
        check: () => systemd.isActive('thing.service').then(on => (on ? 'ON' : 'OFF'))
    },
    registry // any EventSink — @greendrake/rpc-server's ConnRegistry is one
)

const methods = { ...serviceStateMethods(machine), ...whateverElse }
```

The factory reads the service before returning, so the machine never reports a state it has not verified — one that assumed `OFF` at boot would have every dashboard showing `OFF` for a service that is up.

`start` and `stop` **return as soon as the transition has begun**, with the in-flight status. Where it ends up is the machine's next state, pushed to everyone — not the caller's answer, because everyone watching needs it equally and one of them happening to have asked changes nothing. A hook that rejects lands the machine in `ERROR` carrying its message.

One transition at a time, and each only from the state it is defined for. Anything else — `start` while already `ON`, `refresh` mid-transition — is refused with `SERVICE_BUSY`. A client showing the state never triggers it, because the state it is showing offers no such button.

`serviceStateMethods(machine, auth = 'admin')` returns the four `MethodDef`s. Admin-only by default: turning a service off is not a read.
