import { configurableMessages } from '@greendrake/ui'

// Every user-facing string in this package: the failure toast's, and the two
// readings of calls not getting through (see useConnectionMessage). The toast's
// OK and Cancel are @greendrake/ui's own.
const defaults = {
    errorOther: 'Oops.. Sorry, something went wrong',
    errorNetwork: 'Looks like a network connectivity issue',
    errorNotFound: 'The requested page not found',
    tryAgain: 'Try again',
    trying: 'Trying...',
    toHomePage: 'To the home page',
    deviceOffline: 'You appear to be offline — waiting for your connection…',
    unreachable: 'Cannot reach the site — waiting for the connection…'
}

export type ApiMessages = typeof defaults

export const { messages: apiMessages, configure: configureApiMessages } = configurableMessages(defaults)
