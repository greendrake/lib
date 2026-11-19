# @greendrake/service-state

For a dashboard on which an operator turns backend services on and off. The backend keeps each service as a state machine, and every dashboard watching a service is shown the same state as it changes, whoever caused the change. Any number of services share one socket.

```sh
bun add @greendrake/service-state
```

## Why

An operations dashboard has a switch for a service: a systemd unit, a container, a worker. The obvious way to build the switch is a button that calls `start`, waits for the answer, and shows it. Then only the caller learns the outcome: a second operator's dashboard goes on showing the service off. Nothing tells any dashboard when the service is changed from elsewhere, such as a shell or a schedule. Polling for that is costly: a timer runs the check whether or not anyone is looking.

This package makes the state something the backend keeps and every dashboard watches. Each service is one state machine:

```
OFF --start--> STARTING --> ON     ON --stop--> STOPPING --> OFF
                       \                                \
                        --> ERROR                        --> ERROR
```

`refresh` runs from `OFF`, `ON` and `ERROR`, and adopts what the service actually reports.

## Trade-offs

- **Start and stop, and nothing else.** The transition table is fixed: `start` from `OFF`, and `stop` from `ON`. There is no restart, no reload, and no state of the application's own. A transition under way cannot be cancelled, and every command is refused until it ends. A service with more to it than on and off needs a state machine of its own.
- **Outside changes are found by polling.** Nothing tells the machine that a service changed. While a dashboard watches, the machine reads the service again, through the application's `check` function, whenever its latest reading turns `maxAgeMs` old. So a watched dashboard can be behind the service by up to `maxAgeMs`, plus the time `check` takes. A shorter `maxAgeMs` means more calls to `check`. An application that does hear of a change can call the machine's `refresh()`.
- **A machine lives in one process's memory.** Nothing is stored: not the state, not a transition under way, not the reason for an `ERROR`. After a restart the machine knows what `check` reports, and nothing more. Two processes that each run a machine for one service do not see each other's transitions. There is also no history of transitions, and no record of who commanded one.
- **One RPC stack.** The contract is written in the `@greendrake/rpc` wire format: its methods, and its documents. A document is state that a server keeps current on its clients. `./server` is written against `@greendrake/rpc-server`.

## Overview

- **One state, sent to everyone.** Each service has a document, kept current on every socket that watches the service. A command returns nothing. The outcome is in the document, for the caller and for everyone else.
- **A transition is a state.** `STARTING` and `STOPPING` are published as they begin. A command that the state cannot take is refused with `SERVICE_BUSY`, so two operators cannot start one service twice.
- **The service is read only when somebody needs to know.** While a socket watches a service, the machine re-reads it whenever the latest reading turns `maxAgeMs` old. A `start` or `stop` acts on a reading younger than that, and takes one first where it has none. With nobody watching and nobody commanding, the service is left alone.
- **Nothing unverified is reported.** The machine reports no state before its first reading. A failed transition stays in `ERROR`, with the reason, until somebody asks for a `refresh`.
- **One contract for both ends.** The root entry declares the states, the method names and the transition table, as types and data. The dashboard and the backend build against the same declarations.

## Entries

The root entry is the contract: the states, the status, the method and document names, and the transition table. It is types, data and one pure function, `serviceStateDocument`, with no runtime dependency, so a frontend that imports it pulls in nothing. `./server` is the machine behind it.

The `./server` entry needs `@greendrake/rpc-server`. That is an optional peer dependency, so a dashboard that imports only the contract installs neither it nor bun's types.

Both entries ship as TypeScript with no build step, so a frontend that imports the contract needs a bundler that compiles TypeScript, as Vite does.

## The contract

```ts
import { serviceStateDocument, type ServiceDocument, type ServiceStateMethods, type ServiceStatePushes, type ServiceStatus } from '@greendrake/service-state'

type ServiceStatus =
    | { state: 'ERROR'; error: string; changed_at: number } // what went wrong, and since when (epoch ms)
    | { state: 'OFF' | 'STARTING' | 'ON' | 'STOPPING'; changed_at: number }

interface ServiceDocument {
    status?: ServiceStatus // absent until the service has been read, and once its latest reading turns maxAgeMs old with nobody watching — unless a transition is under way, or a failed one's ERROR stands
}
```

Each service has a document of its own, `service.state/<name>` (`serviceStateDocument(name)`). It goes to the sockets watching that service: a service has one state, and everyone watching it sees the same thing. A socket that watches a service is sent the document whole as it starts watching, and a patch on every change after that, all in order on one stream. So a client holds the document with `@greendrake/rpc`'s `liveDocument`, and never re-reads. `ServiceStatePushes` types every such document as a data push, so it is the second type argument of a client's transport: `WsTransport<Record<never, never>, ServiceStatePushes>`.

`ServiceStateMethods` is the five methods. Each takes a `ServiceRef`, `{ service }`, which names the service the call is about. Each returns nothing. Hand the type to a typed client.

- **Watching.** `service.watch` has the caller's socket join the service's topic, and so be sent its document. A topic is a named group of sockets in `@greendrake/rpc-server`, and this one has the document's name. `service.unwatch` leaves the topic. A service is polled only while some socket watches it (see below). Both methods need a WebSocket, and are refused with `WEBSOCKET_REQUIRED` otherwise. A reconnected socket watches nothing until its client calls `service.watch` again.
- **Commanding.** `service.start`, `service.stop` and `service.refresh`. No method returns a service's status: the document carries it, to the sockets that watch. So a caller over plain HTTP can command a service, and cannot see the outcome.

