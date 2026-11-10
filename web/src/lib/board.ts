// What a board is made of, drawn the same on screen and in a picture:
// colours, the geometry of shapes and lines, and how much text fits.
import type { Obj, ObjData, ShapeForm, PreviewShape } from './types'

// ---- colours ----

/** Sticky-note colours: the sixteen of a Miro board, in its order (also fills for shapes and frames). */
const INK = '#1a1a1a'
export const STICKY: Record<string, { bg: string; ink: string; name: string }> = {
  'light-yellow': { bg: '#fff9b1', ink: INK, name: 'Light yellow' },
  yellow: { bg: '#f5d128', ink: INK, name: 'Yellow' },
  orange: { bg: '#ff9d48', ink: INK, name: 'Orange' },
  'light-green': { bg: '#d5f692', ink: INK, name: 'Light green' },
  green: { bg: '#c9df56', ink: INK, name: 'Green' },
  'dark-green': { bg: '#93d275', ink: INK, name: 'Dark green' },
  cyan: { bg: '#67c6c0', ink: INK, name: 'Cyan' },
  'light-pink': { bg: '#ffcee0', ink: INK, name: 'Light pink' },
  pink: { bg: '#ea94bb', ink: INK, name: 'Pink' },
  violet: { bg: '#be88c7', ink: INK, name: 'Violet' },
  red: { bg: '#f16c7f', ink: INK, name: 'Red' },
  'light-blue': { bg: '#a6ccf5', ink: INK, name: 'Light blue' },
  blue: { bg: '#6cd8fa', ink: INK, name: 'Blue' },
  'dark-blue': { bg: '#9ea9ff', ink: INK, name: 'Dark blue' },
  gray: { bg: '#e6e6e6', ink: INK, name: 'Grey' },
  black: { bg: '#000000', ink: '#ffffff', name: 'Black' },
  // older names, still drawn (and white for shapes)
  white: { bg: '#ffffff', ink: INK, name: 'White' },
  lime: { bg: '#d5f692', ink: INK, name: 'Light green' },
}
export const STICKY_KEYS = Object.keys(STICKY).filter(k => k !== 'white' && k !== 'lime')

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
export const FONT = "'Open Sans', -apple-system, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif"

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

export const SHAPES: { form: ShapeForm; name: string; icon: string; group: 'Basic' | 'Flowchart' | 'UML' }[] = [
  { form: 'rect', name: 'Rectangle', icon: 'square', group: 'Basic' },
  { form: 'round', name: 'Rounded', icon: 'round-rect', group: 'Basic' },
  { form: 'ellipse', name: 'Circle', icon: 'circle', group: 'Basic' },
  { form: 'diamond', name: 'Diamond', icon: 'diamond', group: 'Basic' },
  { form: 'triangle', name: 'Triangle', icon: 'triangle', group: 'Basic' },
  { form: 'hexagon', name: 'Hexagon', icon: 'hexagon', group: 'Basic' },
  { form: 'pentagon', name: 'Pentagon', icon: 'pentagon', group: 'Basic' },
  { form: 'octagon', name: 'Octagon', icon: 'octagon', group: 'Basic' },
  { form: 'parallelogram', name: 'Slanted', icon: 'parallelogram', group: 'Basic' },
  { form: 'star', name: 'Star', icon: 'star5', group: 'Basic' },
  { form: 'cross', name: 'Cross', icon: 'cross-shape', group: 'Basic' },
  { form: 'cloud', name: 'Cloud', icon: 'cloud', group: 'Basic' },
  { form: 'callout', name: 'Speech bubble', icon: 'callout', group: 'Basic' },
  { form: 'arrow-right', name: 'Block arrow', icon: 'block-arrow', group: 'Basic' },
  { form: 'chevron', name: 'Chevron', icon: 'chevron-shape', group: 'Basic' },
  { form: 'rect', name: 'Process', icon: 'square', group: 'Flowchart' },
  { form: 'diamond', name: 'Decision', icon: 'diamond', group: 'Flowchart' },
  { form: 'stadium', name: 'Start / end', icon: 'stadium', group: 'Flowchart' },
  { form: 'parallelogram', name: 'Input / output', icon: 'parallelogram', group: 'Flowchart' },
  { form: 'document', name: 'Document', icon: 'doc-shape', group: 'Flowchart' },
  { form: 'cylinder', name: 'Database', icon: 'database', group: 'Flowchart' },
  { form: 'predefined', name: 'Sub-process', icon: 'predefined', group: 'Flowchart' },
  { form: 'manual', name: 'Manual input', icon: 'manual', group: 'Flowchart' },
  { form: 'delay', name: 'Delay', icon: 'delay', group: 'Flowchart' },
  { form: 'display', name: 'Display', icon: 'display-shape', group: 'Flowchart' },
  { form: 'merge', name: 'Merge', icon: 'merge-shape', group: 'Flowchart' },
  { form: 'offpage', name: 'Off-page link', icon: 'offpage', group: 'Flowchart' },
  { form: 'class', name: 'Class', icon: 'class-shape', group: 'UML' },
  { form: 'actor', name: 'Actor', icon: 'user', group: 'UML' },
  { form: 'note', name: 'Note', icon: 'note-shape', group: 'UML' },
  { form: 'component', name: 'Component', icon: 'component', group: 'UML' },
  { form: 'package', name: 'Package', icon: 'package-shape', group: 'UML' },
]

