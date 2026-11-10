// Pictures are shrunk in the browser before they go up (2000 px, and a
// 480 px thumbnail); other files go as they are.
import { ApiError } from './api'

async function toCanvas(file: Blob, max: number): Promise<HTMLCanvasElement> {
  const url = URL.createObjectURL(file)
  try {
    const img = await new Promise<HTMLImageElement>((res, rej) => { const i = new Image(); i.onload = () => res(i); i.onerror = rej; i.src = url })
    const scale = Math.min(1, max / Math.max(img.naturalWidth, img.naturalHeight))
    const c = document.createElement('canvas')
    c.width = Math.round(img.naturalWidth * scale)
    c.height = Math.round(img.naturalHeight * scale)
    c.getContext('2d')!.drawImage(img, 0, 0, c.width, c.height)
    return c
  } finally { URL.revokeObjectURL(url) }
}

const blobOf = (c: HTMLCanvasElement, type: string, q = 0.86) => new Promise<Blob>((res, rej) => c.toBlob(b => b ? res(b) : rej(new Error('could not read the picture')), type, q))

export interface Uploaded { url: string; upload: { id: number; name: string; mime: string; width: number; height: number } }

/** Puts a picture up for a board: shrunk to 2000 px with a 480 px thumbnail. */
export async function uploadPicture(file: File, url: string, headers: Record<string, string> = {}): Promise<Uploaded & { w: number; h: number }> {
  if (!/^image\/(jpeg|png|webp|gif)$/.test(file.type)) throw new Error('pictures only (JPEG, PNG, WebP or GIF)')
  const fd = new FormData()
  let name = file.name || 'picture'
  let w = 0, h = 0
  if (file.type !== 'image/gif') {
    const big = await toCanvas(file, 2000)
    const small = await toCanvas(file, 480)
    const type = file.type === 'image/png' && file.size < 2 * 1024 * 1024 ? 'image/png' : 'image/jpeg'
    if (type === 'image/jpeg') name = name.replace(/\.(png|webp)$/i, '.jpg')
    fd.append('file', await blobOf(big, type), name)
    fd.append('thumb', await blobOf(small, 'image/jpeg', 0.8), 'thumb.jpg')
    w = big.width; h = big.height
  } else {
    const c = await toCanvas(file, 4000)
    w = c.width; h = c.height
    fd.append('file', file, name)
  }
  fd.append('width', String(w))
  fd.append('height', String(h))
  const res = await fetch(url, { method: 'POST', body: fd, credentials: 'same-origin', headers })
  const j = await res.json().catch(() => ({}))
  if (!res.ok) throw new ApiError(res.status, j.error || res.statusText, j)
  return { ...(j as Uploaded), w, h }
}
