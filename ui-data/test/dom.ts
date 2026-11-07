import { JSDOM } from 'jsdom'
import { stubGlobals } from '@greendrake/util/testing'

// bun test has no DOM: the code under test parses and builds markup in
// jsdom's, whose parser is the HTML standard's. The calling file gets a fresh
// jsdom's globals for its own tests.
export const withDom = (): void =>
    stubGlobals(() => {
        const { window } = new JSDOM('')
        return {
            DOMParser: window.DOMParser,
            document: window.document,
            Node: window.Node,
            Element: window.Element,
            Text: window.Text
        }
    })
