import type { GalleryItem } from './Gallery.vue'

/** A file shown as media: where it is, and its MIME type — null where unknown, as of a bare image URL. */
export interface MediaFile {
    url: string
    mime: string | null
}

// The image types a browser displays.
const displayedImages = new Set(['image/jpeg', 'image/png', 'image/gif', 'image/webp', 'image/avif', 'image/svg+xml', 'image/bmp'])

/** A file's type alone, lower-cased ("text/plain; charset=utf-8" is "text/plain"); null where unknown. */
export const mediaType = (file: MediaFile): string | null => file.mime?.split(';')[0].trim().toLowerCase() ?? null

/** Whether a browser displays images of the type. */
export const displayedImage = (type: string): boolean => displayedImages.has(type)

/**
 * The Gallery item a file shows as: an image a browser displays — or one of no
 * type known, as a bare image URL — or a video; null for a file of any other
 * type, which the Gallery cannot show.
 */
export const galleryItem = (file: MediaFile, alt: string): GalleryItem | null => {
    const type = mediaType(file)
    if (type === null || displayedImage(type)) {
        return { url: file.url, alt }
    }
    if (type.startsWith('video/')) {
        return {
            url: file.url,
            alt,
            video: true
        }
    }
    return null
}
