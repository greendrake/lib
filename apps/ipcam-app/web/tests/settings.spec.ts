import { expect, test } from '@playwright/test'
import { banner, call, closeSettings, fakeCommands, fakeTitle, holdingFake, loadSettings, openGroup, openMenu, openSettings, overlayFake, rebootPause, settings, settingsItem, streamChoice } from './app'

test('the overlay shows where the camera draws its name and the date and time, and a new name is drawn', async ({ page }) => {
    await loadSettings(page, 'Overlay')
    const overlay = page.locator('#overlay')
    const name = overlay.getByLabel('Camera name', { exact: true })
    await expect(name).toHaveValue('pond')
    await expect(overlay.getByLabel('Name down (%)', { exact: true })).toHaveValue('94.34')
    await expect(overlay.getByLabel('Date and time across (%)', { exact: true })).toHaveValue('80.35')
    await expect(overlay.getByLabel('Show the date and time', { exact: true })).toBeChecked()
    await name.fill('porch')
    await name.press('Enter')
    await expect(overlay.getByText('Saved')).toBeVisible()
    const title = await fakeTitle()
    expect(title.Height).toBe(20)
    expect(title.Width % 8).toBe(0)
    await loadSettings(page, 'Overlay')
    await expect(name).toHaveValue('porch')
})

test('the settings open on the first group, and opened again, on the group they showed last', async ({ page }) => {
    await page.goto('/')
    await openSettings(page)
    await expect(page.locator('#general')).toBeVisible()
    await openGroup(page, 'Network')
    await expect(page.locator('#network').getByLabel('IP address', { exact: true })).toHaveValue('192.0.2.148')
    await closeSettings(page)
    await expect(settings(page)).toHaveCount(0)
    await openSettings(page)
    await expect(page.locator('#network')).toBeVisible()
})

test('settings opened while the camera is offline wait for it, and show the first group once it is back', async ({ page }) => {
    await call('camera.reboot', { Camera: 'camera' })
    await page.goto('/')
    await openSettings(page)
    await expect(banner(page)).toHaveAttribute('data-state', 'offline')
    await expect(banner(page)).toHaveAttribute('data-state', 'online', { timeout: rebootPause + 5_000 })
    await expect(page.locator('#general .SettingsSection__field').first()).toBeVisible()
})

test('while a group is read, its tab shows nothing but its own spinner', async ({ page }) => {
    await loadSettings(page, 'General')
    const section = page.locator('#ntp')
    await holdingFake('NetWork.NetNTP', async () => {
        await openGroup(page, 'NTP')
        await expect(section).toHaveClass(/\bspinner\b/)
        await expect(section).toBeEmpty()
        await expect(page.locator('body')).not.toHaveClass(/\bspinner\b/)
    })
    await expect(section.getByLabel('Port', { exact: true })).toHaveValue('123')
    await expect(section).not.toHaveClass(/\bspinner\b/)
})

test('the groups show what the camera has', async ({ page }) => {
    await loadSettings(page, 'Network')
    await expect(page.locator('#network').getByLabel('IP address', { exact: true })).toHaveValue('192.0.2.148')
    await openGroup(page, 'NTP')
    await expect(page.locator('#ntp').getByLabel('Port', { exact: true })).toHaveValue('123')
    await openGroup(page, 'Encode')
    // The camera's SmartEncode is the main stream's alone.
    await expect(page.locator('#encode').getByLabel('Main stream SmartEncode', { exact: true })).not.toBeChecked()
    await expect(page.locator('#encode').getByLabel('Extra stream SmartEncode', { exact: true })).toHaveCount(0)
})

test('a group is read from the camera once its tab first opens, and kept', async ({ page }) => {
    await page.goto('/')
    const reads = await fakeCommands('NetWork.NetNTP')
    await openSettings(page)
    await expect(page.locator('#general .SettingsSection__field').first()).toBeVisible()
    expect(await fakeCommands('NetWork.NetNTP')).toBe(reads)
    await openGroup(page, 'NTP')
    await expect(page.locator('#ntp').getByLabel('Port', { exact: true })).toHaveValue('123')
    expect(await fakeCommands('NetWork.NetNTP')).toBe(reads + 1)
    await openGroup(page, 'General')
    await openGroup(page, 'NTP')
    await expect(page.locator('#ntp').getByLabel('Port', { exact: true })).toHaveValue('123')
    expect(await fakeCommands('NetWork.NetNTP')).toBe(reads + 1)
})

// Each reads its write back on a page loaded anew, which reads the camera afresh.

test('a switch is written as soon as it is flipped', async ({ page }) => {
    await loadSettings(page, 'NTP')
    const ntp = page.locator('#ntp').getByLabel('NTP', { exact: true })
    const enabled = await ntp.isChecked()
    await ntp.click()
    await expect(page.locator('#ntp').getByText('Saved')).toBeVisible()
    await loadSettings(page, 'NTP')
    await expect(ntp).toBeChecked({ checked: !enabled })
})

test('a text is written once committed', async ({ page }) => {
    const server = `ntp-${Date.now()}.example`
    await loadSettings(page, 'NTP')
    const field = page.locator('#ntp').getByLabel('Server', { exact: true })
    await field.fill(server)
    await field.press('Enter')
    await expect(page.locator('#ntp').getByText('Saved')).toBeVisible()
    await loadSettings(page, 'NTP')
    await expect(field).toHaveValue(server)
})

test('a setting explains itself on its tip', async ({ page }) => {
    await loadSettings(page, 'Camera')
    const agc = page.locator('#camera .SettingsSection__field').filter({ has: page.getByText('AGC', { exact: true }) })
    await agc.getByRole('button', { name: 'About this' }).hover()
    await expect(page.getByRole('tooltip')).toContainText('Automatic gain control')
})

test('a group stays as it was while another group shows', async ({ page }) => {
    await loadSettings(page, 'NTP')
    const port = page.locator('#ntp').getByLabel('Port', { exact: true })
    // Not a port, so never written: only the page holds it.
    await port.fill('70000')
    await openGroup(page, 'General')
    await openGroup(page, 'NTP')
    await expect(port).toHaveValue('70000')
})

test('a stream on another codec is named and not offered, and the encoding settings the banner opens switch it back once the camera reboots', async ({ page }) => {
    // The camera has the extra stream on H.264 from its next login on.
    await overlayFake('Simplify.Encode', [{ ExtraFormat: { Video: { Compression: 'H.264' } } }])
    await call('camera.reboot', { Camera: 'camera' })

    await page.goto('/')
    await openSettings(page)
    await expect(banner(page)).toHaveAttribute('data-state', 'codec', { timeout: rebootPause + 5_000 })
    await closeSettings(page)
    // The tile plays the one stream left to watch, offering no other.
    await openMenu(page)
    await expect(page.getByRole('switch', { name: 'Sound' })).toBeVisible()
    await expect(streamChoice(page)).toHaveCount(0)
    await settingsItem(page).click()
    await banner(page).getByRole('button', { name: 'Encode settings' }).click()
    const section = page.locator('#encode')
    await expect(section).toBeVisible()

    await section.getByRole('button', { name: 'Switch to H.265' }).click()
    await expect(banner(page)).toHaveAttribute('data-state', 'restart')
    await banner(page).getByRole('button', { name: 'Reboot' }).click()
    await expect(banner(page)).toHaveAttribute('data-state', 'offline')
    await expect(banner(page)).toHaveAttribute('data-state', 'online', { timeout: rebootPause + 5_000 })
    await closeSettings(page)
    await openMenu(page)
    await expect(streamChoice(page)).toBeVisible()
})
