// Puts the element in fullscreen, or leaves fullscreen if it is already the one
// in it. Returns false without doing anything where the browser has no element
// fullscreen at all — iPhone Safari, whose only fullscreen is the native video
// player — leaving the caller to decide on a substitute.
export const toggleFullscreen = (el: Element): boolean => {
    if (!document.fullscreenEnabled) {
        return false
    }
    void (document.fullscreenElement === el ? document.exitFullscreen() : el.requestFullscreen())
    return true
}

// True where the primary input is a precise pointer that can hover — a mouse or
// trackpad, not a finger.
//
// Callers use it as a proxy for "focusing a field will not raise an on-screen
// keyboard", which no platform exposes directly (navigator.virtualKeyboard is
// Chromium-only and governs overlay policy, not availability). The proxy holds
// for a touchscreen laptop — physical keyboard, so nothing pops up — and fails
// only for a touch device driven by a bare mouse with no keyboard attached.
export const hasFinePointer = (): boolean => window.matchMedia('(hover: hover) and (pointer: fine)').matches

let readyStatePromise: Promise<void> | undefined

// Resolves when document.readyState reaches 'complete' (immediately if it
// already has). Browser-only at call time; safe to import in Node.
export const getReadyStatePromise = (): Promise<void> => {
    if (!readyStatePromise) {
        readyStatePromise =
            document.readyState === 'complete'
                ? Promise.resolve()
                : new Promise<void>(resolve => {
                      const listener = (): void => {
                          if (document.readyState === 'complete') {
                              document.removeEventListener('readystatechange', listener)
                              resolve()
                          }
                      }
                      document.addEventListener('readystatechange', listener)
                  })
    }
    return readyStatePromise
}
