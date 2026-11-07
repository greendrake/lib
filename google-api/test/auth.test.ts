import { describe, expect, test } from 'bun:test'
import { generateKeyPairSync, verify } from 'node:crypto'
import { ApiResponseError, serviceAccountBearer } from '../src/main'
import { recordingFetch } from '@greendrake/util/testing'
import { NOW } from './clock'

const key = generateKeyPairSync('rsa', { modulusLength: 2048 })
const PEM = key.privateKey.export({ type: 'pkcs8', format: 'pem' }).toString()
const ACCOUNT = { clientEmail: 'robot@project.iam.gserviceaccount.com', privateKey: PEM }
const SCOPES = ['https://www.googleapis.com/auth/webmasters', 'https://www.googleapis.com/auth/androidpublisher']

const TOKEN = () =>
    Response.json({
        access_token: 'ya29.token',
        expires_in: 3600,
        token_type: 'Bearer'
    })

const decode = (segment: string): unknown => JSON.parse(Buffer.from(segment, 'base64url').toString())

describe('serviceAccountBearer', () => {
    test('exchanges an RS256 assertion for the scopes it is given, signed by the key and timed by the clock', async () => {
        const { fetch, requests: sent } = recordingFetch(TOKEN)
        expect(await serviceAccountBearer(ACCOUNT, SCOPES, { fetch, now: () => NOW })()).toBe('ya29.token')
        expect(sent).toHaveLength(1)
        expect(sent[0]?.method).toBe('POST')
        expect(sent[0]?.url).toBe('https://oauth2.googleapis.com/token')
        const form = new URLSearchParams(sent[0]?.body as URLSearchParams)
        expect(form.get('grant_type')).toBe('urn:ietf:params:oauth:grant-type:jwt-bearer')
        const [header = '', claims = '', signature = ''] = (form.get('assertion') ?? '').split('.')
        expect(decode(header)).toEqual({ alg: 'RS256', typ: 'JWT' })
        expect(decode(claims)).toEqual({
            iss: 'robot@project.iam.gserviceaccount.com',
            scope: 'https://www.googleapis.com/auth/webmasters https://www.googleapis.com/auth/androidpublisher',
            aud: 'https://oauth2.googleapis.com/token',
            iat: NOW / 1000,
            exp: NOW / 1000 + 3600
        })
        expect(verify('sha256', Buffer.from(`${header}.${claims}`), key.publicKey, Buffer.from(signature, 'base64url'))).toBe(true)
    })

    test('takes a key already parsed', async () => {
        const { fetch } = recordingFetch(TOKEN)
        expect(await serviceAccountBearer({ ...ACCOUNT, privateKey: key.privateKey }, SCOPES, { fetch, now: () => NOW })()).toBe('ya29.token')
    })

    test('a key that is not one fails at construction', () => {
        const { fetch } = recordingFetch(TOKEN)
        expect(() => serviceAccountBearer({ ...ACCOUNT, privateKey: 'not a key' }, SCOPES, { fetch, now: () => NOW })).toThrow()
    })

    test('buys one token for calls arriving together', async () => {
        const answer = Promise.withResolvers<Response>()
        const { fetch, requests: sent } = recordingFetch(() => answer.promise)
        const bearer = serviceAccountBearer(ACCOUNT, SCOPES, { fetch, now: () => NOW })
        const both = Promise.all([bearer(), bearer()])
        answer.resolve(TOKEN())
        expect(await both).toEqual(['ya29.token', 'ya29.token'])
        expect(sent).toHaveLength(1)
    })

    test('holds the token until a minute before it expires, then buys another', async () => {
        let now = NOW
        let bought = 0
        const { fetch, requests: sent } = recordingFetch(() => Response.json({ access_token: `token-${++bought}`, expires_in: 3600 }))
        const bearer = serviceAccountBearer(ACCOUNT, SCOPES, { fetch, now: () => now })
        expect(await bearer()).toBe('token-1')
        now += (3600 - 61) * 1000
        expect(await bearer()).toBe('token-1')
        now += 1000
        expect(await bearer()).toBe('token-2')
        expect(sent).toHaveLength(2)
    })

    test("a refused exchange throws ApiResponseError carrying the endpoint's whole answer, and the next call tries again", async () => {
        const refusal = { error: 'invalid_grant', error_description: 'Invalid JWT Signature.' }
        const answers = [Response.json(refusal, { status: 400, statusText: 'Bad Request' }), TOKEN()]
        const { fetch, requests: sent } = recordingFetch(() => answers.shift() ?? TOKEN())
        const bearer = serviceAccountBearer(ACCOUNT, SCOPES, { fetch, now: () => NOW })
        const error = await bearer().catch((e: unknown) => e)
        expect(error).toBeInstanceOf(ApiResponseError)
        expect(error).toMatchObject({
            method: 'POST',
            path: '/token',
            status: 400,
            body: refusal
        })
        expect(await bearer()).toBe('ya29.token')
        expect(sent).toHaveLength(2)
    })

    test('an exchange that gets no answer rejects with its own error, untouched', async () => {
        const unreachable = new TypeError('fetch failed')
        await expect(serviceAccountBearer(ACCOUNT, SCOPES, { fetch: () => Promise.reject(unreachable), now: () => NOW })()).rejects.toBe(unreachable)
    })
})
