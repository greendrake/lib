import { describe, expect, test } from 'bun:test'
import { canPlay, mediaSourceOf, streamCodec, type MediaSources } from '../src/codec'

// A Media Source that takes only the codecs given.
const takes = (...codecs: string[]) => ({ isTypeSupported: (codec: string) => codecs.includes(codec) }) as typeof MediaSource

describe('canPlay', () => {
    test("a browser whose Media Source takes the stream's codec can play it", () => {
        expect(canPlay({ MediaSource: takes(streamCodec) })).toBe(true)
    })
    test('one whose Media Source refuses it cannot', () => {
        expect(canPlay({ MediaSource: takes('video/mp4; codecs="avc1.42E01E"') })).toBe(false)
    })
    test('nor can one with no Media Source at all', () => {
        expect(canPlay({})).toBe(false)
    })
    test("Apple's Managed Media Source is used where there is one", () => {
        const managed = takes(streamCodec)
        const browser: MediaSources = { ManagedMediaSource: managed, MediaSource: takes() }
        expect(mediaSourceOf(browser)).toBe(managed)
        expect(canPlay(browser)).toBe(true)
    })
})
