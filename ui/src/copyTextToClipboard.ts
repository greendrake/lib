// Requires a secure context (https/localhost), which every deployment target
// provides. Rejects when the document is not focused or permission is denied.
export const copyTextToClipboard = (text: string): Promise<void> => navigator.clipboard.writeText(text)
