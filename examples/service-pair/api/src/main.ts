import { startApi } from './service'

const port = Number(process.env.API_PORT)
const adminKey = process.env.ADMIN_API_KEY
if (!port || !adminKey) {
    throw new Error('API_PORT and ADMIN_API_KEY must be set')
}

await startApi({ port, adminKey })
