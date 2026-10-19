import { expect, type Locator, type Page } from '@playwright/test'
import { RpcClient, WebSocketPipe, WsTransport } from '@greendrake/rpc'
import type { Methods } from '@greendrake/ipcam'
import { streamCodec } from '@greendrake/mse-video'
import { stack } from './stack.js'

/** The camera tile's video. */
export const video = (page: Page): Locator => page.locator('.CameraTile .Video')

/** The state the tile's video reaches with a stream to play: `unsupported` where the browser plays no H.265, as none of those Playwright runs here does. */
export const playingState = async (page: Page): Promise<string> => ((await page.evaluate(codec => MediaSource.isTypeSupported(codec), streamCodec)) ? 'playing' : 'unsupported')

/** The camera's state banner, atop its open settings. */
export const banner = (page: Page): Locator => page.locator('.StateBanner')

/** The button that opens the camera tile's menu. */
export const menuTrigger = (page: Page): Locator => page.locator('.CameraTile').getByRole('button', { name: 'Menu' })

/** Opens the camera tile's menu. */
export const openMenu = (page: Page): Promise<void> => menuTrigger(page).click()

/** The tile's choice of stream, in its open menu. */
export const streamChoice = (page: Page): Locator => page.getByRole('radiogroup', { name: 'Stream' })

/** The item of the tile's menu that opens the settings. */
export const settingsItem = (page: Page): Locator => page.locator('.CameraTile__items').getByRole('button', { name: 'Settings' })

/** The settings' modal. */
export const settings = (page: Page): Locator => page.getByRole('dialog')

/** Opens the settings from the tile's menu. */
export const openSettings = async (page: Page): Promise<void> => {
    await openMenu(page)
    await settingsItem(page).click()
}

/** Closes the open settings. */
export const closeSettings = (page: Page): Promise<void> => settings(page).getByRole('button', { name: 'Close' }).click()

/** A group of settings, by its tab's label. */
export type Group = 'General' | 'Encode' | 'Network' | 'NTP' | 'Camera' | 'Overlay' | 'Maintenance'

/** Opens a group's tab in the open settings. */
export const openGroup = (page: Page, group: Group): Promise<void> => settings(page).locator('.TabBar__tab').getByText(group, { exact: true }).click()

/** The app anew, its settings open on a group once the camera is online. */
export const loadSettings = async (page: Page, group: Group): Promise<void> => {
    await page.goto('/')
    await openSettings(page)
    await expect(banner(page)).toHaveAttribute('data-state', 'online')
    await openGroup(page, group)
}

/** How long a reboot keeps the fake camera unreachable, in ms. */
export const rebootPause = stack.rebootSeconds * 1000

/** A request of the fake camera's control endpoint, failing as the endpoint fails it. */
const control = async (path: string, init?: RequestInit): Promise<Response> => {
    const response = await fetch(`http://${stack.control}${path}`, init)
    if (!response.ok) {
        throw new Error(`the fake camera's ${path}: ${await response.text()}`)
    }
    return response
}

/**
 * Lays body over the fake camera's configuration under name, as a write sending only
 * what it changes: for a state a spec starts from that the app does not write.
 */
export const overlayFake = async (name: string, body: unknown): Promise<void> => {
    await control(`/configs/${encodeURIComponent(name)}`, { method: 'PATCH', body: JSON.stringify(body) })
}

/**
 * Runs while with the fake camera leaving the commands it receives under name
 * unanswered, and answers them after.
 */
export const holdingFake = async (name: string, meanwhile: () => Promise<void>): Promise<void> => {
    const path = `/holds/${encodeURIComponent(name)}`
    await control(path, { method: 'PUT' })
    try {
        await meanwhile()
    } finally {
        await control(path, { method: 'DELETE' })
    }
}

/** A title bitmap's size, as the fake camera's control endpoint answers it. */
export interface TitleSize {
    Width: number
    Height: number
}

/** The size of the title bitmap the fake camera was sent to draw last. */
export const fakeTitle = async (): Promise<TitleSize> => (await control('/title')).json()

/** How many commands the fake camera has received under name. */
export const fakeCommands = async (name: string): Promise<number> => Number(await (await control(`/commands/${encodeURIComponent(name)}`)).json())

/** Calls the app's API as the SPA does, for the state a spec starts from: on the camera its arguments name, which is `camera`. */
export const call = async <K extends keyof Methods & string>(method: K, ...args: Parameters<Methods[K]>): Promise<Awaited<ReturnType<Methods[K]>>> => {
    const pipe = new WebSocketPipe({ url: `ws://${stack.app}/api` })
    try {
        return await new RpcClient<Methods>(new WsTransport({ pipe })).call(method, ...args)
    } finally {
        pipe.dispose()
    }
}
