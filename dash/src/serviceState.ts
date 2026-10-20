// Its own entry, so a dashboard without a service-state backend never needs
// @greendrake/service-state (an optional peer) installed.
import { SERVICE_STATE_DOCUMENT, type ServiceStatePushes, type ServiceStates } from '@greendrake/service-state'
import type { PushMapConstraint, WsTransport } from '@greendrake/rpc'
import { keepDocument } from '@greendrake/vue-api'

export { default as ServiceStateControl } from './ServiceStateControl.vue'
export { configureServiceStateMessages, serviceStateMessages, type ServiceStateMessages } from './serviceStateMessages'

// Every service's state, as the backend's document has it, kept for the page's
// life. Called where the host makes its transport, before connecting it: the
// socket is sent the whole document only as it opens. Every control on the
// socket shows its service from this one copy.
export const keepServiceStates = <PEvents extends PushMapConstraint<PEvents>, PData extends ServiceStatePushes & PushMapConstraint<PData>>(transport: WsTransport<PEvents, PData>): ServiceStates => keepDocument(transport, SERVICE_STATE_DOCUMENT, {})
