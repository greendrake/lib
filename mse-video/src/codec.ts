declare global {
    interface Window {
        // Apple's Managed Media Source (Safari 17+), the only Media Source flavour
        // iPhones expose: plain MediaSource is undefined there. TypeScript's DOM
        // library has no definition yet; it is a MediaSource subclass, and the
        // constructor type covers all of it used here — its extra streaming API is
        // not. https://w3c.github.io/media-source/#managedmediasource
        ManagedMediaSource?: typeof MediaSource
    }
}

// The content type of every stream a webcast server casts: H.265 in fMP4.
export const streamCodec = 'video/mp4; codecs="hvc1.1.6.L153.B0"'

// A browser's Media Source constructors, as its globals have them.
export interface MediaSources {
    ManagedMediaSource?: typeof MediaSource
    MediaSource?: typeof MediaSource
}

// The Media Source a browser has: Apple's Managed one where it exists, the standard one
// otherwise, and none at all on an iPhone before Safari 17.
export const mediaSourceOf = (browser: MediaSources): typeof MediaSource | undefined =>
    browser.ManagedMediaSource ?? browser.MediaSource

// Whether a browser can play the stream at all.
export const canPlay = (browser: MediaSources = window): boolean =>
    mediaSourceOf(browser)?.isTypeSupported(streamCodec) ?? false
