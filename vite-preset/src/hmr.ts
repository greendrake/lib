import type { HotPayload, Plugin } from 'vite'

// An HMR update whose accepted module sits inside an import cycle cannot be
// re-executed reliably; Vite's client masks that by silently reloading the
// whole page ("failed to apply HMR as it's within a circular import"), which
// hides the cycle until it bites in subtler ways. This guard intercepts such
// updates server-side and replaces them with an error overlay naming the
// offending modules — the cycle must be broken, not papered over. The client's
// reload lives outside any public hook (it never emits vite:beforeFullReload),
// so the payload send is the one interception point.
export const hmrCircularImportGuard = (): Plugin => ({
    name: 'greendrake:hmr-circular-import-guard',
    apply: 'serve',
    configureServer(server) {
        const hot = server.environments.client.hot
        const send = hot.send.bind(hot)
        hot.send = (payloadOrEvent: HotPayload | string, data?: unknown): void => {
            if (typeof payloadOrEvent === 'string') {
                send(payloadOrEvent, data)
                return
            }
            if (payloadOrEvent.type === 'update') {
                const circular = payloadOrEvent.updates.filter(u => u.isWithinCircularImport)
                if (circular.length) {
                    send({
                        type: 'error',
                        err: {
                            message:
                                `HMR blocked: the update to ${circular.map(u => u.acceptedPath).join(', ')} is within a circular import, ` +
                                `which cannot hot-apply (Vite would mask this with a full page reload). ` +
                                `Break the cycle — run \`vite --debug hmr\` to log its path.`,
                            stack: ''
                        }
                    })
                    return
                }
            }
            send(payloadOrEvent)
        }
    }
})
