import type { AuthInfo, AuthResolver } from './types'

// A single pre-shared secret — the static API key pattern: a trusted
// non-browser caller (an admin dashboard behind a webserver-level gate, a
// deployment tool, an MCP server) authenticates with one configured key
// instead of a per-user token. The identity a match yields is the
// application's, so the whole principal is supplied here.
//
// An empty key matches nothing: an unconfigured secret must not turn into a
// hole that lets everyone in.
export const keyAuthResolver = (key: string, info: AuthInfo): AuthResolver => ({
    resolveToken: token => Promise.resolve(key !== '' && token === key ? info : null)
})

// The credential out of an Authorization header. Two forms are on the wire:
// `Bearer <token>`, and a bare pre-shared key with no scheme at all (what the
// key-auth callers above send). Any other scheme is not a credential this
// understands, and yields none.
export const bearerToken = (request: Request): string => {
    const header = request.headers.get('authorization')
    if (!header) {
        return ''
    }
    const space = header.indexOf(' ')
    if (space === -1) {
        return header
    }
    return header.slice(0, space).toLowerCase() === 'bearer' ? header.slice(space + 1) : ''
}
