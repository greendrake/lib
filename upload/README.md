# @greendrake/upload

File transfer in the browser, with progress: uploading a file straight to a URL the server hands out, downloading a blob, saving it to disk, and a multi-file uploader component for Vue.

```sh
bun add @greendrake/upload
```

The package runs in the browser only: it is built on `XMLHttpRequest`. It ships `src/main.ts` and `src/vue/main.ts` as they are, with no compiled output and no `.d.ts`, so the consuming app's bundler has to compile TypeScript, as Vite does. The root entry is framework-free. `./vue` needs `vue` (^3.5.34), an optional peer dependency that the app importing it installs.

## Why

`fetch` does not report how much of an upload has gone, so a page that shows a progress bar has to use `XMLHttpRequest` and wire its events. It then needs the rest of what a real upload takes: a retry when the network drops, a way to cancel, and a rule for when retrying is pointless.

Direct uploads add traps of their own. There the server hands out a presigned URL, and the browser `PUT`s the file straight to object storage. The `Content-Type` must be the one the URL was signed for. A `403` means the URL has expired or does not match, so no retry will ever help. This package is that code, written once.

## Trade-offs

- **One request per file.** There is no chunked or resumable upload. A retry sends the file again from its first byte, so a very large file over a poor connection may never get through. For that, use a resumable protocol such as S3 multipart upload or tus.
- **A download is held in memory.** `downloadBlob` resolves with the whole response as a `Blob`. That is fine for documents and images, and wrong for files larger than the page can afford to hold.

## Overview

- `Uploader` sends one file to one URL. It reports progress, retries a failure that another attempt can get past, gives up on one that it cannot, and aborts on request.
- `downloadBlob` and `saveBlob` are for the other direction: a download with progress, and saving the result to disk.
- `FileUploader`, at `./vue`, is the usual interface on top: a drop target, a row per file with its progress, and removal.

The package never talks to your server. Getting the URL for a file is a call your app makes, so the package works with any backend and any object store.

## Settling

Every request settles the way `@greendrake/util`'s `fetchJson` does:

- a 2xx resolves;
- any other status rejects with `HttpError` (`status`, `statusText`);
- a request that gets no answer — the network, CORS, a timeout — rejects with `NetworkError`;
- an abort rejects with an `AbortError` `DOMException`.

## `Uploader`

One file's upload to a URL the server has handed out for it: a presigned object-store URL, or an ingestion endpoint of the server's own.

```ts
import { Uploader } from '@greendrake/upload'

const { url } = await api.call('file.book', { name: file.name })
const uploader = new Uploader({ url, file, mime: file.type, verb: 'PUT' })
uploader.on('progress', percent => (shown.value = percent))
await uploader.send()
```

`UploaderOptions`:

- `url`, and `verb`: `PUT` (the default) or `POST`.
- `file`: the `Blob` sent as the body. A `File` is one.
- `mime`: the `Content-Type` sent. It is required, because a presigned URL is signed for one type. The caller decides which (with `@greendrake/util`'s `fileMime`, say); the package never guesses.
- `shouldRetry(reason)`: whether a failed attempt is tried again. `reason` is how the attempt settled (see Settling). The default retries everything except a `403`, so a `404` or a `413` is retried too.

Retries do not stop on their own. A failure that `shouldRetry` accepts is retried 4 seconds later, until the upload succeeds or `abort()` is called. Pass `shouldRetry` to give up sooner.

`send()` resolves once the server has the file. It rejects with the failure `shouldRetry` gave up on. The `Uploader` is an `Emitter` of `UploaderEvents`:

- `progress`, with the percentage: 0–99 while the bytes go, 100 once the server has answered.
- `finished`: the upload is done.
- `cancelled`: `abort()` stopped it. `send()` then resolves without `finished`.

`abort()` stops the upload that is under way and any retry that is waiting. It does nothing once the upload has finished.

## Downloads

`downloadBlob(url, onProgress?)` resolves with the response as a `Blob`, calling `onProgress` with the percentage as it arrives. `saveBlob(blob, filename)` has the browser save the blob under `filename`. They are two steps so that a downloaded blob can just as well be shown, or uploaded elsewhere.

## `FileUploader` (`./vue`)

A multi-file uploader. Its drop target is also the file picker. Each file gets a row with its progress and a button that removes it, aborting the upload if it is still under way.

```vue
<FileUploader :book="book" @uploaded="files.push($event)" @removed="arrayRemove(files, $event)" @uploading="busy = $event" />
```

- `book(file)` is the app's function that gets the upload URL for a file. It resolves to an `UploadBooking`, `{ url, verb?, fileId }`: where the file goes, and the id the server will know it by afterwards. If it rejects, showing the error is the app's job, and the file's row is dropped.
- `mime(file)`: the type each upload is sent with. The default is `fileMime`.
- `prompt`, `uploadingText`, `attachedText`: the texts the component shows.
- Events: `uploaded` and `removed` carry an `UploadedFile`, `{ id, name, size }`. It is the same object for both, so the app can track its collection by identity. `uploading` says whether any upload is under way.

Its look comes from CSS custom properties, each with a default: `--file-upload-bg` (the drop target), `--uploader-row-bg` (a row's caption and button), `--uploader-text-color`, `--uploader-bar-color` and `--uploader-track-color` (the progress bar). It also reads `--space-2` for its margins. That property has no default: the page has to define it.
