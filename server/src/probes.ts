// One dependency this service cannot serve without, and how to ask it. The
// signal is already bounded by the probe deadline — a check must not add a
// timeout of its own on top.
export interface DependencyCheck {
    name: string
    check(signal: AbortSignal): Promise<void>
}

export interface CheckResult {
    ok: boolean
    // "ok", or "fail: <reason>" — the readiness body is what an operator reads
    // when a deploy stalls, so a failure says which dependency and why rather
    // than leaving a bare 503 to be guessed at.
    message: string
}

// Every check at once, each under the same deadline. The deadline is enforced
// here rather than left to the check: a dependency that has stopped answering
// typically stops answering its cancellation too, and a readiness probe that
// hangs is worse than one that fails.
export const runChecks = async (checks: DependencyCheck[], timeoutMs: number): Promise<Record<string, CheckResult>> => {
    const results = await Promise.all(
        checks.map(async ({ name, check }): Promise<[string, CheckResult]> => {
            const signal = AbortSignal.timeout(timeoutMs)
            const expiry = new Promise<never>((_, reject) => {
                signal.addEventListener('abort', () => reject(new Error(`timed out after ${timeoutMs}ms`)), { once: true })
            })
            try {
                await Promise.race([check(signal), expiry])
                return [name, { ok: true, message: 'ok' }]
            } catch (e) {
                return [name, { ok: false, message: `fail: ${(e as Error).message}` }]
            }
        })
    )
    return Object.fromEntries(results)
}

const json = (status: number, body: Record<string, unknown>): Response => new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json; charset=utf-8' } })

// Liveness: is this process still the process. It deliberately asks nothing of
// the dependencies — a database outage must not get the service killed and
// restarted into the same outage. 503 while draining, so a load balancer stops
// sending work before the sockets close.
export const healthz = (draining: boolean): Response => json(draining ? 503 : 200, { status: draining ? 'draining' : 'ok' })

// Readiness: can this process serve right now. Every dependency is probed, and
// the body names each one's verdict.
export const readyz = async (checks: DependencyCheck[], timeoutMs: number, draining: boolean): Promise<Response> => {
    const results = await runChecks(checks, timeoutMs)
    const body: Record<string, unknown> = Object.fromEntries(Object.entries(results).map(([name, result]) => [name, result.message]))
    if (draining) {
        body.draining = true
    }
    const ready = !draining && Object.values(results).every(result => result.ok)
    return json(ready ? 200 : 503, body)
}
