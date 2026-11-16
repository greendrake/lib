// Fragmented MP4 to read: boxes, and segments laid out as a webcast server's.
import { concatArrayBuffers } from '../src/bytes'

// Sample flags: a key frame, and a frame that depends on others.
export const KEY_FRAME = 0x2000000
export const DEPENDENT_FRAME = 0x1010000

const bytes = (size: number, write: (view: DataView) => void): ArrayBuffer => {
    const out = new ArrayBuffer(size)
    write(new DataView(out))
    return out
}

export const u32 = (value: number): ArrayBuffer => bytes(4, view => view.setUint32(0, value))
export const i32 = (value: number): ArrayBuffer => bytes(4, view => view.setInt32(0, value))
export const u64 = (value: number): ArrayBuffer => bytes(8, view => view.setBigUint64(0, BigInt(value)))

export const box = (type: string, ...content: ArrayBuffer[]): ArrayBuffer => {
    const body = concatArrayBuffers(...content)
    return concatArrayBuffers(u32(8 + body.byteLength), Uint8Array.from(type, char => char.charCodeAt(0)).buffer, body)
}

export const fullBox = (type: string, version: number, flags: number, ...content: ArrayBuffer[]): ArrayBuffer => box(type, u32(version * 0x1000000 + flags), ...content)

export const initSegment = (timescale: number, mdhdVersion = 0): ArrayBuffer => {
    const time = mdhdVersion === 1 ? u64(0) : u32(0)
    return concatArrayBuffers(box('ftyp', u32(0)), box('moov', box('mvhd'), box('trak', box('tkhd'), box('mdia', fullBox('mdhd', mdhdVersion, 0, time, time, u32(timescale), time), box('hdlr'), box('minf'))), box('mvex', box('trex'))))
}

// A media segment of one frame, its traf holding boxes.
export const fragment = (...boxes: ArrayBuffer[]): ArrayBuffer => concatArrayBuffers(box('moof', fullBox('mfhd', 0, 0, u32(1)), box('traf', ...boxes)), box('mdat', u32(0)))

export const mediaSegment = (flags: number, decodeTime: number): ArrayBuffer => fragment(fullBox('tfhd', 0, 0x020038, u32(1), u32(3600), u32(4), u32(flags)), fullBox('tfdt', 1, 0, u64(decodeTime)), fullBox('trun', 0, 0x1, u32(1), u32(0)))
