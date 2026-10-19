// @ts-check
// The stack the suite runs against, which playwright.config.js starts: a fake camera of
// each protocol, and the app's binary logged in to each. Each value can be overridden
// through the environment, so that a second run on one machine takes ports of its own.
const setting = (name, fallback) => process.env[name] ?? fallback

export const stack = {
    /** Where the app logged in to the DVRIP camera serves. */
    app: setting('CAMERA_LISTEN', '127.0.0.1:8096'),
    /** The fake DVRIP camera's address, and the recorded device it answers as. */
    camera: setting('FAKECAM_LISTEN', '127.0.0.1:34568'),
    device: setting('FAKECAM_DEVICE', 'techage-hi3516ev100'),
    /** How long a reboot keeps the camera unreachable: longer than the app's one-second redial, so that the suite sees it offline. */
    rebootSeconds: Number(setting('FAKECAM_REBOOT_SECONDS', '3')),
    /** The configurations whose writes take a restart, as a codec change does on the cameras. */
    restart: setting('FAKECAM_RESTART', 'Simplify.Encode'),
    /** The endpoint a spec changes the fake DVRIP camera through. */
    control: setting('FAKECAM_CONTROL', '127.0.0.1:34581'),
    /** Where the app logged in to the RTSP camera serves. */
    rtspApp: setting('RTSP_CAMERA_LISTEN', '127.0.0.1:8097'),
    /** The fake RTSP camera's address. */
    rtspCamera: setting('RTSP_FAKECAM_LISTEN', '127.0.0.1:8554')
}