`TRANSITIONS` is the machine's table: each command's `{ from, during, to }`. `CONTROLS` is the command that a dashboard's control offers in each state: `OFF` → `start`, `ON` → `stop`, `ERROR` → `refresh`. It offers nothing (`null`) while `STARTING` or `STOPPING`, because a transition under way cannot be interrupted.

A call is refused with:

- `SERVICE_BUSY` — the state cannot take the command: a transition is under way, or the command would take the service to the state it is already in. A control offers neither, so only a client that ignored the state gets this code.
- `SERVICE_NOT_FOUND` — the call names a service the backend does not serve. It is a `_NOT_FOUND` code, which `@greendrake/rpc` makes a `NotFoundError`.

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

// Each machine gets the name that calls use for it, and is registered on the
// board. The last argument is how old a reading may get before the machine
// takes another, in milliseconds.
const machines = [createServiceStateMachine('web', unit('web.service'), board, 30_000), createServiceStateMachine('worker', unit('worker.service'), board, 30_000)]

// The application decides who may see the services and who may start and stop
// them, in terms of its own principals. Here anybody signed in sees them, and
// only admins start and stop them.
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

A board is the index of a backend's machines by name, and it publishes each machine's document. A call's `{ service }` is matched to a machine by its name, and the machine's document and topic are named after it. A board refuses a second machine of the same name.

Nothing tells the machine when a service is changed from outside it: a unit stopped from a shell, say, or a schedule that turns it off overnight. So the machine reads the service when somebody needs to know, and only then:

- **Watched.** A service is watched while any socket is in its topic. While it is watched, the machine re-reads it whenever its latest reading turns `maxAgeMs` old, and publishes what changed. So a dashboard left open follows the service. What the first watcher is sent depends on the document. Where the document holds a reading younger than `maxAgeMs`, the watcher is sent it, and the next re-read is timed from when that reading was taken. Where the document holds none, the service is read at once. A machine created while somebody watches reads at once. When the last watcher stops, the re-reads stop. Watching one service causes no read of the others on the board, however expensive their checks.
- **Commanded.** When a machine's latest reading is missing or `maxAgeMs` old, a `start` or `stop` reads the service first, and is then refused or accepted as that reading says. So a command given with nobody watching never acts on a state the machine only assumed.

With nobody commanding and nobody watching, the service is left alone. The document holds the latest reading while anybody watches the service. Once nobody does, it holds the reading until the reading turns `maxAgeMs` old. A reading taken with nobody watching, by a command or a `refresh`, is kept the same way. So a dashboard that is hidden and shown again before the reading ages is sent the service's state at once. A dashboard that starts watching later than that shows its loading indicator until the service has been read. Either way, the first to watch is never sent a reading older than `maxAgeMs`. The machine reports nothing before its first reading, so it never reports a state it has not verified.

A transition that completes counts as a reading, and so does a `refresh`. A `refresh` asked for while a read is under way waits for that read, and starts no other. The exception is a read that started before a transition began: it describes the service as it was before, so a new read starts instead. A transition under way, and the `ERROR` that a failed transition ends in, are states the machine itself set, not readings. They are published whether or not anybody watches, and the machine starts no read of its own while either stands. That `ERROR` stays until somebody asks for a `refresh`, because a new reading would remove the reason before anyone saw it. The `ERROR` of a check that failed is a reading like any other. A transition begun while a read is under way goes ahead. The read's result describes the service before the transition, and is dropped. `close()` stops the re-reads and the listening for watchers, for a process that is shutting down.

The machine sets no deadline on `start`, `stop` or `check`, the three functions that an implementation provides. `check` must settle promptly. A command or a `refresh` can wait on it, and the next re-read is timed from when it settles. So a check that never settles leaves the state as it last stood. A `start` or `stop` that never settles leaves the service in `STARTING` or `STOPPING`, with every command refused, `refresh` included. An implementation has to put a time limit of its own on each.

The machine's `start` and `stop` **resolve as soon as the transition has begun**. How the transition ends is in the document, for everyone. It is not returned to the caller: everyone watching needs it equally, whichever of them asked. When one of the implementation's functions rejects, the machine goes to `ERROR`, with the rejection's message.

There is one transition at a time, and each starts only from the state it is defined for. Anything else rejects with a `ServiceBusyError`: `start` while already `ON`, or any command mid-transition.

`serviceStateMethods(board, { watch, command })` returns the five `MethodDef`s over every machine on the board, so one dispatch table, and one socket, serves them all. `watch` and `command` are each a `MethodAccess` of `@greendrake/rpc-server`. Both are required, because only the application knows its principals:

- `watch` covers `service.watch` and `service.unwatch`. Seeing a service is a read. An application that lets every socket see the state, anonymous ones included, gives `watch` the access `{ auth: 'none' }`.
- `command` covers `service.start`, `service.stop` and `service.refresh`. Turning a service off is not a read.

A call's argument is checked for its shape before any machine is reached, and a bad one is answered `INVALID_ARGUMENT`, naming the field `service`. A call that names a service the board does not have is refused with `SERVICE_NOT_FOUND`. A `ServiceBusyError` is answered `SERVICE_BUSY`.
