import { applyMergePatch, type JsonObject, type StateFrame } from './document'
import type { PushMapConstraint, WsTransport } from './WsTransport'

/**
 * Keeps target the document the state stream `type` carries: replaced whole by each `doc`
 * frame, and patched in place by each `patch` frame after it. A server sends the `doc`
 * when a socket starts keeping the document: on every (re)connect, for a document every
 * socket keeps; as the socket joins the topic, for one a topic's members keep — which a
 * reconnected socket has to join again. A patch over no document is a protocol violation,
 * and throws. Returns the release.
 */
export const liveDocument = <K extends string, D extends object, PEvents extends PushMapConstraint<PEvents>, PData extends PushMapConstraint<PData> & Record<K, StateFrame<D>>>(transport: WsTransport<PEvents, PData>, type: K, target: D): (() => void) => {
    const doc = target as JsonObject
    // Whether this socket has sent the document: a patch is over it, never over what
    // an earlier socket left.
    let held = false
    const releaseFrames = transport.onData(type, data => {
        const frame = data as StateFrame<JsonObject>
        if ('doc' in frame) {
            for (const key of Object.keys(doc)) delete doc[key]
            applyMergePatch(doc, frame.doc)
            held = true
            return
        }
        if (!held) throw new Error(`liveDocument: a patch of '${type}' came before its document`)
        applyMergePatch(doc, frame.patch)
    })
    const releaseConnection = transport.onConnectionChange(connected => {
        if (!connected) held = false
    })
    return () => {
        releaseFrames()
        releaseConnection()
    }
}
