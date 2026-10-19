import { ApiError } from '@greendrake/rpc'
import { errorMessage } from '@greendrake/ui'

/** The details of a failure the engine words itself (greendrake.nz/go/ipcamrpc's Problem). */
export interface Problem {
    Message: string
}

// The codes whose details are a Problem.
const WORDED = new Set(['UNKNOWN_CAMERA', 'NO_SETTINGS', 'CAMERA_OFFLINE', 'CAMERA_REFUSED', 'UNKNOWN_SETTING', 'UNKNOWN_GROUP', 'INVALID_VALUE'])

/** What a failure says where it is shown: the engine's own words for one it words, the error's message for any other. */
export const wording = (e: unknown): string => (e instanceof ApiError && WORDED.has(e.code) ? (e.details as Problem).Message : errorMessage(e))

/** action, failing in the words wording gives its failure: for what shows a failure's message as it is. */
export const inWords =
    <A extends unknown[], R>(action: (...args: A) => Promise<R>): ((...args: A) => Promise<R>) =>
    async (...args) => {
        try {
            return await action(...args)
        } catch (e) {
            throw new Error(wording(e), { cause: e })
        }
    }
