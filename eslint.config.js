// One flat config for the whole repository: every package resolves it by
// ESLint's upward lookup, so packages carry no config of their own.
import greendrake from '@greendrake/eslint-config'

export default greendrake
