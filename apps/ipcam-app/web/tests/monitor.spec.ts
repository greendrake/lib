import { expect, test } from '@playwright/test'
import { menuTrigger, openMenu, playingState, settingsItem, streamChoice, video } from './app'

test('the picture fills the page', async ({ page }) => {
    await page.goto('/')
    const whole = {
        x: 0,
        y: 0,
        ...page.viewportSize()!
    }
    await expect.poll(() => video(page).boundingBox()).toEqual(whole)
})

test('the tile plays the extra stream, then the main one, where the browser plays H.265', async ({ page }) => {
    await page.goto('/')
    const state = await playingState(page)
    await expect(video(page)).toHaveAttribute('data-state', state)
    await openMenu(page)
    await expect(streamChoice(page).getByRole('radio', { name: 'SD' })).toBeChecked()
    // A stream picked closes the menu.
    await streamChoice(page).getByRole('radio', { name: 'HD' }).click()
    await openMenu(page)
    await expect(streamChoice(page).getByRole('radio', { name: 'HD' })).toBeChecked()
    await expect(video(page)).toHaveAttribute('data-state', state)
})

test('where the browser gives no fullscreen, as iPhone Safari does not, a click on the picture has the tile stand in for it, without its settings', async ({ page }) => {
    await page.addInitScript(() => Object.defineProperty(Document.prototype, 'fullscreenEnabled', { get: () => false }))
    await page.goto('/')
    const tile = page.locator('.CameraTile')
    await openMenu(page)
    await expect(settingsItem(page)).toBeVisible()
    await video(page).click()
    await expect(tile).toHaveClass(/\bCameraTile--fullscreen\b/)
    await openMenu(page)
    await expect(page.getByRole('switch', { name: 'Sound' })).toBeVisible()
    await expect(settingsItem(page)).toHaveCount(0)
    await video(page).click()
    await expect(tile).not.toHaveClass(/\bCameraTile--fullscreen\b/)
})

test('the menu button says the menu is open while it shows, the pointer over the menu or not', async ({ page }) => {
    await page.goto('/')
    const trigger = menuTrigger(page)
    const sound = page.getByRole('switch', { name: 'Sound' })
    await expect(trigger).toHaveAttribute('aria-expanded', 'false')
    await trigger.hover()
    await expect(trigger).toHaveAttribute('aria-expanded', 'true')
    await sound.hover()
    await expect(trigger).toHaveAttribute('aria-expanded', 'true')
    // Off the menu, which a hover opened, to the page's far corner: the menu goes.
    await page.mouse.move(1, page.viewportSize()!.height - 1)
    await expect(sound).toBeHidden()
    await expect(trigger).toHaveAttribute('aria-expanded', 'false')
})

test('the sound switch says whether sound is on', async ({ page }) => {
    await page.goto('/')
    await openMenu(page)
    const sound = page.getByRole('switch', { name: 'Sound' })
    await expect(sound).toHaveAttribute('aria-checked', 'false')
    await sound.click()
    await expect(sound).toHaveAttribute('aria-checked', 'true')
})
