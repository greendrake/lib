import { wireXhr } from './xhrProgress'

// Fetch a blob with optional percentage progress. Exposes the blob — saving
// is a separate, explicit step (saveBlob), so callers can also display or
// upload it.
export const downloadBlob = (url: string, onProgress?: (percent: number) => void): Promise<Blob> =>
    new Promise<Blob>((resolve, reject) => {
        const xhr = new XMLHttpRequest()
        xhr.responseType = 'blob'
        wireXhr(xhr, onProgress ?? (() => {}), response => resolve(response as Blob), reject, false)
        xhr.open('GET', url)
        xhr.send()
    })

export const saveBlob = (blob: Blob, filename: string): void => {
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.style.display = 'none'
    a.href = url
    a.download = filename
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
}
