// The engine's wire types (greendrake.nz/go/ipcam), the same for any camera, keyed as
// the Go types are.

export type StreamKey = 'main' | 'extra'

/** A stream's codec, by its common name: "H.265". */
export type Codec = string

export interface StreamState {
    Codec: Codec
    /** Whether the app casts the stream: only one codec is cast. */
    Cast: boolean
}

/** A camera's connection, as the app keeps it. */
export interface CameraState {
    State: 'online' | 'offline'
    Address: string
    /** Why the last dial failed, while offline; empty before the first. */
    Error: string
    /** Whether the camera has settings: known offline too. */
    Settings: boolean
    /** A write the camera takes only once restarted has been made since the last login. */
    RestartPending: boolean
    /** Each stream as the camera encoded it at the last login; absent before the first. */
    Streams?: Record<StreamKey, StreamState>
    /** The codec the engine casts. */
    Cast: Codec
}

/** Every camera's state, by its name. */
export type Cameras = Record<string, CameraState>

/** What a setting's value is and what bounds it, by its Type. */
export type Kind =
    | { Type: 'switch'; Value: boolean }
    // A whole number, within Min and Max where it has them.
    | { Type: 'integer'; Min?: number; Max?: number; Value: number }
    // A number from 0 to 1: a share of the picture, across or down from its top left.
    | { Type: 'fraction'; Value: number }
    // Of MinLength to MaxLength characters where it has them, none of them Excluded.
    | { Type: 'text'; MinLength?: number; MaxLength?: number; Excluded?: string; Value: string }
    // A dotted quad.
    | { Type: 'ipv4'; Value: string }
    // One of the options' values.
    | { Type: 'choice'; Options: Option[]; Value: string }
    // Shown, never written.
    | { Type: 'readonly'; Value: string }
    // Shown, and written only as Only.
    | { Type: 'pinned'; Only: string; Value: string }

/** One of a choice's: its value names what it means, never how a camera encodes it. */
export interface Option {
    Value: string
    Label: string
}

/** One of the camera's settings: described, of its kind, with its value. */
export type Setting = {
    Key: string
    Group: string
    Label: string
    /** What the label leaves unsaid; empty where it says it all. */
    Tip: string
} & Kind

/** A group of settings, which shows as a tab of its own. */
export interface Group {
    ID: string
    Title: string
}

/** The group of a camera's encoding settings, which the banner offers when a stream is not cast. */
export const ENCODE_GROUP = 'encode'

/** The group of a camera's upkeep settings, which shows the camera's clock and its reboot beside them. */
export const MAINTENANCE_GROUP = 'maintenance'

export interface SetResult {
    /** The camera took the write, but applies it only once restarted. */
    Restart: boolean
    /** Every setting the write can have changed, as the camera has them now. */
    Changed: Setting[]
}

/** The camera's wall clock, "2006-01-02 15:04:05", in no zone. */
export interface Clock {
    Time: string
}
