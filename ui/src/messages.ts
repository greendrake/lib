import { reactive } from 'vue'

// A package's user-facing strings, overridable in one place: `messages`, read
// reactively, starts as `defaults`; `configure` replaces any of them. Apps with
// i18n call it at boot and again on each locale change.
export const configurableMessages = <M extends Record<string, string>>(defaults: M): { messages: M; configure: (overrides: Partial<M>) => void } => {
    const messages = reactive({ ...defaults }) as M
    return {
        messages,
        configure: overrides => {
            Object.assign(messages, overrides)
        }
    }
}

// Every user-facing string in this package.
const defaults = {
    loading: 'Loading',
    ok: 'OK',
    cancel: 'Cancel',
    close: 'Close',
    clear: 'Clear',
    // {seconds} is replaced with the countdown value at render time.
    retryCountdown: 'Error. Retrying in {seconds}',
    nothingFound: 'Nothing found',
    searchResults: 'Search results',
    search: 'Search',
    create: 'Create',
    refresh: 'Refresh',
    delete: 'Delete',
    deleting: 'Deleting...',
    newTab: 'New tab',
    copied: 'Copied',
    clickToCopy: 'Click to copy',
    invalidEmail: 'Not a valid email address',
    selectOption: 'Select an option',
    // The accessible name of InfoTip's mark; what it explains is the tooltip.
    aboutThis: 'About this'
}

export type UiMessages = typeof defaults

export const { messages, configure: configureUiMessages } = configurableMessages(defaults)
