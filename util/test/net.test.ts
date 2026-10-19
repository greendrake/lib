import { afterAll, describe, expect, test } from 'bun:test'
import { HttpError, NetworkError, admitsOrigin, fetchJson } from '../src/net'

const server = Bun.serve({
    port: 0,
    fetch(req) {
        const url = new URL(req.url)
        if (url.pathname === '/ok') {
            return Response.json({ hello: 'world' })
        }
        return new Response('nope', { status: 404, statusText: 'Not Found' })
    }
})

afterAll(() => server.stop(true))

describe('fetchJson', () => {
    test('returns parsed JSON on 2xx', async () => {
        const data = await fetchJson<{ hello: string }>(`http://localhost:${server.port}/ok`)
        expect(data.hello).toBe('world')
    })

    test('throws HttpError with status on non-2xx', async () => {
        try {
            await fetchJson(`http://localhost:${server.port}/missing`)
            expect.unreachable()
        } catch (e) {
            expect(e).toBeInstanceOf(HttpError)
            expect((e as HttpError).status).toBe(404)
        }
    })

    test('throws NetworkError on transport failure', async () => {
        await expect(fetchJson('http://127.0.0.1:1/')).rejects.toBeInstanceOf(NetworkError)
    })
})

describe('admitsOrigin', () => {
    test.each([
        ['any origin', '*', 'https://evil.example', true],
        ['a listed origin', ['https://app.example.com', 'app://localhost'], 'app://localhost', true],
        ['an unlisted one', ['https://app.example.com'], 'https://app.example.com.evil.example', false],
        ['one a predicate admits', (origin: string) => origin.endsWith('.example.com'), 'https://dash.example.com', true],
        ['one it does not', (origin: string) => origin.endsWith('.example.com'), 'https://example.org', false]
    ] as const)('%s', (_name, policy, origin, admitted) => {
        expect(admitsOrigin(policy, origin)).toBe(admitted)
    })
})
