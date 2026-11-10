// A picture of a board (or of what is picked): everything drawn again on
// a canvas the way the page shows it.
import type { Obj } from './types'
import { FONT, arrowHead, fitSize, linePath, paper, penPath, rectOf, shapePath, shapeTextBox, stickyBox, tintSolid, union, wrap, type Rect } from './board'

const PAD = 60

function loadImage(src: string): Promise<HTMLImageElement | null> {
  return new Promise(res => {
    const i = new Image()
    i.onload = () => res(i)
    i.onerror = () => res(null)
    i.src = src
  })
}

function drawText(c: CanvasRenderingContext2D, text: string, box: Rect, size: number, colour: string, opts: { bold?: boolean; align?: 'left' | 'center' | 'right'; middle?: boolean } = {}) {
  const lines = wrap(text, box.w, size, opts.bold)
  const lh = size * 1.25
  c.fillStyle = colour
  c.font = `${opts.bold ? 600 : 400} ${size}px ${FONT}`
  c.textBaseline = 'top'
  const align = opts.align || 'center'
  c.textAlign = align
  const total = lines.length * lh
  let y = opts.middle === false ? box.y : box.y + Math.max(0, (box.h - total) / 2)
  const x = align === 'left' ? box.x : align === 'right' ? box.x + box.w : box.x + box.w / 2
  for (const l of lines) { c.fillText(l, x, y + (lh - size) / 2); y += lh }
}

/** Draws the objects into a PNG blob, `scale` pixels per board unit. */
export async function boardPNG(objs: Obj[], all: Map<string, Obj>, opts: { scale?: number; background?: string; imageSrc?: (o: Obj) => string } = {}): Promise<Blob> {
  const bounds = union(objs.map(o => rectOf(o, all)).concat(objs.filter(o => o.kind === 'frame').map(f => ({ x: f.x, y: f.y - 30, w: f.w, h: 30 }))))
  if (!bounds) throw new Error('there is nothing to draw')
  let scale = opts.scale || 2
  const maxPx = 12000
  scale = Math.min(scale, maxPx / (bounds.w + PAD * 2), maxPx / (bounds.h + PAD * 2))
  const canvas = document.createElement('canvas')
  canvas.width = Math.ceil((bounds.w + PAD * 2) * scale)
  canvas.height = Math.ceil((bounds.h + PAD * 2) * scale)
  const c = canvas.getContext('2d')!
  c.fillStyle = opts.background || '#ffffff'
  c.fillRect(0, 0, canvas.width, canvas.height)
  c.scale(scale, scale)
  c.translate(PAD - bounds.x, PAD - bounds.y)
  const imgs = new Map<string, HTMLImageElement | null>()
  await Promise.all(objs.filter(o => o.kind === 'image').map(async o => { imgs.set(o.id, await loadImage(opts.imageSrc ? opts.imageSrc(o) : o.data.src || '')) }))
  const sorted = [...objs].sort((a, b) => a.z - b.z)
  for (const o of sorted) {
    const d = o.data
    c.save()
    switch (o.kind) {
      case 'frame': {
        c.fillStyle = tintSolid(d.fill)
        c.strokeStyle = '#c8ccd2'
        c.lineWidth = 1.5
        c.fillRect(o.x, o.y, o.w, o.h)
        c.strokeRect(o.x, o.y, o.w, o.h)
        c.fillStyle = '#4a515c'
        c.font = `600 20px ${FONT}`
        c.textBaseline = 'bottom'
        c.textAlign = 'left'
        c.fillText(d.title || 'Frame', o.x + 2, o.y - 8)
        break
      }
      case 'sticky': {
        const p = paper(d.color || 'yellow')
        c.shadowColor = 'rgba(0,0,0,.18)'
        c.shadowBlur = 10
        c.shadowOffsetY = 4
        c.fillStyle = p.bg
        c.fillRect(o.x, o.y, o.w, o.h)
        c.shadowColor = 'transparent'
        const b = stickyBox(o)
        const box = { x: o.x + b.x, y: o.y + b.y, w: b.w, h: b.h }
        const size = d.size || fitSize(d.text || '', box.w, box.h, Math.round(o.w * 0.2), 8)
        if (d.text) drawText(c, d.text, box, size, p.ink, { align: d.align || 'center' })
        break
      }
      case 'text': {
        drawText(c, d.text || '', { x: o.x, y: o.y, w: o.w, h: o.h }, d.size || 24, d.color || '#1d2024', { bold: d.bold, align: d.align || 'left', middle: false })
        break
      }
      case 'shape': {
        const p = paper(d.fill || 'white')
        c.translate(o.x, o.y)
        const path = new Path2D(shapePath(d.shape, o.w, o.h))
        if (d.fill !== 'none') { c.fillStyle = p.bg; c.fill(path) }
        c.strokeStyle = d.stroke || '#3d434c'
        c.lineWidth = d.width || 2
        c.stroke(path)
        const tb = shapeTextBox(d.shape, o.w, o.h)
        if (d.text) drawText(c, d.text, tb, d.size || fitSize(d.text, tb.w, tb.h, 28, 8), d.fill === 'none' ? '#1d2024' : p.ink, { bold: d.bold, align: d.align || 'center' })
        break
      }
      case 'line': {
        const g = linePath(o, all)
        c.strokeStyle = d.color || '#3d434c'
        c.lineWidth = d.width || 2.5
        c.lineCap = 'round'
        c.lineJoin = 'round'
        if (d.dash) c.setLineDash([10, 8])
        c.stroke(new Path2D(g.d))
        c.setLineDash([])
        const head = 10 + (d.width || 2.5) * 2
        if ((d.end ?? 'arrow') === 'arrow') c.stroke(new Path2D(arrowHead(g.b, g.angB, head)))
        if (d.start === 'arrow') c.stroke(new Path2D(arrowHead(g.a, g.angA, head)))
        if (d.text) {
          c.font = `500 18px ${FONT}`
          const tw = c.measureText(d.text).width
          c.fillStyle = '#ffffff'
          c.fillRect(g.mid.x - tw / 2 - 6, g.mid.y - 14, tw + 12, 28)
          c.fillStyle = '#1d2024'
          c.textAlign = 'center'
          c.textBaseline = 'middle'
          c.fillText(d.text, g.mid.x, g.mid.y)
        }
        break
      }
      case 'pen': {
        c.translate(o.x, o.y)
        c.strokeStyle = d.color || '#1d2024'
        c.lineWidth = d.width || 4
        c.lineCap = 'round'
        c.lineJoin = 'round'
        if (d.highlight) c.globalAlpha = 0.35
        c.stroke(new Path2D(penPath(d.points || [])))
        break
      }
      case 'image': {
        const img = imgs.get(o.id)
        if (img) c.drawImage(img, o.x, o.y, o.w, o.h)
        else { c.fillStyle = '#e3e5e8'; c.fillRect(o.x, o.y, o.w, o.h) }
        break
      }
    }
    c.restore()
  }
  return new Promise((res, rej) => canvas.toBlob(b => (b ? res(b) : rej(new Error('could not make the picture'))), 'image/png'))
}
