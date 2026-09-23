import { defineStore } from 'pinia'

interface AppState {
    loadingCounter: number
    isFirstLoading: boolean
    hasNavigated: boolean
}

// Global loading state. The counter starts at 0; the app shell
// (@greendrake/vue-app) holds one explicit slot for the boot sequence and
// releases it when the first render settles. `isFirstLoading` distinguishes
// the boot splash from subsequent in-app spinners.
export const useAppState = defineStore('appstate', {
    state: (): AppState => ({
        loadingCounter: 0,
        isFirstLoading: true,
        // Whether the router has ever committed a navigation — i.e. whether
        // anything usable has been on screen. A one-way latch, set by
        // @greendrake/vue-app's afterEach. Distinct from isFirstLoading, which
        // answers "is the app still booting" and flips as soon as the loading
        // counter empties — which can happen with the screen still bare (a
        // call parked waiting for connectivity holds no slot).
        hasNavigated: false
    }),
    actions: {
        setLoading(on: boolean): void {
            if (on) {
                this.loadingCounter++
            } else {
                this.loadingCounter--
            }
            if (this.loadingCounter === 0 && this.isFirstLoading) {
                this.isFirstLoading = false
            }
        }
    },
    getters: {
        isLoading: (state): boolean => state.loadingCounter > 0
    }
})
