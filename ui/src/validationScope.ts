import type { InjectionKey, Ref } from 'vue'

// Form components provide a template ref to their root element under this key
// so descendant TextFields can suppress blur-time validation when focus leaves
// the form (e.g. user clicks Sign In in the header without completing the form).
export const validationScopeKey: InjectionKey<Ref<HTMLElement | null>> = Symbol('validationScope')
