import { z } from 'zod'
import { SERVICE_NOT_FOUND, serviceStateMethods } from '@greendrake/service-state/server'
import type { ServiceStateBoard } from '@greendrake/service-state/server'
import { ApiError, method } from '@greendrake/rpc-server'
import type { ClientMethods, MethodAccess } from '@greendrake/rpc-server'
import type { MockService } from './mock'

// The one principal this API knows: whoever holds the operator key.
export const OPERATOR = 'operator'
export type Operator = typeof OPERATOR

// Every method is the operator's: the key is the only credential there is, so
// holding it is all there is to ask.
const OPERATOR_ONLY: MethodAccess<Operator> = { auth: 'required' }

// The one method beyond the shared service-state contract: a hook for reaching
// ERROR without breaking anything, on the service it names, once its
// transition's delay is up or at once. It takes an argument, so it is also
// where argument validation is shown — zod here, though any Standard Schema
// validator plugs into the same field.
const failNext = z.object({
    service: z.string(),
    command: z.enum(['start', 'stop']),
    at_once: z.boolean().default(false)
})

export const apiMethods = (board: ServiceStateBoard, mocks: ReadonlyMap<string, MockService>) => ({
    ...serviceStateMethods(board, OPERATOR_ONLY),
    'mock.fail_next': method({
        ...OPERATOR_ONLY,
        args: failNext,
        handler: (_ctx, args) => {
            const mock = mocks.get(args.service)
            if (!mock) {
                throw new ApiError(SERVICE_NOT_FOUND)
            }
            mock.failNext(args.command, args.at_once)
        }
    })
})

// What a client hands its ApiClient: the whole surface, derived from the
// definitions rather than restated. A method whose argument shape or result
// changes here stops compiling in whatever calls it.
export type ApiMethods = ClientMethods<ReturnType<typeof apiMethods>>
