import { HttpError, NetworkError } from '@greendrake/util'

// Shared XHR wiring: percentage progress + settle-on-done. XHR (not fetch)
// because fetch exposes no upload progress events. Settles as
// @greendrake/util's fetchJson does: a 2xx resolves, any other status rejects
// with HttpError, a request that got no answer (network, CORS, timeout) with
// NetworkError — and an abort with an AbortError DOMException.
export const wireXhr = (xhr: XMLHttpRequest, onProgress: (percent: number) => void, resolve: (response: unknown) => void, reject: (reason: unknown) => void, isUpload: boolean): void => {
    const progressSource = isUpload ? xhr.upload : xhr
    progressSource.addEventListener('progress', (event: ProgressEvent) => {
        if (event.lengthComputable) {
            const done = Math.floor((100 * event.loaded) / event.total)
            if (done !== 100) {
                onProgress(done)
            }
        }
    })
    xhr.addEventListener('load', () => {
        if (xhr.status >= 200 && xhr.status < 300) {
            onProgress(100)
            resolve(xhr.response)
        } else {
            reject(new HttpError(xhr.status, xhr.statusText))
        }
    })
    xhr.addEventListener('error', () => reject(new NetworkError('the request got no answer')))
    xhr.addEventListener('timeout', () => reject(new NetworkError('the request timed out')))
    xhr.addEventListener('abort', () => reject(new DOMException('the request was aborted', 'AbortError')))
}
