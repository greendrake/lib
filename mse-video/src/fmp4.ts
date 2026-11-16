// Reading the little the player needs of the fragmented MP4 (ISO/IEC 14496-12) a stream
// arrives in: the timescale an initialization segment gives its track, and a media
// segment's first frame — whether it is a key frame, and when it is presented.

export type Segment = { kind: 'init'; timescale: number } | { kind: 'media'; keyFrame: boolean; time: number }

interface Box {
    type: string
    // The box's content, past its header.
    start: number
    end: number
}

// A full box's optional fields, in the order they come past its version, flags and one
// 32-bit field, each by the flag that says it is there and its size in bytes.
type Fields = [flag: number, bytes: number][]

const TFHD_DEFAULT_SAMPLE_FLAGS = 0x20
// Those following the track_ID.
const TFHD_FIELDS: Fields = [
    [0x1, 8],
    [0x2, 4],
    [0x8, 4],
    [0x10, 4],
    [TFHD_DEFAULT_SAMPLE_FLAGS, 4]
]
const TRUN_FIRST_SAMPLE_FLAGS = 0x4
const TRUN_SAMPLE_FLAGS = 0x400
const TRUN_SAMPLE_COMPOSITION_TIME_OFFSET = 0x800
// Those following the sample_count, up to the end of the first sample's.
const TRUN_FIELDS: Fields = [
    [0x1, 4],
    [TRUN_FIRST_SAMPLE_FLAGS, 4],
    [0x100, 4],
    [0x200, 4],
    [TRUN_SAMPLE_FLAGS, 4],
    [TRUN_SAMPLE_COMPOSITION_TIME_OFFSET, 4]
]
// The sample flag that says a sample is not a sync sample: not a key frame.
const SAMPLE_IS_NON_SYNC = 0x10000

const boxesIn = (view: DataView, parent: Box): Box[] => {
    const boxes: Box[] = []
    for (let at = parent.start; at < parent.end;) {
        const size = view.getUint32(at)
        // 0 (up to the end) and 1 (a 64-bit size follows) are for boxes far larger than a
        // segment of a live stream.
        if (size < 8) {
            throw new Error(`Unsupported MP4 box size: ${size}`)
        }
        const type = String.fromCharCode(view.getUint8(at + 4), view.getUint8(at + 5), view.getUint8(at + 6), view.getUint8(at + 7))
        boxes.push({
            type,
            start: at + 8,
            end: at + size
        })
        at += size
    }
    return boxes
}

// The box down path from parent, each box in it the first of its type in the one before.
const boxAt = (view: DataView, parent: Box, ...path: string[]): Box =>
    path.reduce((outer, type) => {
        const box = boxesIn(view, outer).find(inner => inner.type === type)
        if (!box) {
            throw new Error(`No ${type} box in the MP4 ${outer.type}`)
        }
        return box
    }, parent)

// A full box's version: the first byte of its content.
const versionOf = (view: DataView, box: Box): number => view.getUint8(box.start)

// Where each of a full box's optional fields its flags say are there starts.
const fieldsAt = (view: DataView, box: Box, fields: Fields): Map<number, number> => {
    const flags = view.getUint32(box.start) & 0xffffff
    const offsets = new Map<number, number>()
    let at = box.start + 8
    for (const [flag, bytes] of fields) {
        if (flags & flag) {
            offsets.set(flag, at)
            at += bytes
        }
    }
    return offsets
}

export const readSegment = (segment: ArrayBuffer): Segment => {
    const view = new DataView(segment)
    const top: Box = {
        type: 'segment',
        start: 0,
        end: view.byteLength
    }
    if (boxesIn(view, top).some(box => box.type === 'moov')) {
        const mdhd = boxAt(view, top, 'moov', 'trak', 'mdia', 'mdhd')
        // Past the creation and modification times, which version 1 has 64-bit.
        return { kind: 'init', timescale: view.getUint32(mdhd.start + (versionOf(view, mdhd) === 1 ? 20 : 12)) }
    }
    const traf = boxAt(view, top, 'moof', 'traf')
    const tfhd = boxAt(view, traf, 'tfhd')
    const tfdt = boxAt(view, traf, 'tfdt')
    const trun = boxAt(view, traf, 'trun')
    const tfhdFields = fieldsAt(view, tfhd, TFHD_FIELDS)
    const trunFields = fieldsAt(view, trun, TRUN_FIELDS)
    const flagsAt = trunFields.get(TRUN_FIRST_SAMPLE_FLAGS) ?? trunFields.get(TRUN_SAMPLE_FLAGS) ?? tfhdFields.get(TFHD_DEFAULT_SAMPLE_FLAGS)
    // The initialization segment's trex can give a default too, which is not read: the
    // fragments of a stream this reads carry their own.
    if (flagsAt === undefined) {
        throw new Error('No sample flags in the MP4 fragment')
    }
    const decodeTime = versionOf(view, tfdt) === 1 ? Number(view.getBigUint64(tfdt.start + 4)) : view.getUint32(tfdt.start + 4)
    const offsetAt = trunFields.get(TRUN_SAMPLE_COMPOSITION_TIME_OFFSET)
    // Unsigned in version 0, signed in version 1.
    const offset = offsetAt === undefined ? 0 : versionOf(view, trun) === 0 ? view.getUint32(offsetAt) : view.getInt32(offsetAt)
    return {
        kind: 'media',
        keyFrame: (view.getUint32(flagsAt) & SAMPLE_IS_NON_SYNC) === 0,
        time: decodeTime + offset
    }
}
