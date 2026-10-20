import { TOKEN_PARAM, WebSocketPipe, WsTransport } from '@greendrake/rpc'
import { ApiClient, configureApiUX } from '@greendrake/vue-api'
import { keepServiceStates } from '@greendrake/dash/service-state'
import type { ServiceStateMethods, ServiceStatePushes } from '@greendrake/service-state'

declare const __WS_URL__: string
declare const __ADMIN_API_KEY__: string

// Everything goes over one socket: the calls, and the document of every
// service's state that arrives without being asked for. The credential rides
// the handshake URL — a browser cannot put a header on a WebSocket — so a
// reconnect re-authenticates on its own and needs no in-band handshake.
export const transport = new WsTransport<Record<never, never>, ServiceStatePushes>({
    pipe: new WebSocketPipe({
        url: __WS_URL__,
        queryAuth: { param: TOKEN_PARAM, token: __ADMIN_API_KEY__ }
    })
})

// The transport knows whether calls can currently succeed; without this the
// offline handling would be left guessing from navigator.onLine.
configureApiUX({ connectionSource: transport })

export const api = new ApiClient<ServiceStateMethods>({ transport })

// Kept from before the socket opens, so the document it is greeted with lands
// here; then the socket is opened, which nothing else would do — the controls
// call nothing until a button is pressed.
export const states = keepServiceStates(transport)
void transport.connect()
