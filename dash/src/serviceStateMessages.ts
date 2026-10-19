import { configurableMessages } from '@greendrake/ui'

// The service-state control's strings: the label of each command a state
// offers (see @greendrake/service-state's CONTROLS), keyed by the command.
const defaults = {
    start: 'Turn ON',
    stop: 'Turn OFF',
    refresh: 'Refresh'
}

export type ServiceStateMessages = typeof defaults

export const { messages: serviceStateMessages, configure: configureServiceStateMessages } = configurableMessages(defaults)
