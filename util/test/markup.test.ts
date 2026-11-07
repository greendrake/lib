import { describe, expect, test } from 'bun:test'
import { escapeHtml, scriptJson } from '../src/markup'

describe('escapeHtml', () => {
    test.each([
        ['&', '&amp;'],
        ['<', '&lt;'],
        ['>', '&gt;'],
        ['"', '&quot;'],
        ["'", '&#39;']
    ])('%p → %p', (char, reference) => {
        expect(escapeHtml(char)).toBe(reference)
    })

    test('replaces every markup character and nothing else', () => {
        expect(escapeHtml(`Fish & "chips" <b>'n'</b> — &amp; ünï`)).toBe('Fish &amp; &quot;chips&quot; &lt;b&gt;&#39;n&#39;&lt;/b&gt; — &amp;amp; ünï')
        expect(escapeHtml('')).toBe('')
    })
})

describe('scriptJson', () => {
    test('escapes every < so the JSON cannot end or comment out its script element', () => {
        const value = { name: '</script><!-- x < y', list: ['<b>'] }
        const json = scriptJson(value)
        expect(json).toBe('{"name":"\\u003c/script>\\u003c!-- x \\u003c y","list":["\\u003cb>"]}')
        expect(JSON.parse(json)).toEqual(value)
    })
})
