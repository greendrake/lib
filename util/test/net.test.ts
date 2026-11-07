import { afterAll, describe, expect, test } from 'bun:test'
import { ApiResponseError, HttpError, NetworkError, admitsOrigin, bearerRequest, fetchJson, request, type Fetch } from '../src/net'
import { recordingFetch } from '../src/testing'

describe('request', () => {
    test('sends a json body serialized, with its content type', async () => {
        const { fetch, requests } = recordingFetch(() => Response.json({ ok: true }))
        expect(await request<{ ok: boolean }>(fetch, 'https://api.example/v1/things', { method: 'POST', json: { a: 1 } })).toEqual({ ok: true })
        expect(requests).toHaveLength(1)
        expect(requests[0]?.url).toBe('https://api.example/v1/things')
        expect(requests[0]?.method).toBe('POST')
        expect(requests[0]?.body).toBe('{"a":1}')
        expect(requests[0]?.headers.get('Content-Type')).toBe('application/json')
    })

    test('sends a raw body as it is, and answers an empty one as null', async () => {
        const body = new URLSearchParams({ a: '1' })
        const { fetch, requests } = recordingFetch(() => new Response(null, { status: 204 }))
        expect(await request(fetch, 'https://api.example/token', { method: 'POST', body })).toBeNull()
        expect(requests[0]?.body).toBe(body)
    })

    test.each([
        ['a JSON body, parsed', Response.json({ errors: [{ detail: 'no' }] }, { status: 409, statusText: 'Conflict' }), { errors: [{ detail: 'no' }] }],
        ['a +json body, parsed', new Response('{"title":"no"}', { status: 400, headers: { 'Content-Type': 'application/problem+json; charset=utf-8' } }), { title: 'no' }],
        ['a body that is not JSON, as its text', new Response('<html>Bad gateway</html>', { status: 502, headers: { 'Content-Type': 'text/html' } }), '<html>Bad gateway</html>'],
        ['no body, as null', new Response(null, { status: 503 }), null]
    ])('a failing status throws ApiResponseError carrying %s', async (_name, response, body) => {
        const { fetch } = recordingFetch(() => response)
        const error = await request(fetch, 'https://api.example/v1/apps?limit=200', { method: 'DELETE' }).catch((e: unknown) => e)
        expect(error).toBeInstanceOf(ApiResponseError)
        expect(error).toBeInstanceOf(HttpError)
        expect(error).toMatchObject({
            method: 'DELETE',
            path: '/v1/apps?limit=200',
            status: response.status,
            body
        })
    })

    test('names a request with no init a GET, and a relative URL by itself', async () => {
        const { fetch } = recordingFetch(() => Response.json({ error: { message: 'gone' } }, { status: 404, statusText: 'Not Found' }))
        const error = await request(fetch, '/api/me').catch((e: unknown) => e)
        expect(error).toMatchObject({
            method: 'GET',
            path: '/api/me',
            status: 404,
            statusText: 'Not Found',
            body: { error: { message: 'gone' } }
        })
        expect((error as Error).message).toStartWith('GET /api/me → 404\n')
    })

    test('a rejected fetch rejects with the same error, untouched', async () => {
        const failure = new TypeError('connection refused')
        const fetch: Fetch = () => Promise.reject(failure)
        await expect(request(fetch, 'https://api.example/')).rejects.toBe(failure)
    })
})

describe('bearerRequest', () => {
    test.each([
        ['minted on the spot', () => 'minted'],
        ['fetched', () => Promise.resolve('fetched')]
    ])('carries a token %s beside the headers given', async (_name, token) => {
        const { fetch, requests } = recordingFetch(() => Response.json([]))
        await bearerRequest(fetch, token)('https://api.example/', {
            method: 'POST',
            json: {},
            headers: { 'X-Extra': '1' }
        })
        const headers = requests[0]?.headers
        expect(headers?.get('Authorization')).toBe(`Bearer ${await token()}`)
        expect(headers?.get('X-Extra')).toBe('1')
        expect(headers?.get('Content-Type')).toBe('application/json')
    })

    test('asks for the token on every call', async () => {
        const { fetch, requests } = recordingFetch(() => Response.json([]))
        let minted = 0
        const call = bearerRequest(fetch, () => `token-${++minted}`)
        await call('https://api.example/')
        await call('https://api.example/')
        expect(requests.map(r => r.headers.get('Authorization'))).toEqual(['Bearer token-1', 'Bearer token-2'])
    })
})

describe('recordingFetch', () => {
    test('an answer that throws rejects the call, as a fetch that got no answer does', async () => {
        const failure = new TypeError('fetch failed')
        const { fetch, requests } = recordingFetch(() => {
            throw failure
        })
        await expect(request(fetch, 'https://api.example/')).rejects.toBe(failure)
        expect(requests.map(r => r.method)).toEqual(['GET'])
    })
})

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

    test('throws an ApiResponseError, an HttpError, with status and body on non-2xx', async () => {
        const error = await fetchJson(`http://localhost:${server.port}/missing`).catch((e: unknown) => e)
        expect(error).toBeInstanceOf(HttpError)
        expect(error).toBeInstanceOf(ApiResponseError)
        expect(error).toMatchObject({
            status: 404,
            path: '/missing',
            body: 'nope'
        })
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
