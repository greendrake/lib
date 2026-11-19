# @greendrake/ui

Vue 3 components for an app that has a design of its own. They bring the behaviour an interface needs — validation, focus, keyboard, touch, narrow screens — and take their colours, spacing and type from CSS custom properties the app defines.

```sh
bun add @greendrake/ui vue
```

## Why

Most of the work in an interface control is behaviour, and it shows only when it is missing. A modal has to keep Tab inside itself. A search box has to drop the answer to a query the user has already replaced.

Writing such controls in the app means writing the same details again in the next app. A styled component framework brings a look of its own, and an app that already has a design has to restyle it. A headless library supplies behaviour without styles, so the app still writes the styling of every component. The components here are finished in markup, layout and behaviour. Their look comes from CSS custom properties that the app defines once.

## Trade-offs

- **The app has to supply the look.** The custom properties listed under "Read with no fallback" have no defaults here, and five components show progress with a `spinner` class that the app styles. An app with no design tokens of its own has that work to do first, and a styled framework may suit it better.
- **Not a complete kit.** There is no date or time picker, no slider and no file input. `SelectBox` picks one value. Validation is one function per field: there is no schema, and a form's validity is a ref the app computes.
- **Not every component works from the keyboard.** `TabBar`'s tabs, `OptionSwitch`'s options, `ThumbnailGrid`'s tiles, the `Copy` icon and `Password`'s show/hide toggle are `div`s with click handlers. They take no focus and carry no ARIA role. `AsyncSearch`'s results carry roles, but the component binds no key that picks one.

## Overview

What the components take care of:

- **Modals stack.** Every open `Modal` joins one stack. Escape and history navigation close the top one only. Everything outside the top modal is `inert`, Tab wraps inside it, and closing returns focus to the element that had it before.
- **Narrow screens and touch.** Below 600px a `Modal` becomes a bottom sheet that drags down to close, `TabBar` becomes an accordion, and `AsyncSearch` shows its results over the screen below the field. On a touch device, `autofocus` and an opening modal do not focus a field: a field that takes focus by itself would bring up the on-screen keyboard before the user has asked to type.
- **Validation at the field.** A `TextField` with a `validator` keeps an invalid value out of its model. It says what is wrong when the user leaves the field, not while they type.
- **Async work shows its state.** `FormC` and `OptionSwitch` run a save through one state machine: an optional confirmation, the save in progress, and its success or failure. `AsyncSearch` drops the results of a query that has been replaced.
- **Every string is replaceable.** All user-facing text is in one `messages` object, which an app with translations sets in one call.

## Setup

`vue` is the peer dependency. `@greendrake/util`, `@greendrake/domains`, `@greendrake/scss-kit` and `vue-tippy` come in as dependencies.

The package ships TypeScript, SFC and SCSS source as it is: there is no build step and no `.d.ts`, and `exports` points at `src/main.ts`. The consuming app's bundler has to compile all three, as Vite does with its Vue plugin and a Sass compiler installed. The app's `vue-tsc` type-checks the components too. The SFCs are written against the `vueCompilerOptions` of `@greendrake/dev-config/tsconfig/vue.json`: `strictTemplates`, with `data-*` attributes allowed. A consumer's tsconfig extends it:

```json
{
    "extends": "@greendrake/dev-config/tsconfig/vue.json",
    "include": ["src"]
}
```

The components are for the browser only: nothing here is written for server-side rendering.

## Styling contract

Component styles are SCSS inside the SFCs. They use `@greendrake/scss-kit` (breakpoints, z-index layers, icon masks, control mixins), which is resolved at compile time. Its README lists the custom properties those mixins read. Colours, spacing and typography come from CSS custom properties that the consuming app defines. `AsyncAction`, `AsyncSearch`, `TreeView`, `ThumbnailGrid` and `Gallery` show progress by toggling a `spinner` class, which the app styles globally.

Read with no fallback, so the app defines them:

| Property                                                         | Read by                                                                                                                                                                                            |
| ---------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `--font-color`                                                   | text throughout: `AsyncSearch`, `BottomNav`, `Checkbox`, `ContextMenu`, `Gallery`, `Modal`, `Password`, `SearchField`, `SwitchRow`, `TreeView`                                                     |
| `--background-color`                                             | `AsyncSearch` narrow panel                                                                                                                                                                         |
| `--form-background`, `--form-background-hover`                   | control backgrounds: `AsyncSearchResults`, `BottomNav`, `ContextMenu`, `SegmentedControl`, `TabBar`, `ThumbnailGrid`, `TreeView`                                                                   |
| `--input-background`                                             | `Checkbox` box                                                                                                                                                                                     |
| `--border-color`, `--border-color-light`, `--border-color-hover` | control and container borders; `--border-color-light` is also the `SplitPane` handle                                                                                                               |
| `--border-radius`                                                | `AsyncSearch`, `AsyncSearchResults`, `Checkbox`, `ThumbnailGrid` (`FieldTip` falls back to 4px)                                                                                                    |
| `--border-radius-pill`                                           | `SwitchRow` track                                                                                                                                                                                  |
| `--accent-color`                                                 | active and checked states: `BottomNav`, `Checkbox`, `SegmentedControl`, `SwitchRow`, `TabBar`, toasts; the hovered `SplitPane` handle; `TreeView` drop targets; the `ThumbnailGrid` pager's slider |
| `--space-1` … `--space-5`                                        | spacing                                                                                                                                                                                            |
| `--font-xs`, `--font-md`, `--font-lg`                            | `BottomNav` and `ThumbnailGrid` labels, `FieldTip`, `Modal` title                                                                                                                                  |
| `--line-height-normal`                                           | `Password`                                                                                                                                                                                         |
| `--modal-background`, `--modal-border-radius`                    | `Modal` window                                                                                                                                                                                     |

