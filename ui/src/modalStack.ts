// Module-level modal stack: with nested modals open, Escape and browser
// history navigation (popstate) must close only the top-most modal, not all of
// them at once. Modals register their close handler on mount; the shared
// document/window listeners exist only while at least one modal is open and
// always act on the top of the stack.
//
// The stack also owns modal FOCUS CONFINEMENT (WCAG 2.4.3): while a modal is
// open, everything outside the top-most modal's root is `inert` (unfocusable,
// hidden from AT), stray focus is redirected back inside (defence-in-depth for
// engines with partial inert support, with first/last Tab wrapping), initial
// focus moves into the modal, and closing restores focus to the invoker.
// Centralised here — not per-modal — so only the top-most modal ever traps.
import { hasFinePointer } from '@greendrake/util/browser'

type CloseHandler = () => void

// Where a modal wants focus when it opens: on the field it exists to fill, or
// on the dialog itself when its fields serve options within it rather than the
// modal's purpose. This states the modal's INTENT only — whether a field is
// actually worth focusing also depends on the device (see focusInitial).
export type InitialFocus = 'field' | 'dialog'

type ModalEntry = {
    close: CloseHandler
    root: HTMLElement | null
    invoker: HTMLElement | null
}

const stack: ModalEntry[] = []
const top = (): ModalEntry | undefined => stack[stack.length - 1]

const closeTop = (): void => {
    top()?.close()
}

const onKeyUp = (e: KeyboardEvent): void => {
    if (e.key === 'Escape') {
        closeTop()
    }
}

// In an SPA the router drives route changes off popstate, so a modal left open
// by stale parent state would otherwise overlay the new page.
const onPopState = (): void => closeTop()

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

const focusables = (root: HTMLElement): HTMLElement[] => [...root.querySelectorAll<HTMLElement>(FOCUSABLE)].filter(n => n.getClientRects().length > 0)

// Fallback focus target when a modal holds no focusable content: its dialog
// container (carries tabindex="-1").
const container = (root: HTMLElement): HTMLElement | null => root.querySelector<HTMLElement>('[tabindex="-1"]')

// Initial focus prefers a form field (the reason most modals open) over
// chrome like the close button, which is first in DOM order. Two things veto
// that and hand focus to the dialog itself instead: the modal declaring its
// fields incidental (`initialFocus: 'dialog'`), and a device with no fine
// pointer. On touch, focusing a field summons the on-screen keyboard — half
// the viewport gone before the user has asked to type — which is a cost no
// modal's intent can be worth, so it is decided here rather than at each of
// them. Focus still moves INTO the modal either way: the trap, the dialog's
// announcement to assistive tech and the restore-on-close all depend on it
// (see registerModal), so the touch case redirects focus, never skips it.
//
// preventScroll on the open/close focus moves: they are structural, not
// navigation, and scrolling the page to reveal what the code just focused
// moves content the reader did not ask to move. The trap's Tab wrap
// (onFocusIn) deliberately does NOT prevent scroll — that move answers a
// keypress, and in a sheet with internal overflow the wrapped-to control may
// sit outside the visible part, where suppressing the reveal would land
// keyboard focus on something invisible.
const focusInitial = (root: HTMLElement, initialFocus: InitialFocus): void => {
    if (initialFocus === 'dialog' || !hasFinePointer()) {
        container(root)?.focus({ preventScroll: true })
        return
    }
    const all = focusables(root)
    const target = all.find(n => /^(input|textarea|select)$/i.test(n.tagName)) ?? all[0] ?? container(root)
    target?.focus({ preventScroll: true })
}

// Background scroll lock, held while any modal is open. The page behind a
// modal has no business moving, and on iOS it otherwise does: focusing a field
// inside the sheet makes WebKit scroll the document to clear the keyboard, and
// since the sheet is fixed, all that moves is the page under it. Pinning the
// body is what actually leaves the document unscrollable — `overflow: hidden`
// propagates between html and body and stops nothing — so the keyboard scrolls
// the sheet's own overflow instead, which is the thing that should move.
let lockedAt: number | null = null
let lockedHref = ''
const lockScroll = (): void => {
    lockedAt = window.scrollY
    lockedHref = location.href
    // Classic (space-taking) scrollbars: pinning the body leaves the document
    // unscrollable, which drops its scrollbar and shifts the whole background
    // by the gutter width. Hold that width as extra body padding for the
    // lock's duration; overlay scrollbars measure 0 and change nothing.
    const gutter = window.innerWidth - document.documentElement.clientWidth
    Object.assign(document.body.style, {
        position: 'fixed',
        top: `-${lockedAt}px`,
        left: '0',
        right: '0',
        ...(gutter > 0 && { paddingRight: `${parseFloat(getComputedStyle(document.body).paddingRight) + gutter}px` })
    })
}
const unlockScroll = (): void => {
    if (lockedAt === null) return
    Object.assign(document.body.style, {
        position: '',
        top: '',
        left: '',
        right: '',
        paddingRight: ''
    })
    // Restore the offset only where it still applies: a popstate can navigate
    // and close the modal in one stroke (unregistration then runs after the
    // route has changed), and the old page's offset imposed on the new page
    // would override the browser's own scroll restoration.
    if (location.href === lockedHref) window.scrollTo(0, lockedAt)
    lockedAt = null
}

// Everything outside the top-most modal's root goes inert — including the
// roots of modals lower in the stack. Only attributes this module set are
// removed on recompute, so independently-inert content is left alone.
let inerted: Element[] = []
const applyInert = (): void => {
    for (const n of inerted) n.removeAttribute('inert')
    inerted = []
    const root = top()?.root
    if (!root) return
    for (const child of document.body.children) {
        if (child !== root && !child.contains(root) && !child.hasAttribute('inert')) {
            child.setAttribute('inert', '')
            inerted.push(child)
        }
    }
}

const onFocusIn = (e: FocusEvent): void => {
    const root = top()?.root
    if (!root || root.contains(e.target as Node)) return
    const all = focusables(root)
    if (!all.length) {
        container(root)?.focus()
        return
    }
    // Focus escaped: Shift-Tab off the first element wraps to the last;
    // anything else (Tab off the last, programmatic strays) lands on the first.
    const target = e.relatedTarget === all[0] ? all[all.length - 1] : all[0]
    target.focus()
}

export const registerModal = (close: CloseHandler, root: HTMLElement | null = null, initialFocus: InitialFocus = 'field'): (() => void) => {
    if (stack.length === 0) {
        document.addEventListener('keyup', onKeyUp)
        document.addEventListener('focusin', onFocusIn)
        window.addEventListener('popstate', onPopState)
        lockScroll()
    }
    const entry: ModalEntry = {
        close,
        root,
        invoker: document.activeElement instanceof HTMLElement ? document.activeElement : null
    }
    stack.push(entry)
    applyInert()
    if (root) focusInitial(root, initialFocus)
    return () => {
        stack.splice(stack.indexOf(entry), 1)
        if (stack.length === 0) {
            document.removeEventListener('keyup', onKeyUp)
            document.removeEventListener('focusin', onFocusIn)
            window.removeEventListener('popstate', onPopState)
            unlockScroll()
        }
        applyInert()
        if (entry.invoker?.isConnected) entry.invoker.focus({ preventScroll: true })
    }
}
