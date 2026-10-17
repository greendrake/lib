// Captures the event's pointer to the element handling it, so a drag keeps
// tracking the pointer outside that element without document-level listeners.
//
// setPointerCapture throws NotFoundError when the pointer is no longer active —
// released or cancelled between event dispatch and this call, a real race for
// touch/pen and under synthetic pointer streams. A gesture whose pointer is
// already gone has nothing to capture, so exactly that error is a no-op;
// anything else still throws.
export const capturePointer = (event: PointerEvent): void => {
    try {
        ;(event.currentTarget as Element).setPointerCapture(event.pointerId)
    } catch (e) {
        if (!(e instanceof DOMException && e.name === 'NotFoundError')) throw e
    }
}
