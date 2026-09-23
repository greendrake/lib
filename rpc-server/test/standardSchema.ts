import type { StandardSchemaV1 } from '@standard-schema/spec'

// A Standard Schema from a bare validate function. The package depends on no
// validator and its tests must not introduce one either — what they need to
// prove is that the interface is the whole requirement.
export const schema = <T>(validate: (value: unknown) => StandardSchemaV1.Result<T>): StandardSchemaV1<unknown, T> => ({
    '~standard': {
        version: 1,
        vendor: 'test',
        validate
    }
})
