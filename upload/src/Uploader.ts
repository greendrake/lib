import { Emitter, HttpError, Retrier, RetryStopped } from '@greendrake/util'
import { wireXhr } from './xhrProgress'

export interface UploaderOptions {
    url: string
    file: Blob
    // The content type sent on the request. For presigned-URL PUTs it must
    // match the type the URL was signed for, so it is required — the caller
    // decides (e.g. via fileMime), never a silent guess.
    mime: string
    verb?: 'PUT' | 'POST'
    // Error gate for the retry loop; an HTTP failure arrives as
    // @greendrake/util's HttpError, a request that got no answer as its
    // NetworkError. Default: retry everything except a 403 (an
    // expired/invalid presigned URL never heals by retrying).
    shouldRetry?: (reason: unknown) => boolean
}

export interface UploaderEvents {
    progress: [percent: number]
    finished: []
    cancelled: []
    [key: string]: unknown[]
}

export class Uploader extends Emitter<UploaderEvents> {
    readonly name?: string
    readonly #options: UploaderOptions
    readonly #blob: Blob
    #xhr?: XMLHttpRequest
    #retrier?: Retrier<void>
    #fullDone = false

    constructor(options: UploaderOptions) {
        super()
        this.#options = options
        this.name = options.file instanceof File ? options.file.name : undefined
        this.#blob = new Blob([options.file], { type: options.mime })
    }

    async send(): Promise<void> {
        this.#retrier = new Retrier({
            attempt: () =>
                new Promise<void>((resolve, reject) => {
                    const xhr = new XMLHttpRequest()
                    this.#xhr = xhr
                    wireXhr(
                        xhr,
                        percent => {
                            if (percent === 100) {
                                this.#fullDone = true
                            }
                            this.emit('progress', percent)
                        },
                        () => resolve(),
                        reject,
                        true
                    )
                    xhr.open(this.#options.verb ?? 'PUT', this.#options.url)
                    xhr.setRequestHeader('Content-Type', this.#options.mime)
                    xhr.send(this.#blob)
                }).finally(() => {
                    this.#xhr = undefined
                }),
            shouldRetry: this.#options.shouldRetry ?? (reason => !(reason instanceof HttpError && reason.status === 403))
        })
        try {
            await this.#retrier.run()
        } catch (e) {
            if (e instanceof RetryStopped) {
                this.emit('cancelled')
                return
            }
            throw e
        } finally {
            this.#retrier = undefined
        }
        if (!this.#fullDone) {
            this.#fullDone = true
            this.emit('progress', 100)
        }
        this.emit('finished')
    }

    abort(): void {
        if (!this.#fullDone) {
            this.#retrier?.stop()
            this.#xhr?.abort()
        }
    }
}
