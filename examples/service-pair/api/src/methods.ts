import { z } from 'zod'
import { SERVICE_NOT_FOUND, serviceStateMethods } from '@greendrake/service-state/server'
import type { ServiceStateBoard } from '@greendrake/service-state/server'
import { ApiError } from '@greendrake/rpc-server'
import type { MethodAccess, MethodDefs } from '@greendrake/rpc-server'
import type { ApiMethods } from './contract'
import type { MockService } from './mock'

// The one principal this API knows: whoever holds the operator key.
export const OPERATOR = 'operator'
export type Operator = typeof OPERATOR

// Every method is the operator's, watching and commanding alike: the key is the
// only credential there is, so holding it is all there is to ask.
const OPERATOR_ONLY: MethodAccess<Operator> = { auth: 'required' }

// The arguments of the methods beyond the shared service-state contract, each
// naming the mocked service it acts on: where argument validation is shown —
// zod here, though any Standard Schema validator plugs into the same field.
const failNext = z.object({
    service: z.string(),
    command: z.enum(['start', 'stop']),
    at_once: z.boolean().default(false)
})
const named = z.object({ service: z.string() })

// Typed by the contract its clients are typed by: a method declared there has
// to be defined here, with the argument and the result declared there.
export const apiMethods = (board: ServiceStateBoard, mocks: ReadonlyMap<string, MockService>): MethodDefs<ApiMethods, Operator> => {
    const mockOf = (service: string): MockService => {
        const mock = mocks.get(service)
        if (!mock) {
            throw new ApiError(SERVICE_NOT_FOUND)
        }
        return mock
    }
    return {
        ...serviceStateMethods(board, { watch: OPERATOR_ONLY, command: OPERATOR_ONLY }),
        'mock.fail_next': {
            ...OPERATOR_ONLY,
            args: failNext,
            handler: (_ctx, args) => mockOf(args.service).failNext(args.command, args.at_once)
        },
        'mock.reads': {
            ...OPERATOR_ONLY,
            args: named,
            handler: (_ctx, args) => mockOf(args.service).reads()
        }
    }
}
