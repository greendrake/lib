// Markup rebuilt as one line of inline content, safe to render: what it reads,
// with only its inline formatting kept. Nothing of the source is carried over
// but text and the names of allowlisted elements, recreated bare — no
// attribute survives, so neither does a handler, a URL or a style.

/** Inline formatting: recreated, without attributes. */
const KEPT = new Set(['b', 'strong', 'i', 'em', 'u', 's', 'del', 'ins', 'sub', 'sup', 'code', 'mark', 'small'])

/** Never read as text: dropped with everything inside. */
const DROPPED = new Set(['script', 'style', 'template', 'noscript', 'iframe', 'frame', 'frameset', 'object', 'embed', 'svg', 'math', 'img', 'picture', 'video', 'audio', 'canvas', 'map'])

/** Read as a break between runs of text: a single space on the line. */
const BLOCKS = new Set([
    'address', 'article', 'aside', 'blockquote', 'br', 'caption', 'dd', 'details', 'dialog', 'div', 'dl', 'dt', 'fieldset', 'figcaption', 'figure', 'footer', 'form', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'header', 'hgroup', 'hr', 'li', 'main', 'nav', 'ol', 'p', 'pre', 'section', 'summary', 'table', 'tbody', 'td', 'tfoot', 'th', 'thead', 'tr', 'ul'
])

/**
 * The markup as a fragment of text and bare allowlisted inline elements: other
 * elements are unwrapped, blocks part runs of text with one space, and what is
 * never read as text (scripts, styles, embedded and media content) is dropped
 * whole. DOMParser's document is inert: nothing in it runs or loads.
 */
export const inlineMarkup = (markup: string): DocumentFragment => {
    const out = document.createDocumentFragment()
    // A block boundary owes a space to the next text or kept element, if any
    // text came before.
    let spaceOwed = false
    let anyText = false
    const paySpace = (into: Node): void => {
        if (spaceOwed && anyText) {
            into.appendChild(document.createTextNode(' '))
        }
        spaceOwed = false
    }
    const walk = (from: Node, into: Node): void => {
        for (const node of from.childNodes) {
            if (node instanceof Text) {
                paySpace(into)
                anyText = true
                into.appendChild(document.createTextNode(node.data))
            } else if (node instanceof Element && !DROPPED.has(node.localName)) {
                const block = BLOCKS.has(node.localName)
                spaceOwed ||= block
                let target = into
                if (KEPT.has(node.localName)) {
                    paySpace(into)
                    target = into.appendChild(document.createElement(node.localName))
                }
                walk(node, target)
                spaceOwed ||= block
            }
        }
    }
    walk(new DOMParser().parseFromString(markup, 'text/html').body, out)
    return out
}
