// A picture of a board (or of what is picked): everything drawn again on
// a canvas the way the page shows it.
import type { Obj } from './types'
import { FONT, OUTLINE_ONLY, actorPath, arrowHead, fitSize, linePath, paper, penPath, rectOf, shapeLines, shapePath, shapeTextBox, stickyBox, tintSolid, union, wrap, type Rect } from './board'
import { iconPath } from '../components/icons'

const PAD = 60

function loadImage(src: string): Promise<HTMLImageElement | null> {
  return new Promise(res => {
    const i = new Image()
    // pictures from other sites only with their permission (else the canvas cannot be saved)
    try { if (new URL(src, location.href).origin !== location.origin) i.crossOrigin = 'anonymous' } catch { /* a relative address */ }
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

type Opts = { scale?: number; background?: string; imageSrc?: (o: Obj) => string; bounds?: Rect; pad?: number; maxPx?: number }

/** Draws the objects into a PNG blob, `scale` pixels per board unit. */
export async function boardPNG(objs: Obj[], all: Map<string, Obj>, opts: Opts = {}): Promise<Blob> {
  const canvas = await boardCanvas(objs, all, opts)
  return new Promise((res, rej) => canvas.toBlob(b => (b ? res(b) : rej(new Error('could not make the picture'))), 'image/png'))
}

/** Draws the objects on a canvas (just the part in `bounds`, when given). */
export async function boardCanvas(objs: Obj[], all: Map<string, Obj>, opts: Opts = {}): Promise<HTMLCanvasElement> {
  const pad = opts.pad ?? PAD
  const bounds = opts.bounds || union(objs.map(o => rectOf(o, all)).concat(objs.filter(o => o.kind === 'frame').map(f => ({ x: f.x, y: f.y - 30, w: f.w, h: 30 }))))
  if (!bounds) throw new Error('there is nothing to draw')
  let scale = opts.scale || 2
  const maxPx = opts.maxPx || 12000
  scale = Math.min(scale, maxPx / (bounds.w + pad * 2), maxPx / (bounds.h + pad * 2))
  const canvas = document.createElement('canvas')
  canvas.width = Math.ceil((bounds.w + pad * 2) * scale)
  canvas.height = Math.ceil((bounds.h + pad * 2) * scale)
  const c = canvas.getContext('2d')!
  c.fillStyle = opts.background || '#ffffff'
  c.fillRect(0, 0, canvas.width, canvas.height)
  c.scale(scale, scale)
  c.translate(pad - bounds.x, pad - bounds.y)
  if (opts.bounds) { c.beginPath(); c.rect(bounds.x, bounds.y, bounds.w, bounds.h); c.clip() }
  const imgs = new Map<string, HTMLImageElement | null>()
  await Promise.all(objs.filter(o => o.kind === 'image' || (o.kind === 'embed' && o.data.image)).map(async o => { imgs.set(o.id, await loadImage(o.kind === 'image' ? (opts.imageSrc ? opts.imageSrc(o) : o.data.src || '') : o.data.image!)) }))
  const sorted = [...objs].sort((a, b) => a.z - b.z)
  for (const o of sorted) {
    const d = o.data
    c.save()
    if (d.rot && o.kind !== 'line' && o.kind !== 'pen' && o.kind !== 'frame') {
      c.translate(o.x + o.w / 2, o.y + o.h / 2)
      c.rotate(d.rot * Math.PI / 180)
      c.translate(-(o.x + o.w / 2), -(o.y + o.h / 2))
    }
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
        const p = paper(d.color || 'light-yellow')
        c.shadowColor = 'rgba(0,0,0,.22)'
        c.shadowBlur = 9
        c.shadowOffsetY = 5
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
        c.strokeStyle = d.stroke || '#3d434c'
        c.lineWidth = d.width || 2
        if (d.dash) c.setLineDash([(d.width || 2) * 4, (d.width || 2) * 3])
        if (d.shape && OUTLINE_ONLY.has(d.shape)) {
          c.lineCap = 'round'
          c.stroke(new Path2D(actorPath(o.w, o.h)))
        } else {
          const path = new Path2D(shapePath(d.shape, o.w, o.h))
          if (d.fill !== 'none') { c.fillStyle = p.bg; c.fill(path) }
          c.stroke(path)
          const extra = shapeLines(d.shape, o.w, o.h)
          if (extra) c.stroke(new Path2D(extra))
        }
        c.setLineDash([])
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
      case 'sticker': {
        c.font = `${Math.min(o.w, o.h) * 0.86}px ${FONT}`
        c.textAlign = 'center'
        c.textBaseline = 'middle'
        c.fillText(d.emoji || '⭐', o.x + o.w / 2, o.y + o.h / 2 + o.h * 0.04)
        break
      }
      case 'icon': {
        c.translate(o.x, o.y)
        c.scale(o.w / 24, o.h / 24)
        c.strokeStyle = d.color || '#1d2024'
        c.lineWidth = 1.75
        c.lineCap = 'round'
        c.lineJoin = 'round'
        c.stroke(new Path2D(iconPath(d.icon || 'star')))
        break
      }
      case 'card': {
        c.shadowColor = 'rgba(0,0,0,.14)'; c.shadowBlur = 8; c.shadowOffsetY = 2
        c.fillStyle = '#ffffff'
        c.beginPath(); c.roundRect(o.x, o.y, o.w, o.h, 8); c.fill()
        c.shadowColor = 'transparent'
        c.fillStyle = paper(d.color || 'blue').bg
        c.beginPath(); c.roundRect(o.x, o.y, 8, o.h, [8, 0, 0, 8]); c.fill()
        drawText(c, d.text || 'Card', { x: o.x + 22, y: o.y + 16, w: o.w - 40, h: o.h - 60 }, 18, d.done ? '#8a9099' : '#1d2024', { bold: true, align: 'left', middle: false })
        c.font = `500 13px ${FONT}`; c.fillStyle = '#5b6472'; c.textAlign = 'left'; c.textBaseline = 'alphabetic'
        c.fillText([d.due ? `Due ${d.due}` : '', d.assignee?.name || '', d.done ? 'Done' : ''].filter(Boolean).join('  ·  '), o.x + 22, o.y + o.h - 16)
        break
      }
      case 'embed': {
        c.fillStyle = '#ffffff'; c.strokeStyle = '#d6dae0'; c.lineWidth = 1.5
        c.beginPath(); c.roundRect(o.x, o.y, o.w, o.h, 10); c.fill(); c.stroke()
        const img = imgs.get(o.id)
        const tall = d.embed === 'youtube'
        if (img) {
          c.save(); c.beginPath(); c.roundRect(o.x, o.y, tall ? o.w : Math.min(o.h, o.w * 0.35), tall ? o.h : o.h, 10); c.clip()
          c.drawImage(img, o.x, o.y, tall ? o.w : Math.min(o.h, o.w * 0.35), o.h); c.restore()
        }
        if (!tall) {
          const tx = o.x + (img ? Math.min(o.h, o.w * 0.35) : 0) + 14
          drawText(c, d.title || d.name || d.url || '', { x: tx, y: o.y + 14, w: o.x + o.w - tx - 14, h: o.h * 0.5 }, 16, '#1d2024', { bold: true, align: 'left', middle: false })
          c.font = `400 12px ${FONT}`; c.fillStyle = '#6b7280'; c.textAlign = 'left'; c.textBaseline = 'alphabetic'
          c.fillText(d.site || d.mime || '', tx, o.y + o.h - 14)
        }
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
  return canvas
}
