import { afterAll, describe, expect, test } from 'bun:test'
import { NetworkError } from '@greendrake/util'
import { ApiError, HttpTransport, RpcClient, NotFoundError } from '../src/main'
import type { Codec, RpcRequest } from '../src/main'

interface Methods {
    echo: (payload: Record<string, unknown>) => { echoed: Record<string, unknown>; sawHeader: string | null }
    'thing.get': (id: string) => never
    forbidden: () => never
}

const plainServer = Bun.serve({
    port: 0,
    async fetch(req) {
        const request = (await req.json()) as RpcRequest
        switch (request.method) {
            case 'echo':
                return Response.json({
                    success: true,
                    data: { echoed: request.arguments![0], sawHeader: req.headers.get('x-test') }
                })
            case 'thing.get':
                return Response.json({ success: false, error: 'THING_NOT_FOUND' })
            default:
                return Response.json({
                    success: false,
                    error: 'FORBIDDEN',
                    error_details: { reason: 'test' }
                })
        }
    }
})

// Any reversible transform proves the codec plumbing; base64 never yields a
// leading brace, so the server below can tell an encoded body from plain JSON.
const codec: Codec = {
    encode: text => Buffer.from(text).toString('base64'),
    decode: text => Buffer.from(text, 'base64').toString()
}
const encodedServer = Bun.serve({
    port: 0,
    async fetch(req) {
        // Reject plain payloads: proves the client really encodes requests.
        const raw = await req.text()
        if (raw.trim().startsWith('{')) {
            return Response.json({ success: false, error: 'PLAIN_REQUEST_REJECTED' })
        }
        const request = JSON.parse(codec.decode(raw)) as RpcRequest
        return new Response(codec.encode(JSON.stringify({ success: true, data: { method: request.method } })))
    }
})

afterAll(() => {
    plainServer.stop(true)
    encodedServer.stop(true)
})

describe('HttpTransport', () => {
    const client = new RpcClient<Methods>(
        new HttpTransport({
            url: `http://localhost:${plainServer.port}/`,
            headers: () => ({ 'x-test': 'yes', 'content-type': 'application/json' })
        })
    )

    test('success resolves data; headers provider applies', async () => {
        const result = await client.call('echo', { a: 1 })
        expect(result.echoed).toEqual({ a: 1 })
        expect(result.sawHeader).toBe('yes')
    })

    test('*_NOT_FOUND maps to NotFoundError', async () => {
        await expect(client.call('thing.get', 'T1')).rejects.toBeInstanceOf(NotFoundError)
    })

    test('other codes map to ApiError with details', async () => {
        try {
            await client.call('forbidden')
            expect.unreachable()
        } catch (e) {
            expect(e).toBeInstanceOf(ApiError)
            expect((e as ApiError).message).toBe('FORBIDDEN')
            expect((e as ApiError).details).toEqual({ reason: 'test' })
        }
    })

    test('transport failure surfaces as NetworkError', async () => {
        const dead = new RpcClient(new HttpTransport({ url: 'http://127.0.0.1:1/' }))
        await expect(dead.call('echo')).rejects.toBeInstanceOf(NetworkError)
    })

    test('codec encodes requests and decodes responses', async () => {
        const encodedClient = new RpcClient<{ ping: () => { method: string } }>(new HttpTransport({ url: `http://localhost:${encodedServer.port}/`, codec }))
        const result = await encodedClient.call('ping')
        expect(result.method).toBe('ping')
    })
})
