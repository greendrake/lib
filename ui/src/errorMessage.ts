/** Human-readable message from a caught value, for local (non-toast) error display. */
export const errorMessage = (e: unknown): string => (e instanceof Error ? e.message : String(e))
