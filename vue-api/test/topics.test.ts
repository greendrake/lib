import { afterEach, describe, expect, test } from 'bun:test'
import { nextTick } from 'vue'
import { ApiError, WebSocketClosedError, WebSocketPipe, WsTransport, type StateFrame } from '@greendrake/rpc'
import { holdTopics, keepTopicDocuments } from '../src/topics'
import { configureApiUX } from '../src/uxConfig'

interface Unit {
    state: string
}

type UnitType = `unit/${string}`

type DataPushes = Record<UnitType, StateFrame<Unit>>

// A transport whose socket the test opens, drops and feeds: nothing connects,
// and the pipe's events are emitted as a socket's would be.
const fed = (): { transport: WsTransport<Record<never, never>, DataPushes>; up: () => void; down: () => void; frame: (type: UnitType, data: unknown) => void } => {
    const pipe = new WebSocketPipe({ url: 'ws://localhost:1/' })
    return {
        transport: new WsTransport<Record<never, never>, DataPushes>({ pipe }),
        up: () => void pipe.emit('open'),
        down: () => void pipe.emit('close', new WebSocketClosedError(new CloseEvent('close', { code: 1006 }))),
        frame: (type, data) => void pipe.emit('message', JSON.stringify({ type, data }))
    }
}

interface Call {
    kind: 'join' | 'leave'
    key: string
    // Answers the call: with nothing, or with the error given.
    settle: (outcome?: Error) => void
}

// The joins and leaves the holder makes, each left out until the test settles
// it.
const recorded = (): { made: Call[]; kinds: () => string[]; calls: { join: (key: string) => Promise<void>; leave: (key: string) => Promise<void> } } => {
    const made: Call[] = []
    const call =
        (kind: Call['kind']) =>
        (key: string): Promise<void> =>
            new Promise((resolve, reject) =>
                made.push({
                    kind,
                    key,
                    settle: outcome => (outcome ? reject(outcome) : resolve())
                })
            )
    return {
        made,
        kinds: () => made.map(c => `${c.kind} ${c.key}`),
        calls: { join: call('join'), leave: call('leave') }
    }
}

// Past the render flush, with whatever that set going settled.
const flushed = async (): Promise<void> => {
    await nextTick()
    for (let tick = 0; tick < 10; tick++) {
        await Promise.resolve()
    }
}

const lost = (): WebSocketClosedError => new WebSocketClosedError(new CloseEvent('close', { code: 1006 })).asInFlight()

afterEach(() => configureApiUX({ errorSink: undefined }))

describe('holdTopics', () => {
    test('two holders join once, and leave once after both release', async () => {
        const { transport, up } = fed()
        up()
        const { made, kinds, calls } = recorded()
        const holder = holdTopics(transport, calls)
        const [first, second] = [holder.hold('web'), holder.hold('web')]
        await flushed()
        expect(kinds()).toEqual(['join web'])
        made[0].settle()
        first()
        await flushed()
        expect(kinds()).toEqual(['join web'])
        second()
        await flushed()
        expect(kinds()).toEqual(['join web', 'leave web'])
    })

    test('a release and a hold of the key in the same flush send nothing', async () => {
        const { transport, up } = fed()
        up()
        const { made, kinds, calls } = recorded()
        const holder = holdTopics(transport, calls)
        const release = holder.hold('web')
        await flushed()
        made[0].settle()
        release()
        holder.hold('web')
        await flushed()
        expect(kinds()).toEqual(['join web'])
    })

    test('it rejoins after a drop and return, but not on the first connect, nor while a join of the key is still out', async () => {
        const { transport, up, down } = fed()
        const { made, kinds, calls } = recorded()
        holdTopics(transport, calls).hold('web')
        await flushed()
        up()
        down()
        up()
        await flushed()
        expect(kinds()).toEqual(['join web'])
        made[0].settle()
        await flushed()
        down()
        up()
        await flushed()
        expect(kinds()).toEqual(['join web', 'join web'])
    })

    test('a join that fails with a NetworkError is retried at the next return', async () => {
        const { transport, up, down } = fed()
        up()
        const { made, kinds, calls } = recorded()
        const holder = holdTopics(transport, calls)
        holder.hold('web')
        await flushed()
        down()
        made[0].settle(lost())
        await flushed()
        expect(holder.failure('web')).toBeUndefined()
        up()
        await flushed()
        expect(kinds()).toEqual(['join web', 'join web'])
    })

    test("any other join failure is the key's failure until a join of it succeeds", async () => {
        const { transport, up, down } = fed()
        up()
        const { made, calls } = recorded()
        const holder = holdTopics(transport, calls)
        holder.hold('web')
        await flushed()
        const refused = new ApiError('ADMIN_REQUIRED')
        made[0].settle(refused)
        await flushed()
        expect(holder.failure('web')).toBe(refused)
        down()
        up()
        await flushed()
        expect(holder.failure('web')).toBe(refused)
        made[1].settle()
        await flushed()
        expect(holder.failure('web')).toBeUndefined()
    })

    test("a leave asked while its key's join is out goes only after that join settles, and is skipped if the transport is down by then", async () => {
        const { transport, up, down } = fed()
        up()
        const { made, kinds, calls } = recorded()
        const holder = holdTopics(transport, calls)
        holder.hold('web')()
        holder.hold('worker')()
        await flushed()
        expect(kinds()).toEqual(['join web', 'join worker'])
        made[0].settle()
        await flushed()
        expect(kinds()).toEqual(['join web', 'join worker', 'leave web'])
        // Granted, and then the socket went: nothing left to leave.
        made[1].settle()
        down()
        await flushed()
        expect(kinds()).toEqual(['join web', 'join worker', 'leave web'])
    })

    test("a hook that throws is reported, and the key's calls go on", async () => {
        const reported: Error[] = []
        configureApiUX({ errorSink: e => reported.push(e) })
        const { transport, up } = fed()
        up()
        const { made, kinds, calls } = recorded()
        const broken = new Error('the hook broke')
        const holder = holdTopics(transport, {
            ...calls,
            opens: () => {
                throw broken
            },
            closes: () => {
                throw broken
            }
        })
        holder.hold('web')()
        await flushed()
        made[0].settle()
        await flushed()
        made[1].settle()
        holder.hold('web')
        await flushed()
        expect(kinds()).toEqual(['join web', 'leave web', 'join web'])
        expect(reported).toEqual([broken, broken, broken])
    })

    test('a membership whose join was refused, or lost, has nothing to leave, and nothing is reported', async () => {
        const reported: Error[] = []
        configureApiUX({ errorSink: e => reported.push(e) })
        const { transport, up } = fed()
        up()
        const { made, kinds, calls } = recorded()
        const holder = holdTopics(transport, calls)
        holder.hold('db')()
        holder.hold('web')()
        await flushed()
        made[0].settle(new ApiError('SERVICE_NOT_FOUND'))
        made[1].settle(lost())
        await flushed()
        expect(kinds()).toEqual(['join db', 'join web'])
        expect(reported).toEqual([])
    })

    test('a hold while a leave is out joins only after the leave settles', async () => {
        const { transport, up } = fed()
        up()
        const { made, kinds, calls } = recorded()
        const holder = holdTopics(transport, calls)
        holder.hold('web')()
        await flushed()
        made[0].settle()
        await flushed()
        expect(kinds()).toEqual(['join web', 'leave web'])
        holder.hold('web')
        await flushed()
        expect(kinds()).toEqual(['join web', 'leave web'])
        made[1].settle()
        await flushed()
        expect(kinds()).toEqual(['join web', 'leave web', 'join web'])
    })

    test("a leave's NetworkError is dropped, and any other failure reaches the sink", async () => {
        const reported: Error[] = []
        configureApiUX({ errorSink: e => reported.push(e) })
        const { transport, up } = fed()
        up()
        const { made, calls } = recorded()
        const holder = holdTopics(transport, calls)
        holder.hold('web')()
        holder.hold('worker')()
        await flushed()
        made[0].settle()
        made[1].settle()
        await flushed()
        const refused = new ApiError('SERVICE_BUSY')
        made[2].settle(lost())
        made[3].settle(refused)
        await flushed()
        expect(reported).toEqual([refused])
    })
})

