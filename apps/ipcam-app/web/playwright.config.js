// @ts-check
import { spaPlaywright } from '@greendrake/e2e'
import { stack } from './tests/stack.js'

const portOf = address => Number(address.slice(address.lastIndexOf(':') + 1))

// A fake camera, a command of a Go module the server's module requires.
const fake = (command, listen) => ({
    command: `go run ${command.join(' ')}`,
    cwd: '../server',
    port: portOf(listen),
    reuseExistingServer: false
})

// The app as users run it, the binary ../server/build leaves, logged in to the camera
// its arguments end with.
const app = (listen, args) => ({
    command: `./ipcam-app -listen ${listen} ${args.join(' ')}`,
    cwd: '../server',
    url: `http://${listen}`,
    reuseExistingServer: false
})

// The suite starts what it tests: a fake camera of each protocol, then the app on each.
export default spaPlaywright({
    baseURL: `http://${stack.app}`,
    overrides: {
        // One fake DVRIP camera, whose settings and reboots every spec shares.
        fullyParallel: false,
        workers: 1,
        reporter: 'list',
        webServer: [
            fake(
                [
                    'greendrake.nz/go/dvrip/cmd/fakecam',
                    `-device ${stack.device}`,
                    `-listen ${stack.camera}`,
                    `-reboot ${stack.rebootSeconds}s`,
                    `-restart ${stack.restart}`,
                    `-control ${stack.control}`,
                    '-realtime'
                ],
                stack.camera
            ),
            app(stack.app, [stack.camera]),
            fake(['greendrake.nz/go/ipcam/rtspcam/fakecam', `-listen ${stack.rtspCamera}`], stack.rtspCamera),
            app(stack.rtspApp, ['-protocol rtsp', stack.rtspCamera])
        ]
    }
})
