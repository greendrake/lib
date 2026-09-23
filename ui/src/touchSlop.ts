// Distance a touch/pen pointer must travel before the gesture counts as a
// deliberate directional move rather than a tap or start-of-gesture jitter —
// mirrors typical platform touch slop. Its own module so the components that
// arbitrate gestures (the sheet drag in Modal, the pan in InfiniteScrollTable)
// share one value without importing each other.
export const TOUCH_PAN_SLOP_PX = 10