Read with a local default:

| Property                                                                                                      | Read by                                                               | Default                                                                      |
| ------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `--checkbox-size`                                                                                             | `Checkbox` box                                                        | 24px, set on `.Checkbox`                                                     |
| `--switch-width`, `--switch-height`, `--switch-knob`                                                          | `Checkbox` with `switch`                                              | 40px / 24px / `calc(var(--switch-height) - 4px)`, set on `.Checkbox--switch` |
| `--modal-box-shadow`, `--modal-backdrop-filter`, `--modal-window-backdrop-filter`, `--modal-button-min-width` | `Modal`                                                               | per property                                                                 |
| `--circular-progress-arc`, `--circular-progress-label`, `--circular-progress-track`                           | `CircularProgress`                                                    | per property                                                                 |
| `--row-select-color`, `--tree-line-color`                                                                     | `TreeView`; `--row-select-color` also `SelectionBand`                 | per property                                                                 |
| `--accent-contrast`                                                                                           | `SegmentedControl` active segment text                                | none                                                                         |
| `--font-color-muted`                                                                                          | `SelectBox` placeholder, `BottomNav` inactive tabs                    | `--vs-text-color` / `--font-color`                                           |
| `--bottomnav-height`                                                                                          | `BottomNav` bar height; bottom edge of the `AsyncSearch` narrow panel | none / 0px                                                                   |
| `--safe-area-inset-top`, `--safe-area-inset-bottom`                                                           | toast layer; `Modal` sheet and `BottomNav` padding                    | 0px                                                                          |
| `--max-content-width`                                                                                         | `TheWrap` width cap                                                   | none                                                                         |
| `--tippy-background-color`                                                                                    | `FieldTip` validation tips, `InfoTip` tooltips                        | `#5335e8`                                                                    |
| `--toast-color`, `--toast-background`                                                                         | toasts                                                                | `#fff`; `--accent-color` (`--warning-color` for the warning variant)         |
| `--color-attention-fill`, `--color-attention-contrast`                                                        | `BottomNav` badge                                                     | `--accent-color` / `#fff`                                                    |
| `--vs-*`                                                                                                      | `SelectBox` (see the component)                                       | per property                                                                 |

Breakpoint: "narrow" is `@greendrake/scss-kit`'s 600px boundary; `Modal`, `TabBar` and `AsyncSearch` switch presentation below it. The boundary is compiled in: the app cannot set it.

Tooltips: `FieldTip`, `InfoTip`, `MenuButton`, `ContextMenu` and `AsyncSearch` use `vue-tippy`. Each ships the styles of the tippy theme it uses (`tip` for the first two, `HMenu`, `ContextMenu`, `asyncSearchResults`); no global tippy.js stylesheet is loaded or required.

## Messages

Every user-facing string is in one reactive `messages` object (`UiMessages`). `configureUiMessages(overrides: Partial<UiMessages>)` merges overrides in; call it at boot and again on locale change.

```ts
import { configureUiMessages } from '@greendrake/ui'

configureUiMessages({ cancel: 'Abbrechen', nothingFound: 'Nichts gefunden' })
```

Keys and defaults: `loading` "Loading", `ok`, `cancel`, `close`, `clear`, `retryCountdown` "Error. Retrying in {seconds}" (`{seconds}` is substituted at render time), `nothingFound`, `searchResults`, `search`, `create`, `refresh`, `delete`, `deleting`, `newTab`, `copied`, `clickToCopy`, `invalidEmail` "Not a valid email address", `selectOption` "Select an option", `aboutThis` "About this", the names of `ThumbnailGrid`'s pager controls: `firstPage` "First page", `previousPage`, `nextPage`, `lastPage`, `page` "Page" (the slider), `goToPage` "Go to page" (the field), and the names of its file icons: `fileImage` "Image", `fileVideo`, `fileAudio`, `fileDocument`, `fileSpreadsheet`, `filePresentation`, `fileArchive`, `fileOther` "File".

`uiStrings` describes these strings to the i18n setup of a host. The host is the app that uses this package. The object tells the setup three things: the strings are read from the `ui` namespace of the host's message catalogue, they are there under the keys above, and `{seconds}` is passed through unchanged for the countdown to fill.

`configurableMessages(defaults)` gives another package the same arrangement for its own strings. It returns `{ messages, configure }`, where `messages` is reactive and starts as `defaults`. The `@greendrake/ui/messages` entry exports this module alone. It is for code that has to load without the components, such as a module under `bun test`, which compiles no SFC.

## Form controls

### TextField

`v-model: string` — always a string, `''` when blank. Renders an `<input>` (`type` is passed through), a `<textarea>` for `type="textarea"`, or the `richEditor` component for `type="rich"`.

Props: `type` (default `'text'`), `placeholder`, `ariaLabel` (defaults to the placeholder), `id` (set on the input, so `focusIn(id)` and `<label for>` reach it), `autofocus` (pointer devices only, and only when nothing else holds focus), `validator?: (v: string) => true | string`, `validateOnBlur` (default `true`), `richEditor?: Component`. Exposes `focus()`.

