import { computed } from 'vue'
import { RpcClient, WebSocketPipe, WsTransport } from '@greendrake/rpc'
import { browserRuntime, resolveApiTarget } from '@greendrake/vite-preset/api-target'
import { cameraApi, useCameras, type Methods, type Pushes } from '@greendrake/ipcam'

// The camera binary, which serves /api and /streams beside the SPA: in production the
// page's own origin, in dev the binary on __API_PORT__, reached directly.
const { wsOrigin: backend } = resolveApiTarget({ dev: !import.meta.env.PROD, apiPort: __API_PORT__ }, browserRuntime())

export const transport = new WsTransport<Record<never, never>, Pushes>({ pipe: new WebSocketPipe({ url: `${backend}/api` }) })

const cameras = useCameras(transport)

// The binary's one camera, by the name it gives it: the one camera the cameras push has.
// The binary greets each socket with the push, so it is undefined only until the first
// one opens.
export const cameraName = computed(() => Object.keys(cameras)[0])

// The camera's state, known as its name is.
export const camera = computed(() => (cameraName.value === undefined ? undefined : cameras[cameraName.value]))

// Every call is made from a component of the camera's tile, which shows the call's
// progress and its failure where it was made, once the camera is known.
export const api = cameraApi(new RpcClient<Methods>(transport), () => {
    if (cameraName.value === undefined) {
        throw new Error('ipcam-app: a call on the camera before the cameras push named it')
    }
    return cameraName.value
})

export const streamHubURL = `${backend}/streams`