describe('keepTopicDocuments', () => {
    const keeping = (transport: WsTransport<Record<never, never>, DataPushes>, calls: { join: (key: string) => Promise<void>; leave: (key: string) => Promise<void> }) =>
        keepTopicDocuments(transport, {
            ...calls,
            typeOf: (key: string): UnitType => `unit/${key}`,
            empty: (): Unit => ({ state: 'unknown' })
        })

    test('a held key is kept from its greeting, and patched after', async () => {
        const { transport, up, frame } = fed()
        up()
        const { calls } = recorded()
        const units = keeping(transport, calls)
        units.hold('web')
        await flushed()
        expect(units.doc('web')).toEqual({ state: 'unknown' })
        frame('unit/web', { doc: { state: 'ON' } })
        expect(units.doc('web')).toEqual({ state: 'ON' })
        frame('unit/web', { patch: { state: 'OFF' } })
        expect(units.doc('web')).toEqual({ state: 'OFF' })
        expect(units.doc('worker')).toBeUndefined()
    })

    test('a release re-held within the flush keeps its document', async () => {
        const { transport, up, frame } = fed()
        up()
        const { calls } = recorded()
        const units = keeping(transport, calls)
        const release = units.hold('web')
        await flushed()
        frame('unit/web', { doc: { state: 'ON' } })
        release()
        units.hold('web')
        await flushed()
        frame('unit/web', { patch: { state: 'OFF' } })
        expect(units.doc('web')).toEqual({ state: 'OFF' })
    })

    test('after its membership ends a document is dropped and keeps nothing, and a re-hold shows nothing until greeted again', async () => {
        const { transport, up, frame } = fed()
        up()
        const { made, calls } = recorded()
        const units = keeping(transport, calls)
        units.hold('web')()
        await flushed()
        frame('unit/web', { doc: { state: 'ON' } })
        made[0].settle()
        await flushed()
        expect(units.doc('web')).toBeUndefined()
        // A patch sent before the leave was seen reaches nothing.
        frame('unit/web', { patch: { state: 'OFF' } })

        units.hold('web')
        made[1].settle()
        await flushed()
        expect(units.doc('web')).toEqual({ state: 'unknown' })
        frame('unit/web', { doc: { state: 'STARTING' } })
        expect(units.doc('web')).toEqual({ state: 'STARTING' })
    })

    test('while a rejoin stands refused the document shows nothing, until a join is granted and greeted', async () => {
        const { transport, up, down, frame } = fed()
        up()
        const { made, calls } = recorded()
        const units = keeping(transport, calls)
        units.hold('web')
        await flushed()
        frame('unit/web', { doc: { state: 'ON' } })
        made[0].settle()
        await flushed()

        down()
        up()
        await flushed()
        made[1].settle(new ApiError('ADMIN_REQUIRED'))
        await flushed()
        expect(units.doc('web')).toBeUndefined()

        down()
        up()
        await flushed()
        // The greeting comes ahead of the answer granting the join.
        frame('unit/web', { doc: { state: 'OFF' } })
        made[2].settle()
        await flushed()
        expect(units.doc('web')).toEqual({ state: 'OFF' })
    })
})
