// App Store Connect as the tests stand it in.
import { generateKeyPairSync } from 'node:crypto'
import type { Fetch } from '@greendrake/util'
import { recordingFetch, type RecordedRequest } from '@greendrake/util/testing'
import { createAppStoreConnect } from '../src/main'

// Answers by `<method> <pathname>`, each with its JSON; a request nobody
// expected fails the call that made it.
export const routed = (routes: Record<string, unknown>) =>
    recordingFetch(({ method, url }) => {
        const key = `${method} ${new URL(url).pathname}`
        if (!(key in routes)) throw new Error(`unrouted: ${key}`)
        return Response.json(routes[key])
    })

// A request's JSON body, parsed; null when it has none.
export const jsonOf = (request: RecordedRequest | undefined): unknown => (typeof request?.body === 'string' ? JSON.parse(request.body) : null)

export const resource = <A>(type: string, id: string, attributes: A, relationships?: Record<string, { data: { type: string; id: string } | null }>) => ({
    type,
    id,
    attributes,
    ...(relationships ? { relationships } : {})
})

// A collection response on its last page.
export const collection = (data: unknown[], included: unknown[] = []) => ({
    data,
    included,
    links: { self: 'https://api.appstoreconnect.apple.com/v1/x' }
})

// In whole seconds, as the JWT's claims are.
export const NOW = Date.UTC(2026, 10, 6, 12)

export const key = generateKeyPairSync('ec', { namedCurve: 'prime256v1' })

export const clientOver = (fetch: Fetch) =>
    createAppStoreConnect(
        {
            issuerId: 'issuer-1',
            keyId: 'KEY123',
            privateKey: key.privateKey.export({ type: 'pkcs8', format: 'pem' }).toString()
        },
        { fetch, now: () => NOW }
    )
