import { ref } from 'vue'
import { defineStore } from 'pinia'
import { createDeferred } from '@greendrake/util'
import { getConnectionSource } from './uxConfig'

// Connectivity as the app's UX layer sees it, driven by the transport that
// actually carries the calls (see ConnectionSource) rather than by
// navigator.onLine — a captive portal, a dead tunnel or an unreachable backend
// all leave the device "online" while nothing can succeed.
//
// A setup store: the subscription below is a side effect that must run exactly
// once, when the store first materialises.
export const useConnectivity = defineStore('connectivity', () => {
    const source = getConnectionSource()

    // The device's own claim. Never authoritative — it only picks the wording
    // ("you appear to be offline" vs "cannot reach the site"), and kicks a
    // waiting reconnect early where the app wires that.
    const deviceOffline = ref(!navigator.onLine)
    // With a transport oracle, connectivity starts out "not yet known" rather
    // than offline: the eager connect at boot has not had its chance yet, and
    // flipping now would flash the offline surfaces over every start. Without
    // one, the device signal is the whole story and is authoritative already.
    const offline = ref(!source && deviceOffline.value)
    // Offline *and* an attempt to come back has failed too. The surfaces that
    // interrupt a working screen wait for this, so a socket that reconnects on
    // its first try — a rolling deploy, a momentary drop — passes unremarked.
    const confirmedOffline = ref(offline.value)
    // Calls parked until connectivity returns. Plain array, not state: wiring,
    // and settling them must not depend on anything reactive.
    const waiters: Array<() => void> = []

    const connected = (): void => {
        offline.value = false
        confirmedOffline.value = false
        waiters.splice(0).forEach(resolve => resolve())
    }

    // What confirms an outage differs by source: the transport reports once
    // per failed attempt, so the second consecutive report is the confirmation,
    // while the device signal has no attempts to count and speaks for itself.
    const disconnected = (confirmed: boolean): void => {
        confirmedOffline.value = confirmed || offline.value
        offline.value = true
    }

    addEventListener('online', () => {
        deviceOffline.value = false
        if (!source) {
            connected()
        }
    })
    addEventListener('offline', () => {
        deviceOffline.value = true
        if (!source) {
            disconnected(true)
        }
    })
    source?.onConnectionChange(up => (up ? connected() : disconnected(false)))

    // Resolves the moment calls can succeed again — immediately when they
    // already can. Event-driven: no polling, no timers.
    const whenOnline = (): Promise<void> => {
        if (!offline.value) {
            return Promise.resolve()
        }
        const { promise, resolve } = createDeferred()
        waiters.push(resolve)
        return promise
    }

    return {
        offline,
        confirmedOffline,
        deviceOffline,
        whenOnline
    }
})
