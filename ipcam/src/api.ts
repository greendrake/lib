import { inject, provide, toValue, type InjectionKey, type MaybeRefOrGetter } from 'vue'
import type { Caller, PushMapConstraint, StateFrame, WsTransport } from '@greendrake/rpc'
import { keepDocument } from '@greendrake/vue-api'
import type { Cameras, Clock, Group, SetResult, Setting } from './types'

/** The methods on one camera, as the components call them. */
export interface CameraMethods {
    // The groups the camera has settings of.
    'settings.groups': () => Group[]
    // The settings the camera has of a group, read as they alone need.
    'settings.get': (args: { Group: string }) => Setting[]
    // One setting, its value in its kind's form.
    'settings.set': (args: { Key: string; Value: unknown }) => SetResult
    'camera.time': () => Clock
    'camera.setTime': (args: Clock) => true
    'camera.reboot': () => true
}

/** What every method's arguments have on the wire: the camera the call is on. */
export interface On {
    Camera: string
}

/** A camera method's arguments, none where it takes none. */
type ArgumentsOf<F> = F extends (args: infer A) => unknown ? A : Record<never, never>

/** The engine's methods (greendrake.nz/go/ipcamrpc), keyed as they are: each a camera's, on the camera its arguments name. */
export type Methods = { [K in keyof CameraMethods]: (args: On & ArgumentsOf<CameraMethods[K]>) => ReturnType<CameraMethods[K]> }

/** The pushes the engine makes: every camera's state, as a document. */
export interface Pushes {
    cameras: StateFrame<Cameras>
}

/** The engine's API bound to one camera. */
export type CameraApi = Caller<CameraMethods>

/** client's calls, each on the camera name names as it is called. */
export const cameraApi = (client: Caller<Methods>, name: MaybeRefOrGetter<string>): CameraApi => ({
    call: <K extends keyof CameraMethods & string>(method: K, ...args: Parameters<CameraMethods[K]>) => client.call(method, ...([{ ...args[0], Camera: toValue(name) }] as Parameters<Methods[K]>))
})

const cameraApiKey: InjectionKey<CameraApi> = Symbol('CameraApi')

/** Provides the components below with the API of the camera they show. */
export const provideCameraApi = (api: CameraApi): void => {
    provide(cameraApiKey, api)
}

/** The API of the camera the component shows, as provided above it. */
export const useCameraApi = (): CameraApi => {
    const api = inject(cameraApiKey)
    if (!api) {
        throw new Error('@greendrake/ipcam: no camera API is provided')
    }
    return api
}

/** Every camera's state, by name, as the engine publishes it on transport: kept for the page's life, so called before the transport connects. */
export const useCameras = <PEvents extends PushMapConstraint<PEvents>, PData extends Pushes & PushMapConstraint<PData>>(transport: WsTransport<PEvents, PData>): Cameras => keepDocument(transport, 'cameras', {})
