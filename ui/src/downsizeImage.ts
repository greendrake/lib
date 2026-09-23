interface Options {
    maxDimension?: number
    maxSizeKB?: number
    minQuality?: number
}

const DEFAULTS = {
    maxDimension: 2000,
    maxSizeKB: 1024,
    minQuality: 0.2
}

const canvasToBlob = (canvas: HTMLCanvasElement, type: string, quality: number): Promise<Blob> =>
    new Promise((resolve, reject) => {
        canvas.toBlob(blob => (blob ? resolve(blob) : reject(new Error('toBlob returned null'))), type, quality)
    })

const loadImage = (file: File): Promise<HTMLImageElement> =>
    new Promise((resolve, reject) => {
        const url = URL.createObjectURL(file)
        const img = new Image()
        img.onload = () => {
            URL.revokeObjectURL(url)
            resolve(img)
        }
        img.onerror = () => {
            URL.revokeObjectURL(url)
            reject(new Error('Failed to load image'))
        }
        img.src = url
    })

// Re-encodes an oversized image as JPEG, scaling it to fit maxDimension and
// stepping quality down until it fits maxSizeKB (or minQuality is reached).
// Non-images and files already within both limits pass through untouched, as
// do files the browser cannot decode.
export default async function downsizeImage(file: File, opts?: Options): Promise<File> {
    if (!file.type.startsWith('image/')) return file

    const { maxDimension, maxSizeKB, minQuality } = { ...DEFAULTS, ...opts }
    const maxSizeBytes = maxSizeKB * 1024

    let img: HTMLImageElement
    try {
        img = await loadImage(file)
    } catch {
        return file
    }
    let { width, height } = img
    if (file.size <= maxSizeBytes && width <= maxDimension && height <= maxDimension) {
        return file
    }

    if (width > maxDimension || height > maxDimension) {
        const ratio = Math.min(maxDimension / width, maxDimension / height)
        width = Math.round(width * ratio)
        height = Math.round(height * ratio)
    }

    const canvas = document.createElement('canvas')
    canvas.width = width
    canvas.height = height
    const ctx = canvas.getContext('2d')!
    ctx.drawImage(img, 0, 0, width, height)

    let quality = 0.92
    let blob = await canvasToBlob(canvas, 'image/jpeg', quality)

    while (blob.size > maxSizeBytes && quality > minQuality) {
        quality -= 0.05
        blob = await canvasToBlob(canvas, 'image/jpeg', quality)
    }

    canvas.remove()

    const name = file.name.replace(/\.[^.]+$/, '.jpg')
    return new File([blob], name, { type: 'image/jpeg' })
}
