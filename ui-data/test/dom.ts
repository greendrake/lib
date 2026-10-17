import { JSDOM } from 'jsdom'

// bun test has no DOM: the code under test parses and builds markup in
// jsdom's, whose parser is the HTML standard's. Imported first by every test
// that needs it, so the globals exist before the module under test loads.
const { window } = new JSDOM('')
Object.assign(globalThis, {
    DOMParser: window.DOMParser,
    document: window.document,
    Node: window.Node,
    Element: window.Element,
    Text: window.Text
})
