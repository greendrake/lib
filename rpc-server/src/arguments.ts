import type { StandardSchemaV1 } from '@standard-schema/spec'
import { ApiError } from './errors'

// A method takes one argument, and the envelope carries it two ways: wrapped
// in an array, which is what @greendrake/rpc sends, or as the value itself,
// which is what a hand-written caller or another language's client tends to
// send. Both unwrap to the same thing.
export const unwrapArguments = (raw: unknown): unknown => {
    if (raw === undefined || raw === null) {
        return undefined
    }
    return Array.isArray(raw) ? raw[0] : raw
}

// Validates the unwrapped argument against a method's schema. A failure is an
// ApiError carrying every issue, so a form can mark each offending field
// rather than showing one message for the whole submission.
export const validateArguments = async <A>(schema: StandardSchemaV1<unknown, A>, value: unknown): Promise<A> => {
    const result = await schema['~standard'].validate(value)
    if (result.issues) {
        // An issue's path — `['filters', 0, 'from']` — becomes the dotted name a
        // form field is keyed by. Segments arrive bare or wrapped in a `{ key }`
        // object; both name the same step.
        throw new ApiError(
            'INVALID_ARGUMENT',
            result.issues.map(issue => ({
                field: (issue.path ?? []).map(segment => String(typeof segment === 'object' ? segment.key : segment)).join('.'),
                message: issue.message
            }))
        )
    }
    return result.value
}
