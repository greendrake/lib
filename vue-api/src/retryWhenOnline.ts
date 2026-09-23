import { NetworkError } from '@greendrake/util'
import { useConnectivity } from './connectivity'

// Re-runs `attempt` each time connectivity returns, for as long as the app is
// offline. For the fetch sites that reach the network WITHOUT going through
// ApiClient — static content, the i18n locale file — whose failure would
// otherwise be a dead end: nothing re-drives them, so an offline miss leaves
// the surface permanently empty even after the connection is back.
//
// ApiClient keeps its own integrated version of this loop rather than calling
// here: its parking interleaves with the loading windows, the retry-toast
// deferral and the in-flight carve-out, none of which apply to a plain fetch.
//
// Only network failures while the oracle corroborates an outage are retried;
// everything else (a 404, a parse error, a network failure the oracle does not
// see) propagates on the first attempt.
export const retryWhenOnline = async <T>(attempt: () => Promise<T>): Promise<T> => {
    const connectivity = useConnectivity()
    while (true) {
        try {
            return await attempt()
        } catch (e) {
            if (!(e instanceof NetworkError) || !connectivity.offline) {
                throw e
            }
            await connectivity.whenOnline()
        }
    }
}
