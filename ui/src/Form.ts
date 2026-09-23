import { computed, ref, shallowRef, type Ref, type ComputedRef } from 'vue'
import { Retrier } from '@greendrake/util'
import { messages } from './messages'

// Rest states derive from validity/dirtiness; dictated states are imposed by
// an in-flight or just-finished submission.
type RestState = 'invalid' | 'dirty' | 'saved'
type DictatedState = 'confirm' | 'inprogress' | 'success' | 'error'
export type FormState = RestState | DictatedState

export interface FormOptions {
    valid?: Ref<boolean>
    dirty?: Ref<boolean>
    alwaysValid?: boolean
    alwaysDirty?: boolean
    // First action() enters the 'confirm' state; the next one (while it lasts)
    // actually submits.
    requireConfirmation?: boolean
    handler?: () => Promise<unknown>
    // Retry the handler indefinitely (Retrier) until it succeeds or the form
    // is destroyed; the countdown between attempts is surfaced via statusInfo.
    tryPersistently?: boolean
    // How long the respective dictated state lasts before returning to a rest
    // state: a number of ms, false to keep it forever (or, for success, to
    // skip the state entirely), undefined for the 2000ms default.
    successTimeout?: number | false
    confirmTimeout?: number | false
    errorTimeout?: number | false
}

const DEFAULT_STATE_TIMEOUT = 2000

// Form submission state machine: validity/dirtiness flow in as refs, action()
// runs the handler through the confirm/inprogress/success/error lifecycle.
export default class Form {
    readonly isValid: ComputedRef<boolean>
    readonly isDirty: ComputedRef<boolean>
    readonly state: ComputedRef<FormState>
    // Whether action() would do anything: only in the 'dirty' and 'confirm'
    // states is there something to submit.
    readonly isActionable: ComputedRef<boolean>
    // Retry countdown message while a persistent try is between attempts;
    // empty otherwise.
    readonly statusInfo: Ref<string> = ref('')

    readonly #options: FormOptions
    readonly #dictatedState = shallowRef<DictatedState | false>(false)
    #retrier?: Retrier<unknown>
    #timeout?: ReturnType<typeof setTimeout>
    #destroyed = false

    constructor(options: FormOptions = {}) {
        this.#options = options
        this.isValid = computed(() => options.alwaysValid || (options.valid ? options.valid.value : true))
        this.isDirty = computed(() => options.alwaysDirty || (options.dirty ? options.dirty.value : true))
        this.state = computed((): FormState => {
            if (this.#dictatedState.value !== false) {
                return this.#dictatedState.value
            }
            return this.isValid.value ? (this.isDirty.value ? 'dirty' : 'saved') : 'invalid'
        })
        this.isActionable = computed(() => this.state.value === 'dirty' || this.state.value === 'confirm')
    }

    async action(): Promise<void> {
        if (!this.isActionable.value) {
            return
        }
        this.#clearTimeout()
        if (this.#options.requireConfirmation && this.state.value !== 'confirm') {
            this.#dictatedState.value = 'confirm'
        } else {
            this.#dictatedState.value = 'inprogress'
            try {
                if (this.#options.handler) {
                    if (this.#options.tryPersistently) {
                        this.#retrier = new Retrier({ attempt: this.#options.handler })
                        this.#retrier.on('trying', () => (this.statusInfo.value = ''))
                        this.#retrier.on('waiting', secondsLeft => (this.statusInfo.value = messages.retryCountdown.replace('{seconds}', String(secondsLeft))))
                        try {
                            await this.#retrier.run()
                        } finally {
                            this.#retrier = undefined
                            this.statusInfo.value = ''
                        }
                    } else {
                        await this.#options.handler()
                    }
                }
                this.#dictatedState.value = this.#options.successTimeout === false ? false : 'success'
            } catch {
                this.#dictatedState.value = 'error'
            }
        }
        if (this.#destroyed) {
            return
        }
        // Schedule the dictated state's expiry back to a rest state. Only
        // confirm/success/error can be current here: 'inprogress' always ends
        // when the handler settles (re-entry is barred by isActionable).
        const dictated = this.#dictatedState.value
        if (dictated !== false) {
            const timeoutValue = this.#options[`${dictated}Timeout`]
            if (timeoutValue !== false) {
                this.#timeout = setTimeout(() => {
                    this.#dictatedState.value = false
                }, timeoutValue ?? DEFAULT_STATE_TIMEOUT)
            }
        }
    }

    #clearTimeout(): void {
        if (this.#timeout) {
            clearTimeout(this.#timeout)
            this.#timeout = undefined
        }
    }

    destroy(): void {
        this.#destroyed = true
        this.#retrier?.stop()
        this.#clearTimeout()
    }
}
