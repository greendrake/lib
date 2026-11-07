import { afterEach, describe, expect, test } from 'bun:test'
import { requireEnv, requirePort } from '../src/env'

const NAME = 'VITE_PRESET_TEST_INPUT'
const port = process.env.VITE_PORT

afterEach(() => {
    delete process.env[NAME]
    if (port === undefined) {
        delete process.env.VITE_PORT
    } else {
        process.env.VITE_PORT = port
    }
})

describe('requireEnv', () => {
    test('is the variable the environment carries', () => {
        process.env[NAME] = 'value'
        expect(requireEnv(NAME)).toBe('value')
    })

    test('stops the config where the variable is unset or empty', () => {
        expect(() => requireEnv(NAME)).toThrow(`${NAME} environment variable must be set`)
        process.env[NAME] = ''
        expect(() => requireEnv(NAME)).toThrow(`${NAME} environment variable must be set`)
    })
})

describe('requirePort', () => {
    test('is VITE_PORT as a number, and required', () => {
        process.env.VITE_PORT = '5190'
        expect(requirePort()).toBe(5190)
        delete process.env.VITE_PORT
        expect(() => requirePort()).toThrow('VITE_PORT environment variable must be set')
    })
})