/** The outline of a shape, as an SVG path in its own box (0,0 – w,h). */
export function shapePath(form: ShapeForm | undefined, w: number, h: number): string {
  const poly = (pts: number[][]) => 'M' + pts.map(p => `${(p[0]).toFixed(1)},${(p[1]).toFixed(1)}`).join(' L') + ' Z'
  switch (form) {
    case 'ellipse': return `M${w / 2},0 A${w / 2},${h / 2} 0 1 1 ${w / 2 - 0.01},0 Z`
    case 'round': { const r = Math.min(24, w / 4, h / 4); return `M${r},0 H${w - r} Q${w},0 ${w},${r} V${h - r} Q${w},${h} ${w - r},${h} H${r} Q0,${h} 0,${h - r} V${r} Q0,0 ${r},0 Z` }
    case 'stadium': { const r = Math.min(h / 2, w / 2); return `M${r},0 H${w - r} A${r},${h / 2} 0 0 1 ${w - r},${h} H${r} A${r},${h / 2} 0 0 1 ${r},0 Z` }
    case 'diamond': return poly([[w / 2, 0], [w, h / 2], [w / 2, h], [0, h / 2]])
    case 'triangle': return poly([[w / 2, 0], [w, h], [0, h]])
    case 'merge': return poly([[0, 0], [w, 0], [w / 2, h]])
    case 'hexagon': { const d = Math.min(w / 4, h / 2); return poly([[d, 0], [w - d, 0], [w, h / 2], [w - d, h], [d, h], [0, h / 2]]) }
    case 'pentagon': return poly([[w / 2, 0], [w, h * 0.38], [w * 0.81, h], [w * 0.19, h], [0, h * 0.38]])
    case 'octagon': { const a = Math.min(w, h) * 0.29; return poly([[a, 0], [w - a, 0], [w, a], [w, h - a], [w - a, h], [a, h], [0, h - a], [0, a]]) }
    case 'cross': { const a = w / 3, b = h / 3; return poly([[a, 0], [w - a, 0], [w - a, b], [w, b], [w, h - b], [w - a, h - b], [w - a, h], [a, h], [a, h - b], [0, h - b], [0, b], [a, b]]) }
    case 'parallelogram': { const d = Math.min(w / 4, h); return poly([[d, 0], [w, 0], [w - d, h], [0, h]]) }
    case 'manual': return poly([[0, h * 0.25], [w, 0], [w, h], [0, h]])
    case 'arrow-right': return poly([[0, h * 0.28], [w * 0.62, h * 0.28], [w * 0.62, 0], [w, h / 2], [w * 0.62, h], [w * 0.62, h * 0.72], [0, h * 0.72]])
    case 'chevron': { const d = Math.min(w / 3, h / 2); return poly([[0, 0], [w - d, 0], [w, h / 2], [w - d, h], [0, h], [d, h / 2]]) }
    case 'offpage': return poly([[0, 0], [w, 0], [w, h * 0.7], [w / 2, h], [0, h * 0.7]])
    case 'delay': { const r = Math.min(w / 2, h / 2); return `M0,0 H${w - r} A${r},${h / 2} 0 0 1 ${w - r},${h} H0 Z` }
    case 'display': { const d = Math.min(w / 5, h / 2); return `M${d},0 H${w - d} A${d},${h / 2} 0 0 1 ${w - d},${h} H${d} L0,${h / 2} Z` }
    case 'document': { const q = h * 0.12; return `M0,0 H${w} V${h - q} C${w * 0.75},${h - 3 * q} ${w * 0.25},${h + q} 0,${h - q} Z` }
    case 'cylinder': { const e = Math.min(h * 0.14, w * 0.2); return `M0,${e} A${w / 2},${e} 0 0 1 ${w},${e} V${h - e} A${w / 2},${e} 0 0 1 0,${h - e} Z` }
    case 'predefined': return `M0,0 H${w} V${h} H0 Z`
    case 'class': return `M0,0 H${w} V${h} H0 Z`
    case 'component': return `M${w * 0.1},0 H${w} V${h} H${w * 0.1} Z`
    case 'package': { const tw = Math.min(w * 0.4, 120), th = Math.min(h * 0.18, 24); return `M0,0 H${tw} V${th} H${w} V${h} H0 Z` }
    case 'note': { const f = Math.min(w, h) * 0.18; return poly([[0, 0], [w - f, 0], [w, f], [w, h], [0, h]]) }
    case 'callout': { const t = h * 0.22, r = Math.min(16, w / 6); return `M${r},0 H${w - r} Q${w},0 ${w},${r} V${h - t - r} Q${w},${h - t} ${w - r},${h - t} H${w * 0.38} L${w * 0.2},${h} L${w * 0.24},${h - t} H${r} Q0,${h - t} 0,${h - t - r} V${r} Q0,0 ${r},0 Z` }
    case 'cloud': return `M${w * 0.25},${h * 0.85} C${w * 0.05},${h * 0.85} ${w * 0.02},${h * 0.55} ${w * 0.18},${h * 0.48} C${w * 0.12},${h * 0.2} ${w * 0.42},${h * 0.08} ${w * 0.52},${h * 0.28} C${w * 0.62},${h * 0.05} ${w * 0.92},${h * 0.15} ${w * 0.84},${h * 0.42} C${w * 1.02},${h * 0.48} ${w * 0.98},${h * 0.85} ${w * 0.76},${h * 0.85} Z`
    case 'actor': return `M0,0 H${w} V${h} H0 Z`
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

/** Lines drawn over a shape (not filled): compartments, the inside of a cylinder, a stick figure. */
export function shapeLines(form: ShapeForm | undefined, w: number, h: number): string {
  switch (form) {
    case 'cylinder': { const e = Math.min(h * 0.14, w * 0.2); return `M0,${e} A${w / 2},${e} 0 0 0 ${w},${e}` }
    case 'predefined': { const d = Math.min(w * 0.1, 16); return `M${d},0 V${h} M${w - d},0 V${h}` }
    case 'class': return `M0,${h * 0.3} H${w} M0,${h * 0.65} H${w}`
    case 'component': { const bw = w * 0.2, bh = Math.min(h * 0.14, 14); return `M0,${h * 0.25} H${bw} V${h * 0.25 + bh} H0 Z M0,${h * 0.6} H${bw} V${h * 0.6 + bh} H0 Z` }
    case 'note': { const f = Math.min(w, h) * 0.18; return `M${w - f},0 V${f} H${w}` }
    default: return ''
  }
}

/** Shapes drawn only as lines (no fill box). */
export const OUTLINE_ONLY = new Set<ShapeForm>(['actor'])

/** A stick figure in its box. */
export function actorPath(w: number, h: number): string {
  const r = Math.min(w * 0.18, h * 0.12), cx = w / 2
  const headY = r + 2, bodyTop = headY + r, bodyBottom = h * 0.62, armsY = h * 0.38
  return `M${cx + r},${headY} A${r},${r} 0 1 1 ${cx - r},${headY} A${r},${r} 0 1 1 ${cx + r},${headY} M${cx},${bodyTop} V${bodyBottom} M${w * 0.18},${armsY} H${w * 0.82} M${cx},${bodyBottom} L${w * 0.22},${h * 0.92} M${cx},${bodyBottom} L${w * 0.78},${h * 0.92}`
}

/** The room for text inside a shape. */
export function shapeTextBox(form: ShapeForm | undefined, w: number, h: number): Rect {
  switch (form) {
    case 'actor': return { x: -w * 0.5, y: h + 4, w: w * 2, h: 40 }
    case 'class': return { x: 8, y: 4, w: Math.max(1, w - 16), h: h * 0.3 - 8 }
    case 'cylinder': { const e = Math.min(h * 0.14, w * 0.2); return { x: 10, y: e * 2, w: Math.max(1, w - 20), h: Math.max(1, h - e * 3) } }
    case 'document': return { x: 10, y: 8, w: Math.max(1, w - 20), h: Math.max(1, h * 0.75) }
    case 'callout': return { x: 10, y: 6, w: Math.max(1, w - 20), h: Math.max(1, h * 0.72) }
    case 'cloud': return { x: w * 0.2, y: h * 0.3, w: w * 0.6, h: h * 0.45 }
    case 'pentagon': case 'octagon': case 'cross': return { x: w * 0.2, y: h * 0.25, w: w * 0.6, h: h * 0.5 }
    case 'arrow-right': return { x: w * 0.05, y: h * 0.3, w: w * 0.6, h: h * 0.4 }
    case 'merge': case 'offpage': return { x: w * 0.2, y: h * 0.08, w: w * 0.6, h: h * 0.5 }
    case 'package': { const th = Math.min(h * 0.18, 24); return { x: 10, y: th + 6, w: Math.max(1, w - 20), h: Math.max(1, h - th - 12) } }
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
/** Forget the fitted sizes (the board font finished loading). */
export function clearFit() { fitCache.clear() }

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
  const p = Math.max(8, o.w * 0.1)
  return { x: p, y: p, w: Math.max(1, o.w - 2 * p), h: Math.max(1, o.h - 2 * p) }
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
