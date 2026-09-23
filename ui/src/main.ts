export { showToast, type ToastHandle, type ToastOptions, type ToastVariant } from './toast/showToast'
export { messages, configureUiMessages, type UiMessages } from './messages'
export { default as Loading } from './Loading.vue'
export { default as TabBar } from './TabBar.vue'
export type { TabDef } from './TabBar.vue'
export { default as Splash } from './Splash.vue'

// Form controls
export { default as TextField } from './TextField.vue'
export { default as Password } from './Password.vue'
export { default as Email } from './Email.vue'
export { default as Checkbox } from './Checkbox.vue'
export { default as CurrencyInput } from './CurrencyInput.vue'
export { default as Field } from './Field.vue'
export { default as FieldTip } from './FieldTip'
export { validationScopeKey } from './validationScope'

// Form submission state machine
export { default as FormC } from './Form.vue'
export { default as AsyncAction } from './AsyncAction.vue'
export { default as OptionSwitch } from './OptionSwitch.vue'
export type { OptionValue, OptionsArray, OptionsMap } from './OptionSwitch.vue'
export { default as SegmentedControl } from './SegmentedControl.vue'
export type { SegmentedOption } from './SegmentedControl.vue'

// Search
export { default as SearchField } from './SearchField.vue'
export { default as AsyncSearch } from './AsyncSearch.vue'
export type { SearchFunction } from './AsyncSearch.vue'
export type { SearchResultItem } from './AsyncSearchResults.vue'

// Overlay / navigation
export { default as Modal } from './Modal.vue'
export { default as ModalOnClick } from './ModalOnClick.vue'
export { default as MenuButton } from './MenuButton.vue'
export { default as SelectBox } from './SelectBox.vue'
export type { SelectBoxOption } from './SelectBox.vue'

// Media / collections
export { default as Gallery } from './Gallery.vue'
export type { GalleryItem } from './Gallery.vue'
export { default as ThumbnailGrid } from './ThumbnailGrid.vue'
export type { ThumbnailGridItem, ThumbnailGridLoader, ThumbnailGridLoaderParams } from './ThumbnailGrid.vue'
export { default as TreeView } from './TreeView.vue'
export type { TreeNode, TreeNodeId, TreeLoader } from './TreeView.vue'
export { default as Accordion } from './Accordion.vue'
export type { AccordionSection } from './Accordion.vue'
export { default as useSelection } from './useSelection'
export { default as RowSelect } from './RowSelect'
export { default as CircularProgress } from './CircularProgress.vue'
export { default as downsizeImage } from './downsizeImage'
export { default as Copy } from './Copy.vue'
export { copyTextToClipboard } from './copyTextToClipboard'

// Layout shell
export { default as TheWrap } from './TheWrap.vue'
export { default as BottomNav } from './BottomNav.vue'
export type { BottomNavTab } from './BottomNav.vue'

// Gestures
export { TOUCH_PAN_SLOP_PX } from './touchSlop'

// Small DOM conveniences
export const scrollToTop = (): void => window.scroll({ top: 0 })

export const focusIn = (el: string | HTMLElement | null, selector?: string): void => {
    if (el) {
        let input: Element | null = null
        if (typeof el === 'string') {
            input = document.getElementById(el)
        } else if (selector) {
            input = el.querySelector(selector)
        }
        if (input instanceof HTMLElement && document.activeElement !== input) {
            input.focus()
        }
    }
}
