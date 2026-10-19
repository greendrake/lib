import { expect, test } from '@playwright/test'
import { openMenu, playingState, settingsItem, streamChoice, video } from './app'
import { stack } from './stack.js'

// The tile shows once the cameras push has named the camera, its state with it.
test('on a camera over RTSP, the tile plays both its streams and offers no settings, which it has none of', async ({ page }) => {
    await page.goto(`http://${stack.rtspApp}/`)
    await expect(video(page)).toHaveAttribute('data-state', await playingState(page))
    await openMenu(page)
    await expect(streamChoice(page).getByRole('radio', { name: 'SD' })).toBeChecked()
    await expect(streamChoice(page).getByRole('radio', { name: 'HD' })).toBeVisible()
    await expect(settingsItem(page)).toHaveCount(0)
})
