import { h, render, type VNode } from 'vue'
import ToastShell from './ToastShell.vue'

export interface ToastHandle {
    destroy: () => void
}

// What the toast is saying, which decides its colour: `accent` for the ordinary
// "that worked" report, `warning` for a failure.
export type ToastVariant = 'accent' | 'warning'

export interface ToastOptions {
    content: string | VNode
    // Auto-dismiss on the next user interaction (key press / pointer down)
    // after the toast appears. Default true; blocking error toasts opt out.
    hideOnClick?: boolean
    // Defaults to `accent`: most toasts confirm something, and a failure says
    // so explicitly rather than every caller restating the ordinary case.
    variant?: ToastVariant
    onHidden?: () => void
}

// The one fixed top-center layer every toast renders into, so toasts raised
// together stack in the order shown rather than each sitting over the last.
// Created on first use and kept: it is pointer-transparent, and empty it
// costs nothing.
let layer: HTMLElement | null = null
const toastLayer = (): HTMLElement => {
    if (!layer) {
        layer = document.createElement('div')
        layer.className = 'ToastLayer'
        document.body.appendChild(layer)
    }
    return layer
}

// Imperative top-center toast. Renders without an app context, so it is
// callable from stores and plain modules; interactive content arrives as a
// VNode with its handlers already bound.
export const showToast = (options: ToastOptions): ToastHandle => {
    const host = document.createElement('div')
    toastLayer().appendChild(host)
    const isText = typeof options.content === 'string'
    const shellProps = { variant: options.variant ?? 'accent' }
    const vnode = isText ? h(ToastShell, { ...shellProps, text: options.content as string }) : h(ToastShell, shellProps, { default: () => options.content as VNode })
    render(vnode, host)

    const destroy = (): void => {
        detachDismiss()
        render(null, host)
        host.remove()
        options.onHidden?.()
    }

    let detachDismiss = (): void => {}
    if (options.hideOnClick !== false) {
        // Pointer-downs inside ANY toast don't dismiss: destroying at
        // pointerdown would unmount interactive content (e.g. a link) before
        // the browser ever synthesizes its click — and destroying a toast
        // stacked above the one being pressed would shift that one out from
        // under the pointer, so the click lands elsewhere. Interacting with a
        // toast is not the interaction that clears the stack.
        const onInteraction = (e: Event): void => {
            if (e.target instanceof Node && toastLayer().contains(e.target)) return
            destroy()
        }
        // Attach on the next frame so the interaction that triggered the toast
        // cannot dismiss it in the same event cycle.
        const raf = requestAnimationFrame(() => {
            document.addEventListener('keydown', onInteraction)
            document.addEventListener('pointerdown', onInteraction)
        })
        detachDismiss = () => {
            cancelAnimationFrame(raf)
            document.removeEventListener('keydown', onInteraction)
            document.removeEventListener('pointerdown', onInteraction)
        }
    }

    return { destroy }
}
