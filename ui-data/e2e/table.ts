import type { Locator, Page } from '@playwright/test'

// Playwright helpers for InfiniteScrollTable's tables — CrudPanel's among
// them — for a host's E2E suite.

// The cells showing text, of the tables within root: the page, or a part of
// it.
export const cells = (root: Page | Locator, text: string | RegExp): Locator => root.locator('.data-table td .cell-content', { hasText: text })

// The row showing text, opened in its detail tab: the table binds a row's
// activation to a double click.
export const openRow = (root: Page | Locator, text: string | RegExp): Promise<void> => cells(root, text).first().dblclick()
