// Distance a touch/pen pointer must travel before the gesture counts as a
// deliberate directional move rather than a tap or start-of-gesture jitter —
// mirrors typical platform touch slop. Exported so every component arbitrating
// gestures — the sheet drag in Modal, a table's pan — shares one value.
export const TOUCH_PAN_SLOP_PX = 10
