import { z } from 'zod'
import { serviceStateMethods } from '@greendrake/service-state/server'
import type { ServiceStateMachine } from '@greendrake/service-state/server'
import { method } from '@greendrake/rpc-server'
import type { ClientMethods, MethodAccess } from '@greendrake/rpc-server'
import type { MockService } from './mock'

// The one principal this API knows: whoever holds the operator key.
export const OPERATOR = 'operator'
export type Operator = typeof OPERATOR

// Every method is the operator's: the key is the only credential there is, so
// holding it is all there is to ask.
const OPERATOR_ONLY: MethodAccess<Operator> = { auth: 'required' }

// The one method beyond the shared service-state contract: a hook for reaching
// ERROR without breaking anything. It takes an argument, so it is also where
// argument validation is shown — zod here, though any Standard Schema
// validator plugs into the same field.
const failNext = z.object({ command: z.enum(['start', 'stop']) })

export const apiMethods = (machine: ServiceStateMachine, mock: MockService) => ({
    ...serviceStateMethods([machine], OPERATOR_ONLY),
    'mock.fail_next': method({
        ...OPERATOR_ONLY,
        args: failNext,
        handler: (_ctx, args) => mock.failNext(args.command)
    })
})

// What a client hands its ApiClient: the whole surface, derived from the
// definitions rather than restated. A method whose argument shape or result
// changes here stops compiling in whatever calls it.
export type ApiMethods = ClientMethods<ReturnType<typeof apiMethods>>
