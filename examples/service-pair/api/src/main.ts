import { startApi } from './service'

const port = Number(process.env.API_PORT)
const adminKey = process.env.ADMIN_API_KEY
const dashOrigin = process.env.DASH_ORIGIN
if (!port || !adminKey || !dashOrigin) {
    throw new Error('API_PORT, ADMIN_API_KEY and DASH_ORIGIN must be set')
}

await startApi({
    port,
    adminKey,
    dashOrigin
})
