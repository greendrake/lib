// The characters markup gives meaning to, each as the reference that stands
// for it literally. The pattern is built from the table's keys, so every match
// is one of them.
const REFERENCES: Record<string, string> = {
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;'
}
const MARKUP = new RegExp(`[${Object.keys(REFERENCES).join('')}]`, 'g')

// Text as HTML (or XML) reads it back verbatim, both as element content and as
// an attribute value in either quote.
export const escapeHtml = (text: string): string => text.replace(MARKUP, char => REFERENCES[char]!)

// A value as JSON that can sit inside a <script> element: with every `<` as
// its JSON unicode escape, neither `</script>` nor `<!--` can occur in it, and
// a JSON parser reads the value back unchanged.
export const scriptJson = (value: unknown): string => JSON.stringify(value).replaceAll('<', '\\u003c')
