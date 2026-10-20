import { expect, test } from '@playwright/test'
import { HttpTransport, RpcClient } from '@greendrake/rpc'

const API = `http://localhost:${process.env.API_PORT ?? '8100'}/v1/api`
const ADMIN_KEY = 'example-admin-key'

// The open tab's control: a panel is mounted only while its tab is open.
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
    // from here on, and follows along on the document alone.
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
    await api.call('mock.fail_next', {
        service: 'web',
        command: 'start',
        at_once: false
    })
    await control(page).getByRole('button', { name: 'Turn ON' }).click()
    await isIn(page, 'ERROR')
    await expect(control(page)).toContainText('mock service failed to start')

    await control(page).getByRole('button', { name: 'Refresh' }).click()
    await isIn(page, 'OFF')
})

// The failure is published before the answer to the click that caused it, and
// the control takes its state from the document alone.
test('a start that fails at once lands in ERROR', async ({ page }) => {
    await page.goto('/')
    await isIn(page, 'OFF')

    await api.call('mock.fail_next', {
        service: 'web',
        command: 'start',
        at_once: true
    })
    await control(page).getByRole('button', { name: 'Turn ON' }).click()
    await isIn(page, 'ERROR')
    await expect(control(page)).toContainText('mock service failed to start')

    await control(page).getByRole('button', { name: 'Refresh' }).click()
    await isIn(page, 'OFF')
})

test("the second tab's control, mounted after the socket connected, shows its service's state", async ({ page }) => {
    // The worker is moved apart from the web service, so what the second tab
    // shows can only be the worker's.
    await api.call('service.start', { service: 'worker' })
    await page.goto('/')
    await isIn(page, 'OFF')

    await page.locator('.TabBar__tab').getByText('Worker', { exact: true }).click()
    await isIn(page, 'ON')

    await control(page).getByRole('button', { name: 'Turn OFF' }).click()
    await isIn(page, 'OFF')
})
