// What a board is made of, drawn the same on screen and in a picture:
// colours, the geometry of shapes and lines, and how much text fits.
import type { Obj, ObjData, ShapeForm, PreviewShape } from './types'

// ---- colours ----

/** Sticky-note colours (paper on screen; also fills for shapes and frames). */
export const STICKY: Record<string, { bg: string; ink: string; name: string }> = {
  yellow: { bg: '#fff176', ink: '#3b3200', name: 'Yellow' },
  orange: { bg: '#ffc46b', ink: '#3d2400', name: 'Orange' },
  pink: { bg: '#ffb3d1', ink: '#46102a', name: 'Pink' },
  red: { bg: '#ff8a80', ink: '#470b06', name: 'Red' },
  violet: { bg: '#d0b8ff', ink: '#2a1052', name: 'Violet' },
  blue: { bg: '#9ecbff', ink: '#062a52', name: 'Blue' },
  cyan: { bg: '#8ef0e8', ink: '#003b37', name: 'Turquoise' },
  green: { bg: '#b8ef9d', ink: '#123d05', name: 'Green' },
  lime: { bg: '#e2f78a', ink: '#2d3600', name: 'Lime' },
  gray: { bg: '#e3e5e8', ink: '#24272c', name: 'Grey' },
  white: { bg: '#ffffff', ink: '#1d2024', name: 'White' },
  black: { bg: '#2b2f36', ink: '#f2f3f5', name: 'Black' },
}
export const STICKY_KEYS = Object.keys(STICKY)

/** Pen, line and text colours. */
export const INKS = ['#1d2024', '#5b6472', '#e5484d', '#f76b15', '#e2a500', '#2b8a3e', '#12a594', '#2f80ed', '#8e4ec6', '#d6409f', '#ffffff']

export function paper(c?: string): { bg: string; ink: string } {
  if (!c || c === 'none') return { bg: 'transparent', ink: '#1d2024' }
  const s = STICKY[c]
  if (s) return s
  return { bg: c, ink: inkOn(c) }
}

/** A frame's light tint of a colour. */
export function tint(c?: string): string {
  const s = STICKY[c || 'gray']?.bg || c || '#e3e5e8'
  return `color-mix(in srgb, ${s} 22%, transparent)`
}
export function tintSolid(c?: string): string {
  const s = STICKY[c || 'gray']?.bg || c || '#e3e5e8'
  return mixHex(s, '#ffffff', 0.78)
}

function hexRGB(h: string): [number, number, number] {
  const m = /^#?([0-9a-f]{6})$/i.exec(h)
  if (!m) return [255, 255, 255]
  const n = parseInt(m[1], 16)
  return [n >> 16, (n >> 8) & 255, n & 255]
}
function mixHex(a: string, b: string, t: number): string {
  const x = hexRGB(a), y = hexRGB(b)
  return '#' + x.map((v, i) => Math.round(v + (y[i] - v) * t).toString(16).padStart(2, '0')).join('')
}
export function inkOn(bg: string): string {
  const [r, g, b] = hexRGB(bg)
  return (0.299 * r + 0.587 * g + 0.114 * b) > 150 ? '#1d2024' : '#f5f6f8'
}

// ---- ids, sizes ----

