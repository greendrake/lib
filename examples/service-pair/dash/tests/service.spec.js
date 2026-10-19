import { expect, test } from '@playwright/test'
import { HttpTransport, RpcClient } from '@greendrake/rpc'

const API = `http://localhost:${process.env.API_PORT ?? '8100'}/v1/api`
const ADMIN_KEY = 'example-admin-key'

const control = page => page.locator('.ServiceStateControl')

// The state is what the control is for, and it is on the element as
// `data-state` — the same fact the border colour is drawn from.
const isIn = async (page, state) => await expect(control(page)).toHaveAttribute('data-state', state)

// The API as an operator calls it, for the test setting the world up rather
// than anything the dashboard does.
const api = new RpcClient(new HttpTransport({ url: API, headers: () => ({ Authorization: `Bearer ${ADMIN_KEY}` }) }))

test('the service is turned on and off, and every state change is seen live', async ({ page, context }) => {
    await page.goto('/')
    await isIn(page, 'OFF')

    // A second dashboard, opened before anything happens: it asks for nothing
    // from here on, and follows along on the pushes alone.
    const onlooker = await context.newPage()
    await onlooker.goto('/')
    await isIn(onlooker, 'OFF')

    await control(page).getByRole('button', { name: 'Turn ON' }).click()
    await isIn(page, 'STARTING')
    await isIn(onlooker, 'STARTING')
    // Nothing to press mid-transition: there is no interfering with it.
    await expect(control(page).getByRole('button')).toHaveCount(0)

    await isIn(page, 'ON')
    await isIn(onlooker, 'ON')

    await control(page).getByRole('button', { name: 'Turn OFF' }).click()
    await isIn(page, 'STOPPING')
    await isIn(onlooker, 'STOPPING')
    await isIn(page, 'OFF')
    await isIn(onlooker, 'OFF')
})

test('a failed start shows why, and a refresh reads past it', async ({ page }) => {
    await page.goto('/')
    await isIn(page, 'OFF')

    // Arms the mocked service to fail its next transition.
    await api.call('mock.fail_next', { command: 'start' })
    await control(page).getByRole('button', { name: 'Turn ON' }).click()
    await isIn(page, 'ERROR')
    await expect(control(page)).toContainText('mock service failed to start')

    await control(page).getByRole('button', { name: 'Refresh' }).click()
    await isIn(page, 'OFF')
})