Validation: while the typed value fails `validator`, the model is `''`. When the field loses focus holding a non-empty invalid value, the message the validator returned shows as a tooltip on the input (`FieldTip`). Typing dismisses it. Inside a form that provides `validationScopeKey` (see below), blur validation is skipped when focus leaves the form entirely.

`TextField` has no rich-text editor of its own: `richEditor` is how the app passes one in. When `type === 'rich'`, the component passed in is rendered in place of the input. Any component will do that takes the same string `v-model` and accepts `ariaLabel`. The prop is read once at setup, and `type="rich"` without it throws.

```vue
<script setup lang="ts">
import { ref } from 'vue'
import { Field, TextField } from '@greendrake/ui'

const slug = ref('')
const slugRule = (v: string): true | string => /^[a-z0-9-]+$/.test(v) || 'Lowercase letters, digits and dashes only'
</script>
<template>
    <Field label="Slug">
        <TextField v-model="slug" :validator="slugRule" placeholder="my-page" />
    </Field>
</template>
```

### Password

`v-model: string`. Password input with a show/hide toggle that keeps the caret where it was. Props: `autofocus`, `minLength` (default 1), `id` (set on the wrapper, so `FieldTip` can anchor on `#<id>`; the inputs are `#<id> input`).

### Email

`v-model: string`. `TextField` with `type="email"` and a validator backed by `isEmailValid` from `@greendrake/domains`; the failure message is `messages.invalidEmail`. Props: `placeholder`, `autofocus`, `id`, `validateOnBlur`. Other attributes fall through to the `TextField`.

### Checkbox

