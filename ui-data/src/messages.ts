import { configurableMessages } from '@greendrake/ui/messages'

// The words this package renders itself.
const defaults = {
    yes: 'yes',
    no: 'no'
}

export type UiDataMessages = typeof defaults

export const { messages, configure: configureUiDataMessages } = configurableMessages(defaults)
