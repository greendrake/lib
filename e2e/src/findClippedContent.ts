import type { Page } from '@playwright/test'

// Find elements whose own content is clipped away and cannot be scrolled to —
// the signature of a box that collapsed under its contents.
//
// The usual cause is a grid or flex item that is also a scroll container. Such
// an item has no automatic minimum size (CSS Sizing 3 §3.4: the size of a
// scroll container's content does not constrain it), so its track or line
// collapses to nothing while its children keep their full height and are
// clipped. It is easy to write by accident — `overflow: hidden` for rounded
// corners on a grid card is enough — and the result is invisible content
// rather than an error.
//
// Engines disagree on which overflow values trigger it, so a page that looks
// right in one browser can collapse in another. Run this in every engine the
// app is used in; `overflow: clip` in particular is safe in Blink and not in
// older WebKit.

export interface ClippedElement {
    // A readable path to the element, e.g. "div.News > a.Card".
    selector: string
    // Rendered box height, and the height its content actually needs.
    height: number
    contentHeight: number
    overflowY: string
    // The parent's display, since the hazard is specific to grid/flex items.
    parentDisplay: string
    text: string
}

// A box may sit a pixel or two under its content through rounding alone.
const TOLERANCE_PX = 4

export const findClippedContent = async (page: Page): Promise<ClippedElement[]> =>
    page.evaluate(tolerance => {
        const describe = (el: Element): string => {
            const name = (e: Element) => e.tagName.toLowerCase() + (e.classList.length ? `.${[...e.classList].join('.')}` : '')
            return el.parentElement ? `${name(el.parentElement)} > ${name(el)}` : name(el)
        }

        const found: ClippedElement[] = []
        for (const el of document.querySelectorAll<HTMLElement>('*')) {
            const style = getComputedStyle(el)
            // Only boxes that clip without offering a scrollbar hide content
            // for good; `auto`/`scroll` are deliberate scroll containers, and
            // `visible` does not clip at all.
            if (style.overflowY !== 'hidden' && style.overflowY !== 'clip') continue
            if (el.scrollHeight <= el.clientHeight + tolerance) continue
            // A collapsed box has children taller than itself. An ordinary
            // truncation (a clamped line of text) has none, and is intended.
            if (el.childElementCount === 0) continue
            found.push({
                selector: describe(el),
                height: Math.round(el.getBoundingClientRect().height),
                contentHeight: el.scrollHeight,
                overflowY: style.overflowY,
                parentDisplay: el.parentElement ? getComputedStyle(el.parentElement).display : '',
                text: (el.textContent ?? '').trim().slice(0, 60)
            })
        }
        return found
    }, TOLERANCE_PX)