`v-model: boolean`. Props: `label`, `switch` (iOS-style sliding switch instead of a tick box), `disabled`, `indeterminate` (the tick box shows a dash, neither ticked nor unticked, whatever the value: a folder's box when only some of its files are ticked). Sizing via `--checkbox-size` / `--switch-*` (see Styling contract).

### CurrencyInput

`v-model: string | null` — the normalised amount (no thousands separators, `precision` decimals) or `null` when empty; the input itself shows separators as the user types. Props: `symbol` (rendered before the amount), `hideSymbol`, `precision` (default 2), `forceDecimals`, `doNotShowZeroDecimals`, `min` (input rejects values below it), `placeholder`, `ariaLabel`, `autofocus`, `disabled`.

### Field

Caption + control column. Props: `label`; `group` wraps the slot in a `role="group"` named by the label (for a set of controls); `unlabelable` leaves the caption as plain text (for a control a `<label>` cannot bind to, e.g. a contenteditable). Without either, the slot renders inside a `<label>` so the caption is the control's accessible name. Default slot.

### FieldTip

`FieldTip(source: Ref<string | false>, target: string | Ref<HTMLElement | ComponentPublicInstance | null>): void` — binds a manually-triggered tippy tooltip (theme `tip`) to an element or element id: setting `source` to a string shows it as text, `false` hides it; user dismissal resets `source` to `false`. Call it from `setup`; it cleans up on unmount.

`<InfoTip text="…" />` — a small "?" button whose `text` shows in a tooltip (theme `tip`) on hover, on focus, and on a tap where there is no hover; its accessible name is `messages.aboutThis`. Place it beside a field's `<label>`, not inside: within one it would become the control the label names.

### validationScopeKey

`InjectionKey<Ref<HTMLElement | null>>`. A form component provides a ref to its root element under this key; descendant `TextField`s then skip blur validation when focus leaves that element (e.g. the user clicks elsewhere without finishing the form).

## Form submission

### FormC

A `<form>` whose submit button also shows the form's status. A state machine drives it. With no submission under way, the state is `invalid`, `dirty` or `saved`, derived from `refs.valid` and `refs.dirty` (a missing ref counts as `true`). A submission puts the form through states of its own: `confirm` → `inprogress` → `success` | `error`. `confirm`, `success` and `error` last 2000ms, and then the state is derived from the refs again. `inprogress` lasts until the action settles. The root carries the classes `form form-<state>`, and the button carries `button button-<state>`. The button is disabled unless the state is `dirty` or `confirm`.

Props: `action?: () => Promise<unknown> | unknown`, `askToConfirm` (first submit enters `confirm`, the next one submits), `refs?: { dirty?: Ref<boolean>; valid?: Ref<boolean> }`, per-state button labels `savedText`, `dirtyText`, `invalidText`, `confirmText`, `inprogressText`, `successText`, `errorText` (a missing label shows the raw state name), `noContent` (empty button, state via classes only), `tryPersistently` (retries `action` until it succeeds via `Retrier` from `@greendrake/util`, showing `messages.retryCountdown` in the button between attempts), `successTimeout` (`true`/unset → 2000ms; a number → that many ms; `false` → skip the success state). Default slot holds the fields.

### AsyncAction

Button (or `tag="a"`) for a one-shot async action: label switches between `labelDirty` / `labelDone` / `labelInProgress`, the element disables itself while the promise is pending, with a `spinner` class meanwhile. Props: `promiseMaker: () => Promise<void>` (required), `labelDirty` (required), `labelDone`, `labelInProgress`, `isDirty`, `isValid`, `showSpinner` (default `true`), `type` (default `'button'`), `tag` (default `'button'`). Exposes `run()`.

### useAsyncAction, errorMessage

`useAsyncAction(fn)` keeps the state of an action button that the page writes in its own markup. It returns `{ acting, error, notice, run }` (`AsyncActionState<Args>`). A call to `run(...args)` while `acting` is ignored. Otherwise `run` clears `error` and `notice` and awaits `fn(...args)`. If `fn` resolves to a string, that string becomes the `notice`. If `fn` rejects, the rejection's message goes into `error`. The failure is shown where the action was taken, so `fn` makes a silent call: one that reports its failure nowhere else. When only one of several buttons may act at a time, the buttons share one `useAsyncAction`: `fn` takes a parameter, and each button passes its variant to `run`.

```vue
<script setup lang="ts">
import { useAsyncAction } from '@greendrake/ui'

const save = useAsyncAction(async () => {
    await saveThing(draft.value) // reports its failure to nobody else
    return 'Saved'
})
</script>
<template>
    <button :disabled="save.acting.value" @click="save.run()">Save</button>
    <span v-if="save.notice.value">{{ save.notice.value }}</span>
    <span v-if="save.error.value" class="error">{{ save.error.value }}</span>
</template>
```

`errorMessage(e)` turns a caught value into text: an `Error` gives its message, and anything else is stringified.

### OptionSwitch

`v-model: T` (`T extends string | number | boolean`). Radio-style switch: clicking an option runs `saveHandler` through the same state machine as `FormC` (optionally with a confirm step) and updates the model on success. Props: `options: OptionsArray<T> | OptionsMap<T>` (required; array entries are bare values or `[value, title]` tuples, the map form is `Map<value, title>`), `label`, `requireConfirmation`, `disabledOptions: T[]`, `saveHandler?: (value: T) => Promise<void> | void`. Emits `click(event)`. A slot named after `String(value)` adds markup to that option. Classes: `.OptionSwitch` gets `saving` / `error`; options get `current`, `confirm`, `disabled`. A model value outside `options` throws at setup.

### SegmentedControl

`v-model: string` (required). Pill-shaped radio group backed by real radio inputs. Props: `options: SegmentedOption[]` (`{ value, label }`), `ariaLabel` (names the group; makes the container an explicit `radiogroup`).

## Search

### SearchField

`v-model: string`. Text input with search icon, clear button and Escape-to-clear. Prop `debounce?: boolean | number | ((value: string) => number)` delays model updates: `true` → 600ms, a number → that many ms, a function → decided per keystroke from the value about to be set. Also `placeholder`, `autofocus`. Exposes `flush(): boolean` — puts the typed value in the model immediately, dropping any pending update; returns whether the model changed. `class`/`style` go to the root, every other attribute to the inner input.

### AsyncSearch

`SearchField` plus a list of results. Props: `search: SearchFunction` (required), `placeholder`, `autofocus`, `itemComponent?: Component | null` (rendered per result with a `data` prop; the default renderer shows `htmlTitle` via `v-html`), `searchKey?: unknown` (re-runs the current query when it changes), `minQueryLength` (default 3). Emits `result(data: SearchResultItem)` when a result is picked, and does nothing else: what follows is up to the component using it. It may navigate, or call the exposed `clear()` (empties the query) or `hide()` (dismisses the results popper and keeps the query). Slots: `resultsFooter` (strip under the list), `empty` (replaces the "nothing found" row). Enter searches the current input immediately. The response to a query that has since been replaced is dropped.

```ts
type SearchFunction = (query: string, isSearching: Ref<boolean>) => Promise<SearchResultItem[]>
interface SearchResultItem {
    id: string
    htmlTitle?: string // trusted/escaped markup; used by the default renderer only
    [key: string]: unknown
}
```

`isSearching` is handed to the function so that it can drive the field's spinner for exactly the duration of its call. On a wide viewport the results show in a tippy popper anchored under the field (theme `asyncSearchResults`). Below the narrow breakpoint they fill the viewport, from the field's bottom edge down to `--bottomnav-height`. A field inside a `Modal` is the exception: there the results stay inside the modal.

## Modals

### Modal

Centred dialog, or a bottom sheet with drag-to-dismiss below the narrow breakpoint. Teleported to `body`; attributes (e.g. `class`) go to the teleported root. The consumer shows and hides the modal (`v-if`): `ok` and `cancel` are emits only.

Props: `title` (titled header with an explicit close), `closable` (X in the toolbar), `expandable` (full-viewport toggle), `buttons` (default `true`), `okLabel` / `cancelLabel` (default `messages.ok` / `messages.cancel`), `okDisabled`, `hideOk`, `hideCancel`, `mainClass` (class on the content area), `initialFocus: 'field' | 'dialog'` (default `'field'`; see the stack). Emits `ok`, `cancel`. Slots: default (content), `buttons` (replaces the button row), `ok` and `cancel` (button contents). Exposes `rootEl` and `windowEl`, the dialog window.

The modal stack is module-level and automatic: every mounted `Modal` registers in it.

- Escape and `popstate` close the top-most modal only, through its `cancel` emit.
- Everything outside the top-most modal's root is made `inert`. Tab wraps inside the modal, and focus that lands outside it is moved back in.
- Initial focus goes to the first field. It goes to the dialog itself for `initialFocus="dialog"`, and on touch devices, where a focused field would bring up the on-screen keyboard.
- Closing returns focus to the element that had it when the modal opened.
- Body scroll is locked while any modal is open.

```vue
<script setup lang="ts">
import { ref } from 'vue'
import { Modal, TextField } from '@greendrake/ui'

const props = defineProps<{ rename: (title: string) => Promise<void> }>()
const open = ref(false)
const title = ref('')
const save = async (): Promise<void> => {
    await props.rename(title.value)
    open.value = false
}
</script>
<template>
    <button type="button" @click="open = true">Rename</button>
    <Modal v-if="open" title="Rename" :ok-disabled="!title" @ok="save" @cancel="open = false">
        <TextField v-model="title" placeholder="New title" />
    </Modal>
</template>
```

### ModalOnClick

Trigger element plus a `Modal` around the `content` slot. The default slot is the trigger (`tag`, default `'a'`; `id`, `type`, `ariaLabel` and other attributes go to it); `modalClass` and `modalTestid` (its `data-testid`) go to the modal. The `Modal` props `title`, `okLabel`, `cancelLabel`, `okDisabled`, `hideOk`, `closable`, `expandable`, `buttons` are forwarded as given, unset ones keeping `Modal`'s defaults. Emits `click(event)` and `open` when the trigger is clicked. `ok` is re-emitted without closing the modal, so an async submit handler can keep it open on failure and close it on success through the exposed `onCancel()`. `cancel` closes the modal and is then re-emitted. Exposes `open(event)`, `onCancel()`.

### ConfirmDialog

A `Modal` that puts an action to the user before it runs. Props: `title` (the action's name: the heading and the OK label), `message` (what the action does), `run: () => Promise<unknown>` (the action). OK runs it, with the button disabled while it runs, and emits `close` once it is done. Cancel emits `close` too. An action that fails leaves the dialog open: its failure is reported wherever the action reports it.

### Gallery

Image and video viewer in a `Modal`. Props: `items: GalleryItem[]` (`{ url, alt?, caption?, video? }`; `video` plays the item, with controls, instead of showing it as an image), `startIndex` (default 0). Emits `cancel`. Arrow keys and horizontal swipes navigate. A swipe past either end, a tap outside the item, or a click on an image closes the gallery.

The gallery sizes its window to the item. An item that fits in the browser window shows at its own size, and the window is just big enough to house it and its caption. An item that does not fit gets a window filling the browser window, and it is scaled down to fit in it. A resize of the browser window decides again. While an item loads, the window fills the browser window.

`galleryItem(file: MediaFile, alt: string): GalleryItem | null` is the item a file shows as. An image of a type browsers display shows as an image, as does a file of unknown type (`mime: null`, a bare image URL). A video plays. Any other file gives `null`: the gallery cannot show it. Its parts are exported too: `mediaType(file)` is the file's type alone, lower-cased and without parameters (`null` when unknown), and `displayedImage(type)` tells whether browsers display images of the type.

```ts
interface MediaFile {
    url: string
    mime: string | null // null: unknown
}
```

## Menus and selects

### MenuButton

Hover/click dropdown built on `vue-tippy`'s `Tippy` (theme `HMenu`, placement `bottom-end`). It is appended to `body`, or to the fullscreen element while there is one. Any click inside the menu closes it. Slots: `main` (trigger; slot prop `open`, whether the menu shows, for the trigger's `aria-expanded`), `menuitems` (menu content, wrapped in `.Menubutton-Menu`). Props: `menuClass` (on the wrapper), `boxClass` (on the tippy box, applied once on create), `offset` (default `[0, -1]`), `matchTriggerWidth` (sets the popper's width to the trigger's).

### ContextMenu

A menu at the pointer in place of the browser's own, built on `vue-tippy`'s `Tippy` (theme `ContextMenu`), appended to `body`. Prop `items: ContextMenuItem[]` (`{ label, run: () => unknown }`), rendered as `role="menuitem"` buttons. Exposes `open(event: MouseEvent)`. Call it from a `contextmenu` listener: it cancels the browser's menu and shows this one at the event's point. Choosing an item closes the menu and runs the item. A press outside the menu closes it, and so does any key. Place it inside the element it serves: a press there closes it too.

### SelectBox

`v-model: T | null` (`T extends string | number`; `null` = no selection, shows the placeholder). Single-select dropdown with keyboard support: arrows, Enter, and Escape, which closes the menu without closing a modal the control sits in. Props: `options: SelectBoxOption<T>[]` (`{ value, label, disabled?, depth? }` — `depth` indents the option in the menu), `placeholder` (default `messages.selectOption`), `ariaLabel`, `clearable`, `searchable` (type-to-filter input while open), `disabled`, `htmlLabel` (labels via `v-html`; trusted content only). Emits `pick(value)` on every menu selection, including re-picking the current one. Exposes `open()`. The menu is positioned against the viewport, flips upward or limits its height when a fixed element, such as a bottom bar, would cover it, and follows scroll and on-screen-keyboard changes.

Styled through `--vs-*`, each with a fallback: `--vs-height` (unset → auto), `--vs-padding` (`4px 8px`), `--vs-line-height` (`normal`), `--vs-border` (`1px solid #e4e4e7`), `--vs-border-radius` (`4px`), `--vs-input-bg` (`#fff`), `--vs-text-color` (`inherit`), `--vs-menu-height` (`200px`), `--vs-menu-bg` (`#fff`), `--vs-option-padding` (`8px 12px`), `--vs-option-text-color`, `--vs-option-hover-color` (`#dbeafe`), `--vs-option-selected-color` (`#93c5fd`), `--vs-option-selected-text-color`.

## Tabs

### TabBar

Props: `tabs: TabDef[]` (`{ id, label, closable? }`), `modelValue: string | null`, `closable` (default `false`; per-tab `closable` overrides). Emits `update:modelValue(id)`, `close(id)`. The default slot renders a panel under the strip; below the narrow breakpoint the strip becomes a vertical accordion with the panel directly under the active tab.

```vue
<script setup lang="ts">
import { ref } from 'vue'
import { TabBar, type TabDef } from '@greendrake/ui'

const tabs: TabDef[] = [
    { id: 'general', label: 'General' },
    { id: 'security', label: 'Security' }
]
const active = ref<string | null>('general')
</script>
<template>
    <TabBar v-model="active" :tabs="tabs">
        <GeneralPane v-if="active === 'general'" />
        <SecurityPane v-else-if="active === 'security'" />
    </TabBar>
</template>
```

## Toasts

`showToast(options: ToastOptions): ToastHandle` — imperative top-centre toast, rendered without an app context (callable from stores and plain modules). Toasts raised together stack in order.

```ts
interface ToastOptions {
    content: string | VNode // text, or a VNode with its handlers already bound
    variant?: 'accent' | 'warning' // default 'accent'
    hideOnClick?: boolean // default true: dismissed by the next key press / pointer down outside the toast layer
    blocking?: boolean // must be answered: an overlay holds the page until the toast goes, and no interaction elsewhere dismisses it
    onHidden?: () => void
}
```

```ts
import { showToast } from '@greendrake/ui'

showToast({ content: 'Saved' })
const failure = showToast({ content: 'Upload failed', variant: 'warning', hideOnClick: false })
failure.destroy()
```

## Layout

### TheWrap

Page shell. The `top` slot and the default slot (the routed content) stretch, and the `bottom` slot is pushed to the bottom of the viewport on short pages. A lone child (e.g. a splash) is centred vertically. The width is capped by `--max-content-width`.

### BottomNav

Fixed bottom tab bar for narrow viewports; the app hides it on wide ones. Props: `tabs: BottomNavTab[]` and `link: Component`. A tab that navigates is rendered as `link`, which is given the tab's `to`. Pass the app's router link: `RouterLink` from `vue-router`, or any component taking a `to`. Emits `select(id)`. Default slot is a trailing cell for page actions.

```ts
interface BottomNavTab {
    id: string
    label: string
    to?: string // the link's destination; omit for a tab that toggles a shell overlay (rendered as a button, reports via `select` + aria-expanded)
    active: boolean // the app owns the route → tab mapping
    badge?: number
}
```

Per-tab icons are the app's: style `.BottomNav__icon--<id>` with a `mask-image`. The bar's height is `--bottomnav-height` plus `--safe-area-inset-bottom`.

### SplitPane

Two panes side by side. Dragging the handle between them resizes the start pane. Props: `initialWidth` (default 150) and `minWidth` (default 100), both the start pane's. Slots: `start`, `end`. Each pane is a flex column whose children grow to fill it, and the start pane scrolls its overflow. Without an `end` slot (e.g. a conditional `<template v-if="…" #end>`) the start pane spans the whole width and no handle renders.

### Splash, Loading

`Splash` centres its default slot (logo) above a `Loading` indicator, covering its positioned ancestor. `Loading` renders `messages.loading` with animated dots.

## Tree

### TreeView

Lazy tree rendered as one flat list of visible nodes. Props: `loader: TreeLoader` (required), `idField` (default `'key'`), `labelField` (default `'title'`), `editField` (the field a rename edits; default `labelField`), `multiSelect` (default `true`), `draggable` (default `false`), `dropType?: string`, `onRename?: (node: TreeNodeId, value: string) => Promise<void>`, `onAddChild?: (parent: TreeNodeId, value: string) => Promise<void>`. `v-model:selected: TreeNodeId[]`. Children load on first expand and are cached per node. Selection uses `useSelection`: click selects, Shift extends a range over the visible list, Ctrl/Cmd toggles; the list is focusable, the arrows move the cursor (Shift extending the range) and Ctrl/Cmd+A selects all. A right-click focuses its row. If the row's node is not selected yet, it becomes the only one selected. Then `contextmenu(event)` is emitted, and the consumer opens its menu.

A node may have several parents. It then shows under each of them, and each such row is an occurrence of the node. Rows are keyed by their path from the root, while the children cache, expansion and selection stay per node. Once a node's children are loaded, they decide whether its row expands, and the `expandable` that came with the node in its parent's load is ignored. The cursor's row is the focused occurrence (`aria-current`).

In-place editing:

- With `onRename`, double-clicking a label turns it into an input holding the node's `editField`.
- With `onAddChild`, `addChild()` opens an empty input, last under the focused row.
- Enter trims the value and hands it to the callback: a rename's value if it has changed, a new child's if it is not empty. The input closes once the callback resolves. A rejection leaves it open.
- Escape discards the input, and so does moving focus out of it.
- Keys typed in the input stay there: they do not reach the tree.

Exposed:

- `reload(nodeId | null)` re-fetches a node's children (`null`: the root level) and keeps what is expanded.
- `expand(nodeId)` expands a node wherever it shows, loading its children first.
- `levelsOf(nodeId)` lists the loaded levels that hold a node: the parents it shows under, and `null` for the root level. These are the levels to reload when the node changes or is removed.
- `focused` is the focused occurrence while its row is visible, and `null` otherwise.
- `addChild()`, described above.

With `draggable`, a row can be dragged onto another row. A drop on the target row's upper third lands before it, a drop on its lower third after it, and a drop on the middle inside it. A row cannot be a target if its path passes through the dragged node: that rules out every occurrence of the node and every row under one. While hovered, the target row carries `data-drop` (`before` | `inside` | `after`). The drop emits `move`. The consumer carries the move out and reloads afterwards.

With `dropType`, a drag from outside the tree can be dropped onto a row, if its `dataTransfer` carries data of that type. Such a drag drops inside the row, with the effect its source allows, and the row carries `data-drop="inside"` while hovered. The drop emits `drop(node, event)`: the row's node, and the drop event, whose data the consumer reads.

```ts
interface TreeNode {
    expandable: boolean
    [key: string]: unknown // must include [idField] and [labelField]
}
type TreeNodeId = string | number
type TreeLoader = (parentId: TreeNodeId | null) => Promise<TreeNode[]> // null → root level
interface TreeOccurrence {
    node: TreeNodeId
    parent: TreeNodeId | null // null: at the root level
}
type TreeMove = { node: TreeNodeId; from: TreeNodeId | null } & ({ position: 'inside'; to: TreeNodeId } | { position: 'before' | 'after'; to: TreeNodeId | null; relativeTo: TreeNodeId })
```

## Thumbnails

### ThumbnailGrid

Paged multi-select grid of thumbnails. Props: `loader: ThumbnailGridLoader` (required), `filters` (reloads from the first page when it changes), `thumbnail: (item) => MediaFile | null` (required: the file an item's tile shows, null for none; `MediaFile` is under `Gallery`), `titleField` (default `'title'`), `idField` (default `'id'`), `draggable` (default `false`). A page is as many tiles as the grid's area holds in full rows, measured from its layout; a resize that changes that number reloads, at the page holding the first tile shown. Give the grid a height (a flex item, say): a page fills it and never scrolls. `v-model:selected: Array<string | number>`. Tiles are picked as in a file manager, through `useSelection`: a click selects its tile alone, Ctrl/Cmd+click adds or removes the tile, and Shift+click selects the tiles from the last one picked to the one clicked, in page order. A selection band (`SelectionBand`) dragged over the tiles selects the tiles it touches in place of the selection; with Ctrl/Cmd or Shift held, it adds them to the selection. A click on the background, outside any tile, clears the selection, unless Ctrl/Cmd or Shift is held. The selection holds tiles of the page shown alone: a load drops the ids of items not on the page. Double-clicking a tile emits `open` with its item; the tile is left as the double-click's first click left it. Right-clicking a tile selects it alone, unless it is selected already, and emits `tilecontextmenu(items, event)`: the items of the selection, in page order, for a `ContextMenu` to open at the event. With `draggable`, the tiles are HTML-draggable. Dragging a tile selects it the same way, and emits `tiledragstart(items, event)` with the items of the selection, for the consumer to fill in the drag's `dataTransfer`. A band then starts only on the background (the padding, the gaps between tiles and the space below them), because a press on a tile starts a drag. Exposes `reload()`, which reloads the page shown (or the last page there is, once the page is past the items left), and `removeItems(ids)`, which takes items out of the page and out of the selection without a reload.

The pager below the tiles has a slider across its width, from the first page at the left to the last at the right. Dragging it shows the page it points at in the page field, and releasing it goes to that page. Under the slider are buttons to the first, previous, next and last pages, and the page field beside the number of pages. A page number typed into the field is gone to on Enter or when the field loses focus. A number that is no page is replaced with the page shown. The range of items shown, out of the total, is at the pager's end.

A tile shows its file as an image when the file's type is one a browser displays (JPEG, PNG, GIF, WebP, AVIF, SVG, BMP), or when its type is unknown, as for a bare image URL. Any other file shows as an icon of its kind: image, video, audio, document, spreadsheet, presentation, archive, or a plain file. The kind comes from the MIME type. The icons are `@greendrake/scss-kit`'s `file` icons. A tile with no file is left blank.

```ts
interface ThumbnailGridLoaderParams {
    offset: number
    limit: number
    [key: string]: unknown // the filters, spread in
}
type ThumbnailGridLoader = (params: ThumbnailGridLoaderParams, loading: Ref<boolean>) => Promise<{ data: ThumbnailGridItem[]; total: number }>
```

## Selection

### useSelection

`useSelection<Id extends string | number>(items: Ref<Record<string, unknown>[]> | ComputedRef<...>, idField: string, { keyField?: string, multi?: MaybeRefOrGetter<boolean>, model?: Ref<Id[]> })` — multi-selection over a flat, ordered list. The selection holds ids (`items[i][idField]`). The cursor is a row, and so is the anchor that ranges extend from. Rows are addressed by `items[i][keyField]` (default `idField`). The two fields differ when one item shows in several rows, as a tree node under two parents does. With `multi` off (it defaults to `true`), every pick and move selects one row. A `model` array, such as a component's `v-model`, mirrors the selection both ways: the selection starts as the model holds, and each follows the other's changes. Returns:

- `selected: Set<Id>` (shallowReactive);
- `cursor: ShallowRef<string | number | null>`: the cursor's row key. Setting it moves the cursor and nothing else;
- `getId(index)`;
- `replace(key)`: selects that row alone, and puts the cursor and the anchor on it;
- `selectOnly(ids)`: selects those ids alone, leaving the cursor and the anchor;
- `pick(key, event: MouseEvent)`: a pointer's pick. Shift selects the range from the anchor (with no anchor in the list, it toggles), Ctrl/Cmd toggles the row, and a plain pick replaces;
- `onKeyDown(event: KeyboardEvent): number`: Ctrl/Cmd+A selects all. ArrowUp/ArrowDown move the cursor and select its row alone, or with Shift the range from the anchor to it. Returns the cursor's new index, for the caller to scroll into view, or -1.

`RowSelect`, `TreeView` and `ThumbnailGrid` are built on it.

### RowSelect

`RowSelect<Id>(items, idField, onReorder?: (orderedIds: Id[]) => void)` — pointer-driven selection for a component that renders a `<table>`: click / Ctrl / Shift row selection via `useSelection`, a selection band (below) that selects the rows it touches, and, when `onReorder` is given, drag-to-reorder of rows via `useRowReorder` (below). A drag leaves the selection as the press found it. Returns `{ selected, containerRef, band, reorder, marks, onPointerDown, onKeyDown }`. Bind `containerRef` to the table's container, and attach `onPointerDown` / `onKeyDown` to it as listeners. Render `<SelectionBand :band="band" />` inside the container. `reorder` and `marks` are `useRowReorder`'s. Interactive descendants (`a`, `button`, inputs, `[role="button"]`) are left alone.

### useRowReorder

`useRowReorder<T>(containerRef, items: () => readonly T[], onReorder: (items: T[]) => void)` — drag-to-reorder of the rows of the `<tbody>` inside `containerRef`, one row per item, in the items' order. It moves one row at a time, with the pointer, and has no keyboard counterpart. Returns:

- `onPointerDown(event: PointerEvent)`: the container's press listener for a table whose rows a drag moves and nothing else does. A press on a row starts a drag of it (below), unless it lands on one of the row's links, buttons or form controls; it selects no text.
- `start(press: PointerEvent, fromIndex: number, onDrag?: () => void)`: call it from the press on the row at `fromIndex`, for a container whose presses do more than drag rows. A press of the primary mouse or pen button becomes a drag once the pointer has moved 5px; a touch scrolls instead. `onDrag` runs as the drag begins. The release hands `onReorder` the items with the row's item moved to the gap the pointer was nearest, unless that leaves it in its place. A cancelled drag, or one the component's unmount ends, moves nothing.
- `reorder: RowReorderState` (`{ active, fromIndex, toIndex }`), reactive: whether a drag is on, the dragged row's index, and the gap it would drop into, from 0 above the first row to the row count below the last.
- `marks(index)`: the attributes to bind on row `index` (`v-bind="marks(i)"`). While a drag is on, the dragged row has `data-dragged`, and the row the gap borders has `data-drop`: `before` it, or `after` the last row. scss-kit's `drop-marks` mixin styles them.
- `rowAt(element)`: the index of the row an element is in, or -1.
- `rows()`: the items' row elements.

### SelectionBand

The rectangle a drag draws to select the items it touches, as `RowSelect` and `ThumbnailGrid` have it. A press of the primary mouse or pen button starts it, once the pointer has moved 5px. A touch scrolls instead, and so does a press on the container's scrollbar. The band runs from the press to the pointer, and the selection follows it until the button is released.

`<SelectionBand :band="band" />` draws a band from its state (`{ active, left, top, width, height }`). Render it inside the band's container, and give the container a position (`relative`) so that it places the band. The coordinates are the container's content coordinates, so the band scrolls with the content. The band is filled and outlined with `--row-select-color`.

## Misc

- `InfoBlock` — a notice set apart by a border in `--infoblock-line-color` and by the `info` icon. An `actions` slot stacks controls beneath the message.
- `SwitchRow` — an on/off switch shaped as a row (`v-model`, `label`, `title`). It is an `<a role="switch">`, so that it can sit among menu items. Its layout comes from the class its parent gives it.
- `CircularProgress` — SVG ring, prop `progress: number` (0–100, clamped), `role="progressbar"`; colours via `--circular-progress-track` / `--circular-progress-arc` / `--circular-progress-label`.
- `RefreshButton` — a button showing the `refresh` icon, named `messages.refresh` (`aria-label` and tooltip). Its click is the host's listener.
- `Copy` — click-to-copy icon for `value: string`; `showValue` renders the value beside it; a `copied` class marks success for 2s. Title is `messages.clickToCopy`.
- `copyTextToClipboard(text: string): Promise<void>` — `navigator.clipboard.writeText`; requires a secure context.
- `downsizeImage(file: File, opts?: { maxDimension?: number; maxSizeKB?: number; minQuality?: number }): Promise<File>` — re-encodes an oversized image as JPEG, scaling to `maxDimension` (2000) and stepping quality down until under `maxSizeKB` (1024) or `minQuality` (0.2). Non-images, files within both limits, and undecodable files pass through untouched.
- `scrollToTop()` — `window.scroll({ top: 0 })`.
- `focusIn(el: string | HTMLElement | null, selector?: string)` — focuses the element with that id, or the first `selector` match inside `el`, unless it already has focus.
- `TOUCH_PAN_SLOP_PX` — 10; the distance in pixels a touch must move before it counts as a directional gesture; exported so that other components that handle touch gestures use the same value.
- `capturePointer(event: PointerEvent)` — captures the pointer to `event.currentTarget`, so a drag keeps tracking it outside the element; a pointer already released is a no-op.
- `useWidthDrag(minWidth: MaybeRefOrGetter<number>): WidthDrag` — a width dragged by a handle at its end edge, never below `minWidth`. Returns `{ active, start(event, width, set), move(event), end() }`: attach `start` to the handle's `pointerdown` (it captures the pointer and drags from `width`, passing each new width to `set`), `move` to `pointermove`, `end` to `pointerup` and `pointercancel`; `active` is true while a drag is in progress. `SplitPane` uses it.
