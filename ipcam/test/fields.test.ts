import { describe, expect, test } from 'bun:test'
import { checkText, draftsOf, isText, settle, writeOf } from '../src/fields'
import type { Setting } from '../src/types'

const described = {
    Group: 'network',
    Label: 'A setting',
    Tip: ''
}

const port = {
    ...described,
    Key: 'network.port',
    Type: 'integer',
    Min: 1,
    Max: 65535,
    Value: 34567
} satisfies Setting
const frameRate = {
    ...described,
    Key: 'encode.main.frameRate',
    Group: 'encode',
    Type: 'integer',
    Value: -2
} satisfies Setting
const atMost = {
    ...described,
    Key: 'general.autoLogout',
    Group: 'general',
    Type: 'integer',
    Max: 120,
    Value: 0
} satisfies Setting
const gateway = {
    ...described,
    Key: 'network.gateway',
    Type: 'ipv4',
    Value: '192.0.2.1'
} satisfies Setting
const server = {
    ...described,
    Key: 'ntp.server',
    Group: 'ntp',
    Type: 'text',
    Value: 'pool.ntp.org'
} satisfies Setting
const name = {
    ...described,
    Key: 'overlay.name',
    Group: 'overlay',
    Type: 'text',
    MinLength: 1,
    MaxLength: 8,
    Excluded: '\\"',
    Value: 'pond'
} satisfies Setting
const nameTop = {
    ...described,
    Key: 'overlay.nameTop',
    Group: 'overlay',
    Type: 'fraction',
    Value: 0.943359375
} satisfies Setting
const dhcp = {
    ...described,
    Key: 'network.dhcp',
    Type: 'switch',
    Value: true
} satisfies Setting
const policy = {
    ...described,
    Key: 'network.transferPolicy',
    Type: 'choice',
    Options: [
        { Value: 'quality', Label: 'Quality' },
        { Value: 'fluency', Label: 'Fluency' }
    ],
    Value: 'quality'
} satisfies Setting
const resolution = {
    ...described,
    Key: 'encode.extra.resolution',
    Group: 'encode',
    Type: 'choice',
    Options: [{ Value: 'D1', Label: 'D1' }],
    Value: 'CIF'
} satisfies Setting
const codec = {
    ...described,
    Key: 'encode.extra.codec',
    Group: 'encode',
    Type: 'pinned',
    Only: 'H.265',
    Value: 'H.264'
} satisfies Setting
const mac = {
    ...described,
    Key: 'network.mac',
    Type: 'readonly',
    Value: '00:00:5e:00:53:01'
} satisfies Setting

const settings: Setting[] = [port, frameRate, gateway, server, name, nameTop, dhcp, policy, resolution, codec, mac]

describe('a text', () => {
    test('of an integer is a whole number within its bounds', () => {
        expect(checkText(port, '80')).toBe(true)
        expect(checkText(port, '70000')).toBe('A whole number from 1 to 65535')
        expect(checkText(port, '8.5')).toBe('A whole number from 1 to 65535')
        expect(checkText(atMost, 'x')).toBe('A whole number to 120')
        expect(checkText(frameRate, '-1')).toBe(true)
        expect(checkText(frameRate, '')).toBe('A whole number')
    })
    test('of a share is a percentage to two decimals', () => {
        for (const text of ['0', '94.34', '100', '5.5']) {
            expect(checkText(nameTop, text)).toBe(true)
        }
        for (const text of ['100.01', '94.345', '-1', '', 'half']) {
            expect(checkText(nameTop, text)).toBe('A percentage from 0 to 100, to two decimals')
        }
    })
    test('of an address is a dotted quad', () => {
        expect(checkText(gateway, '203.0.113.1')).toBe(true)
        for (const text of ['256.0.0.1', '203.0.113', '203.0.113.1.', '']) {
            expect(checkText(gateway, text)).toBe('An IPv4 address: four numbers to 255 joined by dots')
        }
    })
    test('of a text is anything within its lengths, without the characters it excludes', () => {
        expect(checkText(server, '')).toBe(true)
        // Characters, not UTF-16 units.
        expect(checkText(name, 'Ünïcode😀')).toBe(true)
        for (const text of ['', 'ninechars', 'a"b', 'a\\b']) {
            expect(checkText(name, text)).toBe('A text of 1 to 8 characters without \\ or "')
        }
        expect(checkText({ ...server, MinLength: 2 }, 'a')).toBe('A text of at least 2 characters')
        expect(checkText({ ...server, MaxLength: 2 }, 'abc')).toBe('A text of at most 2 characters')
    })
})

describe('a section', () => {
    test('edits as text the settings of the kinds typed in', () => {
        expect(settings.filter(isText)).toEqual([port, frameRate, gateway, server, name, nameTop])
    })

    test('drafts what it reads in its controls’ terms', () => {
        expect(draftsOf(settings)).toEqual({
            text: {
                'network.port': '34567',
                'encode.main.frameRate': '-2',
                'network.gateway': '192.0.2.1',
                'ntp.server': 'pool.ntp.org',
                'overlay.name': 'pond',
                // A share in percent, to two decimals.
                'overlay.nameTop': '94.34'
            },
            switch: { 'network.dhcp': true },
            // A value none of the options is drafted as none.
            choice: { 'network.transferPolicy': 'quality', 'encode.extra.resolution': null }
        })
    })

    test('writes a setting’s draft as a value of its kind', () => {
        const drafts = draftsOf(settings)
        drafts.text['network.port'] = '8080'
        drafts.text['network.gateway'] = '192.0.2.254'
        drafts.text['overlay.nameTop'] = '50'
        drafts.switch['network.dhcp'] = false
        drafts.choice['network.transferPolicy'] = 'fluency'
        expect(writeOf(port, drafts)).toBe(8080)
        expect(writeOf(gateway, drafts)).toBe('192.0.2.254')
        expect(writeOf(server, drafts)).toBe('pool.ntp.org')
        expect(writeOf(nameTop, drafts)).toBe(0.5)
        expect(writeOf(dhcp, drafts)).toBe(false)
        expect(writeOf(policy, drafts)).toBe('fluency')
        // A pinned setting is written as what it is pinned to.
        expect(writeOf(codec, drafts)).toBe('H.265')
    })

    test('writes nothing of a text that is not yet a value, or a choice of none', () => {
        const drafts = draftsOf(settings)
        drafts.text['network.port'] = '70000'
        drafts.text['network.gateway'] = '192.168.72'
        drafts.text['overlay.name'] = ''
        drafts.text['overlay.nameTop'] = '101'
        expect(writeOf(port, drafts)).toBeUndefined()
        expect(writeOf(gateway, drafts)).toBeUndefined()
        expect(writeOf(name, drafts)).toBeUndefined()
        expect(writeOf(nameTop, drafts)).toBeUndefined()
        expect(writeOf(resolution, drafts)).toBeUndefined()
    })

    test('takes what the camera has once a write is read back, and keeps what was edited meanwhile', () => {
        const drafts = draftsOf(settings)
        drafts.text['network.gateway'] = '0.0.0.0'
        const sent = structuredClone(drafts)
        drafts.text['network.port'] = '8080'
        // The camera took the gateway and kept its own.
        settle(drafts, sent, draftsOf([gateway, port]))
        expect(drafts.text['network.gateway']).toBe('192.0.2.1')
        expect(drafts.text['network.port']).toBe('8080')
        expect(drafts.switch['network.dhcp']).toBe(true)
    })
})
