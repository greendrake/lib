import { createDash } from '@greendrake/dash'
import { transport } from '@/api'
import Camera from './Camera.vue'

// Connected at once: the camera's state arrives as a push, ahead of any call.
void transport.connect()

createDash({
    splash: ['CAMERA'],
    defaultTitle: 'Camera',
    routes: [{ path: '/', component: Camera }]
})
