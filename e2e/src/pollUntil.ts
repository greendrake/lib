// Poll an async producer until the predicate returns a truthy match, and
// return that match. For verifying fire-and-forget writes (visit tracking,
// queued tasks): the row lands asynchronously, so a single fetch races the
// write and a wait-and-retry is required. The default budget covers the
// visit lib's error debouncer (1500ms) plus network slack.
//
// Exposed as the `@greendrake/e2e/poll` subpath (not the barrel): the
// barrel pulls in @playwright/test via the config factory, and importing
// that from a spec loads a SECOND @playwright/test copy in apps that pin
// a different playwright version — a hard collection error.
export const pollUntil = async <T, M>(produce: () => Promise<T>, predicate: (value: T) => M | undefined, budgetMs = 5000, intervalMs = 200): Promise<M> => {
    const deadline = Date.now() + budgetMs
    for (;;) {
        const match = predicate(await produce())
        if (match) return match
        if (Date.now() >= deadline) {
            throw new Error(`pollUntil: predicate never satisfied within ${budgetMs}ms`)
        }
        await new Promise(resolve => setTimeout(resolve, intervalMs))
    }
}
