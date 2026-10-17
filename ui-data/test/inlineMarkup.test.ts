import './dom'
import { describe, expect, test } from 'bun:test'
import { inlineMarkup } from '../src/inlineMarkup'

// The fragment as InlineMarkup renders it.
const serialized = (markup: string): string => {
    const holder = document.createElement('div')
    holder.appendChild(inlineMarkup(markup))
    return holder.innerHTML
}

describe('inlineMarkup', () => {
    test('keeps inline formatting, never an attribute', () => {
        expect(serialized('<b onclick="x()" style="color: red" class="c">a</b><a href="javascript:x()">b</a><em title="t">c</em><code id="i">d</code>')).toBe('<b>a</b>b<em>c</em><code>d</code>')
    })

    test('drops what is never read as text, with all of its content', () => {
        const dropped: Record<string, string> = {
            script: '<script>secret()</script>',
            style: '<style>secret {}</style>',
            template: '<template><b>secret</b></template>',
            noscript: '<noscript><b>secret</b></noscript>',
            iframe: '<iframe src="x">secret</iframe>',
            object: '<object data="x"><b>secret</b></object>',
            embed: '<embed src="x">',
            svg: '<svg onload="x()"><text>secret</text><script>secret()</script></svg>',
            math: '<math><mi>secret</mi></math>',
            img: '<img src="x" onerror="x()">',
            video: '<video src="x"><b>secret</b></video>',
            audio: '<audio src="x">secret</audio>',
            canvas: '<canvas>secret</canvas>',
            comment: '<!-- secret -->'
        }
        for (const [kind, markup] of Object.entries(dropped)) {
            expect([kind, serialized(`a${markup}b`)]).toEqual([kind, 'ab'])
        }
    })

    test('unwraps other elements, keeping the formatting inside them', () => {
        expect(serialized('<span class="x"><a href="y"><b>bold <i>it</i></b></a> <font color="red">plain</font></span>')).toBe('<b>bold <i>it</i></b> plain')
    })

    test('parts runs of text across blocks with exactly one space', () => {
        expect(serialized('<p>a</p><p>b</p><ul><li>c</li><li><b>d</b></li></ul>e<br>f<div><div>g</div></div>')).toBe('a b c <b>d</b> e f g')
        expect(serialized('<p>a</p>')).toBe('a')
        expect(serialized('<div><p></p></div><p>a</p><div></div>')).toBe('a')
    })

    test('serializes to escaped text and the allowlisted tags alone', () => {
        const hostile = '1 &lt; 2 &amp;&amp; <b>3 &gt; 2</b><img src=x onerror=alert(1)><a href=javascript:alert(1)>x</a><svg><script>alert(1)</script></svg><table><tr><td><i>t</i></td></tr></table><select><option>o</option></select>'
        const out = serialized(hostile)
        const tags = [...out.matchAll(/<\/?([^\s>/]+)/g)].map(m => m[1])
        expect(tags).toEqual(['b', 'b', 'i', 'i'])
        expect(out).toBe('1 &lt; 2 &amp;&amp; <b>3 &gt; 2</b>x <i>t</i> o')
    })
})
