import { reactive } from 'vue'

// Every user-facing string in this package, overridable in one place. Apps
// with i18n call configureUiMessages at boot (and again on locale change).
const defaults = {
    loading: 'Loading',
    ok: 'OK',
    cancel: 'Cancel',
    close: 'Close',
    clear: 'Clear',
    tryAgain: 'Try again',
    trying: 'Trying...',
    // {seconds} is replaced with the countdown value at render time.
    retryCountdown: 'Error. Retrying in {seconds}',
    toHomePage: 'To the home page',
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
    errorOther: 'Oops.. Sorry, something went wrong',
    errorNetwork: 'Looks like a network connectivity issue',
    errorNotFound: 'The requested page not found'
}

export type UiMessages = typeof defaults

export const messages: UiMessages = reactive({ ...defaults })

export const configureUiMessages = (overrides: Partial<UiMessages>): void => {
    Object.assign(messages, overrides)
}
