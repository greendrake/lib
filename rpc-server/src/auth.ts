import { timingSafeEqual } from 'node:crypto'
import type { AuthResolver } from './types'

// A single pre-shared secret — the static API key pattern: a trusted
// non-browser caller (an admin dashboard behind a webserver-level gate, a
// deployment tool, an MCP server) authenticates with one configured key
// instead of a per-user token. The principal a match yields is the
// application's, so it is supplied here. The comparison takes the same time
// wherever a guess goes wrong, so timing it reveals nothing of the key.
//
// An empty key matches nothing: an unconfigured secret must not turn into a
// hole that lets everyone in.
export const keyAuthResolver = <P>(key: string, principal: P): AuthResolver<P> => {
    const expected = Buffer.from(key)
    return {
        resolveToken: token => {
            const presented = Buffer.from(token)
            return Promise.resolve(key !== '' && presented.length === expected.length && timingSafeEqual(presented, expected) ? principal : null)
        }
    }
}

// The credential out of an Authorization header: the token of `Bearer
// <token>`, the scheme cased however the client likes. Any other scheme is not
// a credential this understands, and yields none.
export const bearerToken = (request: Request): string => {
    const [scheme, token] = request.headers.get('authorization')?.split(' ', 2) ?? []
    return scheme?.toLowerCase() === 'bearer' && token ? token : ''
}
