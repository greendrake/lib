import { toValue, watch as track, type MaybeRefOrGetter } from 'vue'
import { serviceStateDocument, type ServiceDocument, type ServiceStateDocumentType, type ServiceStateMethods, type ServiceStatePushes, type ServiceStatus } from '@greendrake/service-state'
import { RpcClient, type PushMapConstraint, type WsTransport } from '@greendrake/rpc'
import { keepTopicDocuments } from '@greendrake/vue-api'

export interface ServiceStateKeeper {
    // Watches the service named, for as long as it is held; returns the
    // release. Given a ref or getter it follows it: a change releases the
    // service held and watches the new one.
    watch(service: MaybeRefOrGetter<string>): () => void
    // The service's status, reactively: undefined until the backend has read it
    // with somebody watching, and while it is not watched.
    status(service: string): ServiceStatus | undefined
    // Why watching the service was refused, while it is watched.
    failure(service: string): Error | undefined
}

// The backend's services as the sockets watching them see them, each a
// document of its own (@greendrake/vue-api's keepTopicDocuments). Watching goes
// over `transport`, the socket the documents come on, by calls of the keeper's
// own — plain ones, holding nothing up across an outage — whatever client the
// host gives the controls for their commands. It can be made at any time, and
// the first watch opens the socket.
export const keepServiceStates = <PEvents extends PushMapConstraint<PEvents>, PData extends ServiceStatePushes & PushMapConstraint<PData>>(transport: WsTransport<PEvents, PData>): ServiceStateKeeper => {
    const client = new RpcClient<ServiceStateMethods>(transport)
    const documents = keepTopicDocuments<string, ServiceStateDocumentType, ServiceDocument, PEvents, PData>(transport, {
        typeOf: serviceStateDocument,
        empty: () => ({}),
        join: service => client.call('service.watch', { service }),
        leave: service => client.call('service.unwatch', { service })
    })
    return {
        watch: service => {
            let release = documents.hold(toValue(service))
            const stop = track(
                () => toValue(service),
                next => {
                    release()
                    release = documents.hold(next)
                }
            )
            return () => {
                stop()
                release()
            }
        },
        status: service => documents.doc(service)?.status,
        failure: service => documents.failure(service)
    }
}
