import { afterEach, describe, expect, test } from 'bun:test'
import { NetworkError } from '@greendrake/util'
import { stubGlobals } from '@greendrake/util/testing'
import { downloadBlob, Uploader } from '../src/main'

// An XMLHttpRequest as far as the package drives one, the global for this
// file's tests: each request sent is handed to `script`, which plays the
// server's part through the helpers. A request ends as a browser's does: DONE,
// with readystatechange first, then the event saying how — a request that got
// no answer is DONE with status 0.
class FakeXhr extends EventTarget {
    static readonly DONE = 4
    static script: (xhr: FakeXhr) => void
    static sent: FakeXhr[] = []

    readonly upload = new EventTarget()
    readonly headers: Record<string, string> = {}
    method = ''
    url = ''
    body: unknown
    responseType = ''
    response: unknown = null
    readyState = 1
    status = 0
    statusText = ''

    open(method: string, url: string): void {
        this.method = method
        this.url = url
    }

    setRequestHeader(name: string, value: string): void {
        this.headers[name] = value
    }

    send(body?: unknown): void {
        this.body = body
        FakeXhr.sent.push(this)
        queueMicrotask(() => FakeXhr.script(this))
    }

    abort(): void {
        this.#end('abort')
    }

    progress(loaded: number, total: number): void {
        const target = this.method === 'GET' ? this : this.upload
        target.dispatchEvent(
            Object.assign(new Event('progress'), {
                lengthComputable: true,
                loaded,
                total
            })
        )
    }

    answer(status: number, statusText = '', response: unknown = null): void {
        this.status = status
        this.statusText = statusText
        this.response = response
        this.#end('load')
    }

    fail(): void {
        this.#end('error')
    }

    #end(how: string): void {
        this.readyState = FakeXhr.DONE
        this.dispatchEvent(new Event('readystatechange'))
        this.dispatchEvent(new Event(how))
    }
}

stubGlobals(() => ({ XMLHttpRequest: FakeXhr }))

afterEach(() => {
    FakeXhr.sent = []
})

const file = new File(['hello'], 'hello.txt', { type: 'text/plain' })

const uploader = (options: Partial<ConstructorParameters<typeof Uploader>[0]> = {}): Uploader =>
    new Uploader({
        url: '/put',
        file,
        mime: 'text/plain',
        ...options
    })

describe('Uploader', () => {
    test('sends the file with its type, reports progress and finishes on a 2xx', async () => {
        FakeXhr.script = xhr => {
            xhr.progress(2, 5)
            xhr.progress(5, 5)
            xhr.answer(204)
        }
        const up = uploader({ verb: 'POST' })
        const events: string[] = []
        up.on('progress', p => events.push(`progress:${p}`))
        up.on('finished', () => events.push('finished'))
        await up.send()
        expect(events).toEqual(['progress:40', 'progress:100', 'finished'])
        const [xhr] = FakeXhr.sent
        expect([xhr.method, xhr.url, xhr.headers['Content-Type']]).toEqual(['POST', '/put', 'text/plain'])
        expect(await (xhr.body as Blob).text()).toBe('hello')
    })

    test('a request that gets no answer fails with a NetworkError, never finishing', async () => {
        FakeXhr.script = xhr => xhr.fail()
        const up = uploader({ shouldRetry: () => false })
        const events: string[] = []
        up.on('finished', () => events.push('finished'))
        await expect(up.send()).rejects.toBeInstanceOf(NetworkError)
        expect(events).toEqual([])
    })

    test('a status other than 2xx fails with its HttpError', async () => {
        FakeXhr.script = xhr => xhr.answer(500, 'Internal Server Error')
        await expect(uploader({ shouldRetry: () => false }).send()).rejects.toMatchObject({ name: 'HttpError', status: 500 })
    })

    test('by default, a 403 is not retried', async () => {
        FakeXhr.script = xhr => xhr.answer(403, 'Forbidden')
        await expect(uploader().send()).rejects.toMatchObject({ name: 'HttpError', status: 403 })
        expect(FakeXhr.sent).toHaveLength(1)
    })

    test('an abort cancels the upload', async () => {
        let sent!: () => void
        const sending = new Promise<void>(resolve => (sent = resolve))
        FakeXhr.script = () => sent()
        const up = uploader()
        const events: string[] = []
        up.on('cancelled', () => events.push('cancelled'))
        up.on('finished', () => events.push('finished'))
        const done = up.send()
        await sending
        up.abort()
        await done
        expect(events).toEqual(['cancelled'])
    })
})

describe('downloadBlob', () => {
    test('resolves with the response on a 2xx, reporting progress', async () => {
        const blob = new Blob(['bytes'])
        FakeXhr.script = xhr => {
            xhr.progress(1, 2)
            xhr.answer(200, 'OK', blob)
        }
        const progress: number[] = []
        expect(await downloadBlob('/get', p => progress.push(p))).toBe(blob)
        expect(progress).toEqual([50, 100])
        expect(FakeXhr.sent[0].responseType).toBe('blob')
    })

    test('a request that gets no answer fails with a NetworkError', async () => {
        FakeXhr.script = xhr => xhr.fail()
        await expect(downloadBlob('/get')).rejects.toBeInstanceOf(NetworkError)
    })

    test('a status other than 2xx fails with its HttpError', async () => {
        FakeXhr.script = xhr => xhr.answer(404, 'Not Found')
        await expect(downloadBlob('/get')).rejects.toMatchObject({ name: 'HttpError', status: 404 })
    })
})
