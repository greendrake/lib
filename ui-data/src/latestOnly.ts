// A source of calls of which only the latest counts — a table's loads, a
// pane's reads. Each call to it starts one and hands back whether that one is
// still the latest, which it stops being as the next one starts: what an
// answer checks before it is applied, so that one landing after a later call
// was made is dropped rather than overwriting that call's answer.
export const latestOnly = (): (() => () => boolean) => {
    let started = 0
    return () => {
        const mine = ++started
        return () => mine === started
    }
}
