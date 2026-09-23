// Single source of truth for the wire shapes. The protocol:
// request  {method, nonce?, arguments?}  (nonce = server-side dedup key for
//          non-idempotent calls; arguments omitted when empty)
// response {success, data?, error?, error_details?}
// WS adds an `id` to both directions for request/response correlation, and
// server-push frames {type: 'event', event, data} / {type, data}.

export interface RpcRequest {
    method: string
    nonce?: string
    // The caller's UI language tag, when the app supplies a provider. In the
    // envelope rather than a header: the WebSocket transport has none, and the
    // browser cannot set any on the handshake, while every call rides that
    // socket.
    locale?: string
    arguments?: unknown[]
    [key: string]: unknown
}

export interface RpcResponse {
    success: boolean
    data?: unknown
    error?: string
    error_details?: unknown
}

// Payload codec applied to outbound request bodies and inbound response text.
// Identity by default; an app supplies its own for an obscured or encrypted wire.
export interface Codec {
    encode(s: string): string
    decode(s: string): string
}

export const identityCodec: Codec = {
    encode: s => s,
    decode: s => s
}

// Extra request headers, evaluated per request so time-variant values (auth
// tokens, identity blobs) are always fresh.
export type HeadersProvider = () => Record<string, string>

// App-declared RPC surface: method name → call signature. Enables fully typed
// client.call('user.get', id) with inferred argument and result types.
//    interface Methods { 'user.get': (id: string) => User }
// The `never[]` parameter constraint deliberately accepts any concrete
// signature (contravariance) without resorting to `any`. Generic constraints
// use the `Record<keyof M, …>` form so plain interfaces (which have no
// implicit index signature) qualify.
export type MethodMap = Record<string, (...args: never[]) => unknown>

export type MethodMapConstraint<M> = Record<keyof M, (...args: never[]) => unknown>

export interface Transport {
    send(request: RpcRequest): Promise<unknown>
}
