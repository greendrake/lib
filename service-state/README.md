# @greendrake/service-state

Backend services an operator can turn on and off, each a state machine both halves of a dashboard share, any number of them over one socket. The root entry is the contract — the states, the status, the method and document names, the transition table — and is pure types and data with no runtime dependency, so the frontend importing it pulls in nothing. `./server` is the machine behind it.

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
import { serviceStateDocument, type ServiceDocument, type ServiceStateMethods, type ServiceStatePushes, type ServiceStatus } from '@greendrake/service-state'

type ServiceStatus =
    | { state: 'ERROR'; error: string; changed_at: number } // what went wrong, and since when (epoch ms)
    | { state: 'OFF' | 'STARTING' | 'ON' | 'STOPPING'; changed_at: number }

interface ServiceDocument {
    status?: ServiceStatus // absent until the service has been read with somebody watching it
}
```

Each service has a document of its own, `service.state/<name>` (`serviceStateDocument(name)`), kept by the sockets watching that service: a service has one state, and everyone watching it is watching the same thing. A socket that watches a service is sent its document whole as it starts, and a patch on every change after, all on one ordered stream, so a client keeps it with `@greendrake/rpc`'s `liveDocument` and never re-reads. `ServiceStatePushes` types every such document as a data push, so a client's transport carries them in its data slot: `WsTransport<Record<never, never>, ServiceStatePushes>`.

`ServiceStateMethods` is the five methods, each taking a `ServiceRef` — `{ service }`, the name of the service it is about — and answering nothing. Hand it to a typed client.

- **Watching.** `service.watch` has the caller's socket join the service's topic, of the document's name, and so keep its document; `service.unwatch` leaves. A service is read only while some socket watches it (see below). Both need a WebSocket (`WEBSOCKET_REQUIRED` otherwise), and a reconnected socket watches nothing until it watches again — which `@greendrake/dash`'s `keepServiceStates` does.
- **Commanding.** `service.start`, `service.stop` and `service.refresh`: where the service went is the document's to say.

`TRANSITIONS` is the machine's table: each command's `{ from, during, to }`. `CONTROLS` is what a control offers in each state — `OFF` → `start`, `ON` → `stop`, `ERROR` → `refresh`, and nothing (`null`) while `STARTING` or `STOPPING`, there being no interfering with a transition under way.

A call is refused with:

- `SERVICE_BUSY` — the state cannot take the command: a transition is under way, or the command would take the service where it already is. A control offers neither, so this is what a client that ignored the state gets told.
- `SERVICE_NOT_FOUND` — the call names a service the backend does not serve. A `_NOT_FOUND` code, which `@greendrake/rpc` makes a `NotFoundError`.

Both are exported as constants of those names.

## The machine

```ts
import { createServiceStateMachine, serviceStateBoard, serviceStateMethods } from '@greendrake/service-state/server'

const unit = (name: string) => ({
    start: () => systemd.start(name),
    stop: () => systemd.stop(name),
    // A unit found failed rejects with why, which the machine reads as ERROR.
    check: () => systemd.state(name).then(state => (state === 'failed' ? Promise.reject(new Error(`${name} failed`)) : state === 'active' ? 'ON' : 'OFF'))
})

// The board keeps each machine's document on a publisher —
// @greendrake/rpc-server's ConnRegistry is one — and is the one index of the
// machines by name.
const board = serviceStateBoard(registry)

// Each machine is named for the wire and registered on the board; the last
// argument is how old a reading may get before the machine takes another, in
// milliseconds.
const machines = [createServiceStateMachine('web', unit('web.service'), board, 30_000), createServiceStateMachine('worker', unit('worker.service'), board, 30_000)]

// Who may see the services and who may work them is the application's to say,
// in its own principals: here anybody signed in sees them, and admins alone
// work them.
const methods = {
    ...serviceStateMethods(board, {
        watch: { auth: 'required' },
        command: { auth: 'required', access: user => (user?.admin ? true : 'ADMIN_REQUIRED') }
    }),
    ...whateverElse
}

// On shutdown, stop the re-reads and the listening for watchers.
machines.forEach(machine => machine.close())
```

The name is what a call's `{ service }` reaches the machine by, and what its document and topic are named for. A board refuses a second machine of a name.

Nothing tells the machine when a service is changed by other hands — a unit stopped from a shell, a schedule that turns it off overnight — so it reads the service when somebody needs to know, and only then:

- **Watched.** While anybody watches the service — a socket in its topic — the machine re-reads whenever its latest reading turns `maxAgeMs` old, and publishes what changed, so a dashboard left open follows the service. The first to watch has the service read at once, and so does a machine created while somebody watches; the last to stop stops the re-reads. Watching one service reads nothing of the others on the board, however expensive their checks.
- **Commanded.** A `start` or `stop` on a machine whose latest reading is missing or `maxAgeMs` old reads first, and is refused or taken as that reading says, so a command with nobody watching never acts on a state the machine only assumed.

With nobody commanding and nobody watching, the service is left alone. A reading is published only when somebody watched it being taken, and the last to stop watching takes the reading out of the document: a dashboard that starts watching after a quiet spell shows its loading indicator until the service has been read, and is never shown what it was. The machine reports nothing before its first reading, so it never reports a state it has not verified.

A transition that lands counts as a reading, and so does a `refresh`; one asked while a read is under way joins it, unless a transition has begun since that read went out — which makes it a reading of the past, so a fresh one goes out instead. A transition under way, and the `ERROR` a failed one lands in, are the machine's own word rather than a reading: published whatever the audience, and not read past — that `ERROR` until somebody asks for a `refresh`, because reading past it would take the why away before anyone saw it. The `ERROR` of a check that failed is a reading like any other. A transition begun while a read is out goes ahead, and the read's answer, which describes the service before it, is dropped. `close()` stops the re-reads and the listening for watchers, for a process shutting down.

`check` must settle promptly: a command or a `refresh` can wait on it, and the next re-read is timed from when it settles, so a check that never settles leaves the state as it last stood.

`start` and `stop` **resolve as soon as the transition has begun**. Where it ends up is the document's to say, to everyone — not the caller's answer, because everyone watching needs it equally and one of them happening to have asked changes nothing. A hook that rejects lands the machine in `ERROR` carrying its message.

One transition at a time, and each only from the state it is defined for. Anything else — `start` while already `ON`, anything mid-transition — rejects with a `ServiceBusyError`.

`serviceStateMethods(board, { watch, command })` returns the five `MethodDef`s over every machine on the board, so one dispatch table — and one socket — serves them all. Each of `watch` and `command` is `@greendrake/rpc-server`'s `MethodAccess`, required, and only the application knows its principals:

- `watch` covers `service.watch` and `service.unwatch`. Seeing a service is a read; a host that lets every socket see the state, whoever it is bound as, opens it.
- `command` covers `service.start`, `service.stop` and `service.refresh`. Turning a service off is not a read.

A call's argument is checked for its shape before any machine is reached (`INVALID_ARGUMENT` on `service`); one naming a service the board does not have is refused with `SERVICE_NOT_FOUND`, and a `ServiceBusyError` is answered `SERVICE_BUSY`.