export function newID(): string {
  const a = new Uint8Array(9)
  crypto.getRandomValues(a)
  return Array.from(a, b => 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_'[b & 63]).join('')
}

export const STICKY_SIZE = 200
export const FONT = "Inter, -apple-system, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif"

export interface Rect { x: number; y: number; w: number; h: number }
export interface Pt { x: number; y: number }

export function rectOf(o: Obj, all?: Map<string, Obj>): Rect {
  if (o.kind === 'line') {
    const [a, b] = lineEnds(o, all)
    return { x: Math.min(a.x, b.x), y: Math.min(a.y, b.y), w: Math.abs(a.x - b.x), h: Math.abs(a.y - b.y) }
  }
  return { x: o.x, y: o.y, w: o.w, h: o.h }
}

export function union(rs: Rect[]): Rect | null {
  if (!rs.length) return null
  let x1 = Infinity, y1 = Infinity, x2 = -Infinity, y2 = -Infinity
  for (const r of rs) { x1 = Math.min(x1, r.x); y1 = Math.min(y1, r.y); x2 = Math.max(x2, r.x + r.w); y2 = Math.max(y2, r.y + r.h) }
  return { x: x1, y: y1, w: x2 - x1, h: y2 - y1 }
}

export const overlaps = (a: Rect, b: Rect) => a.x < b.x + b.w && a.x + a.w > b.x && a.y < b.y + b.h && a.y + a.h > b.y
export const inside = (p: Pt, r: Rect, pad = 0) => p.x >= r.x - pad && p.x <= r.x + r.w + pad && p.y >= r.y - pad && p.y <= r.y + r.h + pad
export const center = (r: Rect): Pt => ({ x: r.x + r.w / 2, y: r.y + r.h / 2 })

// ---- shapes ----

export const SHAPES: { form: ShapeForm; name: string; icon: string }[] = [
  { form: 'rect', name: 'Rectangle', icon: 'square' },
  { form: 'round', name: 'Rounded', icon: 'round-rect' },
  { form: 'ellipse', name: 'Circle', icon: 'circle' },
  { form: 'diamond', name: 'Diamond', icon: 'diamond' },
  { form: 'triangle', name: 'Triangle', icon: 'triangle' },
  { form: 'hexagon', name: 'Hexagon', icon: 'hexagon' },
  { form: 'parallelogram', name: 'Slanted', icon: 'parallelogram' },
  { form: 'star', name: 'Star', icon: 'star5' },
]

/** The outline of a shape, as an SVG path in its own box (0,0 – w,h). */
export function shapePath(form: ShapeForm | undefined, w: number, h: number): string {
  switch (form) {
    case 'ellipse': return `M${w / 2},0 A${w / 2},${h / 2} 0 1 1 ${w / 2 - 0.01},0 Z`
    case 'round': { const r = Math.min(24, w / 4, h / 4); return `M${r},0 H${w - r} Q${w},0 ${w},${r} V${h - r} Q${w},${h} ${w - r},${h} H${r} Q0,${h} 0,${h - r} V${r} Q0,0 ${r},0 Z` }
    case 'diamond': return `M${w / 2},0 L${w},${h / 2} L${w / 2},${h} L0,${h / 2} Z`
    case 'triangle': return `M${w / 2},0 L${w},${h} L0,${h} Z`
    case 'hexagon': { const d = Math.min(w / 4, h / 2); return `M${d},0 H${w - d} L${w},${h / 2} L${w - d},${h} H${d} L0,${h / 2} Z` }
    case 'parallelogram': { const d = Math.min(w / 4, h); return `M${d},0 H${w} L${w - d},${h} H0 Z` }
    case 'star': {
      const pts: string[] = []
      for (let i = 0; i < 10; i++) {
        const a = -Math.PI / 2 + i * Math.PI / 5, r = i % 2 ? 0.42 : 1
        pts.push(`${(w / 2 + Math.cos(a) * r * w / 2).toFixed(1)},${(h / 2 + Math.sin(a) * r * h / 2 + h * 0.04).toFixed(1)}`)
      }
      return 'M' + pts.join(' L') + ' Z'
    }
    default: return `M0,0 H${w} V${h} H0 Z`
  }
}

/** The room for text inside a shape. */
export function shapeTextBox(form: ShapeForm | undefined, w: number, h: number): Rect {
  switch (form) {
    case 'ellipse': return { x: w * 0.15, y: h * 0.15, w: w * 0.7, h: h * 0.7 }
    case 'diamond': return { x: w * 0.25, y: h * 0.25, w: w * 0.5, h: h * 0.5 }
    case 'triangle': return { x: w * 0.25, y: h * 0.45, w: w * 0.5, h: h * 0.5 }
    case 'star': return { x: w * 0.3, y: h * 0.35, w: w * 0.4, h: h * 0.4 }
    case 'hexagon': case 'parallelogram': return { x: w * 0.18, y: h * 0.08, w: w * 0.64, h: h * 0.84 }
    default: return { x: 10, y: 8, w: Math.max(1, w - 20), h: Math.max(1, h - 16) }
  }
}

// ---- lines that stay joined ----

/** Where a line from inside a box towards p leaves the box (ellipses round). */
function edgePoint(r: Rect, toward: Pt, round: boolean): Pt {
  const c = center(r)
  const dx = toward.x - c.x, dy = toward.y - c.y
  if (dx === 0 && dy === 0) return c
  if (round) {
    const a = r.w / 2, b = r.h / 2
    const t = 1 / Math.sqrt((dx * dx) / (a * a) + (dy * dy) / (b * b))
    return { x: c.x + dx * t, y: c.y + dy * t }
  }
  const sx = dx !== 0 ? (r.w / 2) / Math.abs(dx) : Infinity
  const sy = dy !== 0 ? (r.h / 2) / Math.abs(dy) : Infinity
  const t = Math.min(sx, sy)
  return { x: c.x + dx * t, y: c.y + dy * t }
}

/** A line's two ends, following the things it is joined to. */
export function lineEnds(o: Obj, all?: Map<string, Obj>): [Pt, Pt] {
  const d = o.data
  let a: Pt = { x: d.x1 ?? o.x, y: d.y1 ?? o.y }
  let b: Pt = { x: d.x2 ?? o.x + o.w, y: d.y2 ?? o.y + o.h }
  const A = d.from ? all?.get(d.from) : undefined
  const B = d.to ? all?.get(d.to) : undefined
  const ra = A && A.kind !== 'line' ? rectOf(A) : null
  const rb = B && B.kind !== 'line' ? rectOf(B) : null
  const elbow = d.curve === 'elbow'
  if (ra && rb && elbow) {
    // elbows leave from the side facing the other thing
    const ca = center(ra), cb = center(rb)
    const horiz = Math.abs(cb.x - ca.x) > Math.abs(cb.y - ca.y)
    a = horiz ? { x: cb.x > ca.x ? ra.x + ra.w : ra.x, y: ca.y } : { x: ca.x, y: cb.y > ca.y ? ra.y + ra.h : ra.y }
    b = horiz ? { x: cb.x > ca.x ? rb.x : rb.x + rb.w, y: cb.y } : { x: cb.x, y: cb.y > ca.y ? rb.y : rb.y + rb.h }
    return [a, b]
  }
  const round = (x?: Obj) => x?.kind === 'shape' && x.data.shape === 'ellipse'
  if (ra) a = edgePoint(ra, rb ? center(rb) : b, round(A))
  if (rb) b = edgePoint(rb, ra ? center(ra) : a, round(B))
  if (ra && !rb) a = edgePoint(ra, b, round(A))
  return [a, b]
}

export interface LineGeom { d: string; mid: Pt; angA: number; angB: number; a: Pt; b: Pt }

/** The path of a line (straight, elbow or curve) and the direction at each end. */
/** Whether an elbow between two joined things leaves sideways (else up or down). */
function elbowSide(o: Obj, all?: Map<string, Obj>): boolean | undefined {
  const A = o.data.from ? all?.get(o.data.from) : undefined
  const B = o.data.to ? all?.get(o.data.to) : undefined
  if (!A || !B || A.kind === 'line' || B.kind === 'line') return undefined
  const ca = center(rectOf(A)), cb = center(rectOf(B))
  return Math.abs(cb.x - ca.x) > Math.abs(cb.y - ca.y)
}

export function linePath(o: Obj, all?: Map<string, Obj>): LineGeom {
  const [a, b] = lineEnds(o, all)
  const kind = o.data.curve || 'straight'
  if (kind === 'elbow') {
    const horiz = elbowSide(o, all) ?? Math.abs(b.x - a.x) >= Math.abs(b.y - a.y)
    if (horiz) {
      const mx = (a.x + b.x) / 2
      return { d: `M${a.x},${a.y} H${mx} V${b.y} H${b.x}`, mid: { x: mx, y: (a.y + b.y) / 2 }, angA: Math.atan2(0, a.x - mx), angB: Math.atan2(0, b.x - mx), a, b }
    }
    const my = (a.y + b.y) / 2
    return { d: `M${a.x},${a.y} V${my} H${b.x} V${b.y}`, mid: { x: (a.x + b.x) / 2, y: my }, angA: Math.atan2(a.y - my, 0), angB: Math.atan2(b.y - my, 0), a, b }
  }
  if (kind === 'curve') {
    const dx = b.x - a.x, dy = b.y - a.y
    const horiz = Math.abs(dx) >= Math.abs(dy)
    const c1 = horiz ? { x: a.x + dx * 0.5, y: a.y } : { x: a.x, y: a.y + dy * 0.5 }
    const c2 = horiz ? { x: b.x - dx * 0.5, y: b.y } : { x: b.x, y: b.y - dy * 0.5 }
    const mid = { x: 0.125 * a.x + 0.375 * c1.x + 0.375 * c2.x + 0.125 * b.x, y: 0.125 * a.y + 0.375 * c1.y + 0.375 * c2.y + 0.125 * b.y }
    return { d: `M${a.x},${a.y} C${c1.x},${c1.y} ${c2.x},${c2.y} ${b.x},${b.y}`, mid, angA: Math.atan2(a.y - c1.y, a.x - c1.x), angB: Math.atan2(b.y - c2.y, b.x - c2.x), a, b }
  }
  const ang = Math.atan2(b.y - a.y, b.x - a.x)
  return { d: `M${a.x},${a.y} L${b.x},${b.y}`, mid: { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 }, angA: ang + Math.PI, angB: ang, a, b }
}

/** An arrow head at p pointing along ang, as a path. */
export function arrowHead(p: Pt, ang: number, size: number): string {
  const s = size, w = 0.5
  const l = { x: p.x - s * Math.cos(ang - w), y: p.y - s * Math.sin(ang - w) }
  const r = { x: p.x - s * Math.cos(ang + w), y: p.y - s * Math.sin(ang + w) }
  return `M${l.x},${l.y} L${p.x},${p.y} L${r.x},${r.y}`
}

/** Distance from p to a line's path (straight segments are enough to pick it). */
export function nearLine(o: Obj, all: Map<string, Obj>, p: Pt, tol: number): boolean {
  const g = linePath(o, all)
  const pts: Pt[] = []
  if (o.data.curve === 'curve') {
    const m = /C([-\d.e]+),([-\d.e]+) ([-\d.e]+),([-\d.e]+)/.exec(g.d)
    const c1 = m ? { x: +m[1], y: +m[2] } : g.a, c2 = m ? { x: +m[3], y: +m[4] } : g.b
    for (let i = 0; i <= 16; i++) {
      const t = i / 16, u = 1 - t
      pts.push({ x: u * u * u * g.a.x + 3 * u * u * t * c1.x + 3 * u * t * t * c2.x + t * t * t * g.b.x, y: u * u * u * g.a.y + 3 * u * u * t * c1.y + 3 * u * t * t * c2.y + t * t * t * g.b.y })
    }
  } else {
    const nums = g.d.match(/[-\d.e]+/g)?.map(Number) || []
    let x = g.a.x, y = g.a.y
    pts.push({ x, y })
    const re = /([MLHV])([-\d.e]+)(?:,([-\d.e]+))?/g
    let m: RegExpExecArray | null
    while ((m = re.exec(g.d))) {
      if (m[1] === 'H') x = +m[2]
      else if (m[1] === 'V') y = +m[2]
      else { x = +m[2]; y = +(m[3] ?? 0) }
      pts.push({ x, y })
    }
    void nums
  }
  for (let i = 1; i < pts.length; i++) if (segDist(p, pts[i - 1], pts[i]) <= tol) return true
  return false
}

function segDist(p: Pt, a: Pt, b: Pt): number {
  const dx = b.x - a.x, dy = b.y - a.y
  const l = dx * dx + dy * dy
  const t = l ? Math.max(0, Math.min(1, ((p.x - a.x) * dx + (p.y - a.y) * dy) / l)) : 0
  return Math.hypot(p.x - (a.x + t * dx), p.y - (a.y + t * dy))
}

// ---- pen ----

/** A smooth path through a drawing's points (relative to its box). */
export function penPath(points: number[][]): string {
  if (!points.length) return ''
  if (points.length < 3) return `M${points[0][0]},${points[0][1]} ` + points.map(p => `L${p[0]},${p[1]}`).join(' ')
  let d = `M${points[0][0]},${points[0][1]}`
  for (let i = 1; i < points.length - 1; i++) {
    const mx = (points[i][0] + points[i + 1][0]) / 2, my = (points[i][1] + points[i + 1][1]) / 2
    d += ` Q${points[i][0]},${points[i][1]} ${mx},${my}`
  }
  const l = points[points.length - 1]
  return d + ` L${l[0]},${l[1]}`
}

// ---- text that fits ----

let measureCtx: CanvasRenderingContext2D | null = null
function ctx2d(): CanvasRenderingContext2D {
  if (!measureCtx) measureCtx = document.createElement('canvas').getContext('2d')!
  return measureCtx
}

/** Text broken into lines that fit a width at a font size. */
export function wrap(text: string, width: number, size: number, bold = false): string[] {
  const c = ctx2d()
  c.font = `${bold ? 600 : 400} ${size}px ${FONT}`
  const out: string[] = []
  for (const para of text.split('\n')) {
    if (para === '') { out.push(''); continue }
    let line = ''
    for (const word of para.split(/(\s+)/)) {
      const tryLine = line + word
      if (c.measureText(tryLine).width <= width || line === '') {
        if (c.measureText(tryLine).width > width && line === '') {
          // a word longer than the line: break it
          let part = ''
          for (const ch of word) {
            if (c.measureText(part + ch).width > width && part) { out.push(part); part = ch } else part += ch
          }
          line = part
        } else line = tryLine
      } else {
        out.push(line.trimEnd())
        line = word.trimStart()
      }
    }
    out.push(line.trimEnd())
  }
  return out
}

const fitCache = new Map<string, number>()

/** The biggest font size (max → min) at which text fits a box, like a sticky note. */
export function fitSize(text: string, w: number, h: number, max = 40, min = 8, bold = false): number {
  const key = `${text}\u0000${Math.round(w)}\u0000${Math.round(h)}\u0000${max}\u0000${bold}`
  const hit = fitCache.get(key)
  if (hit) return hit
  let size = max
  if (text.trim()) {
    for (; size > min; size -= size > 20 ? 2 : 1) {
      const lines = wrap(text, w, size, bold)
      if (lines.length * size * 1.25 <= h) break
    }
  }
  if (fitCache.size > 4000) fitCache.clear()
  fitCache.set(key, size)
  return size
}

/** A sticky note's text box (padding inside the paper). */
export function stickyBox(o: { w: number; h: number }): Rect {
  const p = Math.max(8, o.w * 0.08)
  return { x: p, y: p, w: Math.max(1, o.w - 2 * p), h: Math.max(1, o.h - 2 * p - o.h * 0.04) }
}

// ---- reading order, frames ----

/** The frame a thing sits in (the smallest one around its middle). */
export function frameOf(o: Obj, frames: Obj[]): Obj | undefined {
  const c = center(rectOf(o))
  let best: Obj | undefined
  for (const f of frames) {
    if (f.id === o.id) continue
    if (inside(c, f) && (!best || f.w * f.h < best.w * best.h)) best = f
  }
  return best
}

/** Frames in reading order: rows top to bottom, left to right in a row. */
export function framesInOrder(objs: Iterable<Obj>): Obj[] {
  const fs = [...objs].filter(o => o.kind === 'frame')
  return fs.sort((a, b) => (Math.abs(a.y - b.y) > Math.min(a.h, b.h) / 2 ? a.y - b.y : a.x - b.x))
}

// ---- data defaults ----

export function textOf(o: Obj): string {
  return (o.kind === 'frame' ? o.data.title : o.data.text) || ''
}

export function withData(o: Obj, d: Partial<ObjData>): Obj {
  return { ...o, data: { ...o.data, ...d } }
}

// ---- the miniature on the board list ----

export function previewBounds(p: PreviewShape[]): Rect | null {
  return union(p.map(s => ({ x: s.x, y: s.y, w: Math.max(s.w, 1), h: Math.max(s.h, 1) })))
}
