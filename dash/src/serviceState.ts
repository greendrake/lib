// Its own entry, so a dashboard without a service-state backend never needs
// @greendrake/service-state (an optional peer) installed.
export { default as ServiceStateControl } from './ServiceStateControl.vue'
export { keepServiceStates, type ServiceStateKeeper } from './keeper'
export { configureServiceStateMessages, serviceStateMessages, type ServiceStateMessages } from './serviceStateMessages'
