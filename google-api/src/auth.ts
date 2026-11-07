// A service account's access to Google APIs.
//
// Google does not take a signed assertion as a bearer token the way App Store
// Connect does: the assertion is exchanged for an access token first. That
// token lasts an hour, so it is bought once and held until nearly spent rather
// than per request.
import { createPrivateKey, sign, type KeyObject } from 'node:crypto'
import { request, type ClientOptions } from '@greendrake/util'

const TOKEN_URL = 'https://oauth2.googleapis.com/token'
const ASSERTION_LIFETIME_SECONDS = 3600
// A token this close to expiry is not handed out: a call that starts with it
// should not have it expire under it.
const REFRESH_MARGIN_SECONDS = 60

// What answers a bearer token for each call: `bearerRequest` of
// @greendrake/util takes one.
export type Bearer = () => Promise<string>

// The two fields of a service-account key's JSON that sign for it:
// `client_email` and `private_key` — the PEM, or the key already parsed.
export interface ServiceAccountKey {
    clientEmail: string
    privateKey: string | KeyObject
}

// What the token endpoint answers an exchange with.
interface Token {
    access_token: string
    expires_in: number
}

const segment = (value: unknown): string => Buffer.from(JSON.stringify(value)).toString('base64url')

// The access token of a service account for `scopes`, bought by the JWT-bearer
// exchange and held until a minute before it expires. One exchange is in
// flight at a time: calls arriving during it get its token, and one that fails
// leaves the next call to start another. A scope the account was never granted
// fails at the call that needs it, which is where that error belongs.
export const serviceAccountBearer = (key: ServiceAccountKey, scopes: readonly string[], options: ClientOptions): Bearer => {
    // Parsed once, here, so a key that is not one fails at construction rather
    // than at the first call.
    const privateKey = typeof key.privateKey === 'string' ? createPrivateKey(key.privateKey) : key.privateKey
    const seconds = () => Math.floor(options.now() / 1000)
    let held: { value: string; expiresAt: number } | null = null
    let exchanging: Promise<string> | null = null

    const exchange = async (): Promise<string> => {
        const now = seconds()
        const input = `${segment({ alg: 'RS256', typ: 'JWT' })}.${segment({
            iss: key.clientEmail,
            scope: scopes.join(' '),
            aud: TOKEN_URL,
            iat: now,
            exp: now + ASSERTION_LIFETIME_SECONDS
        })}`
        const assertion = `${input}.${sign('sha256', Buffer.from(input), privateKey).toString('base64url')}`
        const token = await request<Token>(options.fetch, TOKEN_URL, {
            method: 'POST',
            body: new URLSearchParams({ grant_type: 'urn:ietf:params:oauth:grant-type:jwt-bearer', assertion })
        })
        held = { value: token.access_token, expiresAt: now + token.expires_in }
        return held.value
    }

    return () => {
        if (held && held.expiresAt > seconds() + REFRESH_MARGIN_SECONDS) return Promise.resolve(held.value)
        exchanging ??= exchange().finally(() => {
            exchanging = null
        })
        return exchanging
    }
}
