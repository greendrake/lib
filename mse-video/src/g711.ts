// G.711 decoding (ITU-T G.711), which no browser media pipeline offers: a table from
// each 8-bit code to its sample.

export type G711Codec = 'alaw' | 'ulaw'

const alawSample = (code: number): number => {
    const a = code ^ 0x55
    const segment = (a & 0x70) >> 4
    let magnitude = (a & 0x0f) << 4
    magnitude += segment === 0 ? 8 : 0x108
    if (segment > 1) {
        magnitude <<= segment - 1
    }
    return a & 0x80 ? magnitude : -magnitude
}

const ulawSample = (code: number): number => {
    const u = ~code & 0xff
    const magnitude = (((u & 0x0f) << 3) + 0x84) << ((u & 0x70) >> 4)
    return u & 0x80 ? 0x84 - magnitude : magnitude - 0x84
}

const table = (sample: (code: number) => number): Float32Array => Float32Array.from({ length: 256 }, (_, code) => sample(code) / 32768)

const tables: Record<G711Codec, Float32Array> = {
    alaw: table(alawSample),
    ulaw: table(ulawSample)
}

// One sample per byte, scaled to [-1, 1).
export const decodeG711 = (codec: G711Codec, codes: Uint8Array): Float32Array<ArrayBuffer> => {
    const decode = tables[codec]
    const samples = new Float32Array(codes.length)
    for (let i = 0; i < codes.length; i++) {
        samples[i] = decode[codes[i]]
    }
    return samples
}
