// One thing on the board, drawn in board units (the board's transform
// scales everything at once). Memoised: only what changed draws again.
import { memo, useEffect, useLayoutEffect, useRef, useState } from 'react'
import type { Obj } from '../../lib/types'
import { OUTLINE_ONLY, actorPath, arrowHead, fitSize, paper, penPath, shapeLines, shapePath, shapeTextBox, stickyBox, tint, type LineGeom } from '../../lib/board'
import { Icon, iconPath } from '../icons'
import { Avatar } from '../ui'
import { hasFormatting, htmlToText, sanitize, textToHTML } from '../../lib/rich'

export interface EditResult { text: string; html?: string; h?: number }
export type EditHow = 'done' | 'tab' | 'back' | 'cancel' | 'enter'

export interface ViewProps {
  o: Obj
  geom?: LineGeom
  editing: boolean
  imgSrc?: string
  remote?: string // the colour of someone else who has it selected
  showAuthor?: boolean
  cell?: string // the table cell being written in, "row,col"
  onEditDone?: (id: string, r: EditResult, how: EditHow) => void
}

/** Where the editor of the note being written is (the format bar uses it). */
export let activeEditor: HTMLElement | null = null

function Editable({ id, text, html, rich, className, style, onDone, onText, multiline = true }: {
  id: string; text: string; html?: string; rich?: boolean; className: string; style?: React.CSSProperties; onDone: ViewProps['onEditDone']; onText?: (t: string) => void; multiline?: boolean
}) {
  const ref = useRef<HTMLDivElement>(null)
  const done = useRef(false)
  const born = useRef(performance.now())
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    if (rich) el.innerHTML = html ? sanitize(html) : textToHTML(text)
    else el.innerText = text
    activeEditor = el
    el.focus({ preventScroll: true })
    const r = document.createRange()
    r.selectNodeContents(el)
    if (text) r.collapse(false)
    const s = window.getSelection()
    s?.removeAllRanges()
    s?.addRange(r)
    return () => { if (activeEditor === el) activeEditor = null }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps
  const finish = (how: EditHow) => {
    if (done.current) return
    done.current = true
    const el = ref.current
    const box = el?.closest('.ob') as HTMLElement | null
    const h = box ? box.scrollHeight : undefined
    if (how === 'cancel' || !el) { onDone?.(id, { text, html, h }, how); return }
    if (rich) {
      const clean = sanitize(el.innerHTML)
      const fmt = hasFormatting(clean)
      onDone?.(id, { text: (fmt ? htmlToText(clean) : el.innerText).replace(/ /g, ' ').replace(/\n$/, ''), html: fmt ? clean : undefined, h }, how)
    } else onDone?.(id, { text: (el.innerText || '').replace(/ /g, ' ').replace(/\n$/, ''), h }, how)
  }
  return (
    <div ref={ref} className={`ob-edit ${className}`} style={style} contentEditable={(rich ? 'true' : 'plaintext-only') as unknown as boolean} suppressContentEditableWarning spellCheck
      onBlur={e => {
        // a tap's late mouse events can take focus right after the note opens: take it back
        if (performance.now() - born.current < 400) { setTimeout(() => ref.current?.focus({ preventScroll: true }), 0); return }
        // the format bar keeps the note open
        if ((e.relatedTarget as HTMLElement | null)?.closest?.('.wb-format')) return
        finish('done')
      }}
      onInput={e => onText?.((e.target as HTMLElement).innerText)}
      onPaste={e => { if (!rich) return; e.preventDefault(); document.execCommand('insertText', false, e.clipboardData.getData('text/plain')) }}
      onPointerDown={e => e.stopPropagation()}
      onKeyDown={e => {
        e.stopPropagation()
        if (e.key === 'Escape') { e.preventDefault(); finish('done') }
        else if (e.key === 'Tab') { e.preventDefault(); finish(e.shiftKey ? 'back' : 'tab') }
        else if (e.key === 'Enter' && (e.metaKey || e.ctrlKey || !multiline) && !e.shiftKey) { e.preventDefault(); finish(multiline ? 'done' : 'enter') }
      }} />
  )
}

export function VoteBadge({ votes, mine }: { votes?: number; mine?: number }) {
  if (!votes && !mine) return null
  return (
    <span className="ob-votes" aria-label={`${votes || 0} votes`}>
      {!!mine && <span className="ob-mine">{Array.from({ length: Math.min(mine, 6) }, (_, i) => <i key={i} />)}</span>}
      {!!votes && <b>{votes}</b>}
    </span>
  )
}

const TAG_COLOURS = ['#4262ff', '#e5484d', '#12a594', '#f76b15', '#8e4ec6', '#d6409f', '#2b8a3e', '#b5850b']
export function tagColour(t: string): string {
  let h = 0
  for (const c of t.toLowerCase()) h = (h * 31 + c.charCodeAt(0)) >>> 0
  return TAG_COLOURS[h % TAG_COLOURS.length]
}

function Tags({ tags }: { tags?: string[] }) {
  if (!tags?.length) return null
  return <div className="ob-tags">{tags.slice(0, 4).map(t => <span key={t} style={{ background: tagColour(t) }}>{t}</span>)}{tags.length > 4 && <span className="more">+{tags.length - 4}</span>}</div>
}

/** The box style every thing shares: where it is, and its turn. */
function place(o: Obj, extra: React.CSSProperties = {}): React.CSSProperties {
  return { left: o.x, top: o.y, width: o.w, height: o.h, ...(o.data.rot ? { transform: `rotate(${o.data.rot}deg)` } : {}), ...extra }
}

function Body({ d, className, style }: { d: Obj['data']; className: string; style: React.CSSProperties }) {
  if (d.html) return <div className={`${className} rich`} style={style} dangerouslySetInnerHTML={{ __html: sanitize(d.html) }} />
  return <div className={className} style={style}>{d.text}</div>
}

function StickyView({ o, editing, onEditDone, showAuthor }: ViewProps) {
  const d = o.data
  const p = paper(d.color || 'light-yellow')
  const box = stickyBox(o)
  const tagRoom = d.tags?.length ? Math.min(28, o.h * 0.14) : 0
  const [draft, setDraft] = useState(d.text || '')
  useEffect(() => { if (!editing) setDraft(d.text || '') }, [editing, d.text])
  const size = d.size || fitSize(editing ? draft || ' ' : d.text || '', box.w, box.h - tagRoom, Math.round(o.w * 0.2), 8, !!d.html && /<b>/.test(d.html))
  const style: React.CSSProperties = { fontSize: size, padding: `${box.y}px ${box.x}px ${box.y + tagRoom}px`, textAlign: d.align || 'center' }
  return (
    <div className={`ob ob-sticky ${o.hidden ? 'is-hidden' : ''}`} data-id={o.id} style={place(o, { background: p.bg, color: p.ink })}>
      {editing
        ? <Editable id={o.id} text={d.text || ''} html={d.html} rich className="ob-st-text" style={style} onDone={onEditDone} onText={setDraft} />
        : <Body d={d} className="ob-st-text" style={style} />}
      <Tags tags={d.tags} />
      {showAuthor && o.author && !editing && !d.tags?.length && <span className="ob-author">{o.author}</span>}
      {o.hidden && <span className="ob-private" title="Private: only you see this until someone reveals the notes">only you</span>}
    </div>
  )
}

function TextView({ o, editing, onEditDone }: ViewProps) {
  const d = o.data
  const style: React.CSSProperties = { fontSize: d.size || 24, fontWeight: d.bold ? 650 : 400, fontStyle: d.italic ? 'italic' : undefined, color: d.color || undefined, textAlign: d.align || 'left' }
  return (
    <div className={`ob ob-text ${d.text ? '' : 'is-empty'} ${o.hidden ? 'is-hidden' : ''}`} data-id={o.id} style={place(o, { height: undefined, minHeight: o.h })}>
      {editing
        ? <Editable id={o.id} text={d.text || ''} html={d.html} rich className="ob-tx" style={style} onDone={onEditDone} />
        : d.text ? <Body d={d} className="ob-tx" style={style} /> : <div className="ob-tx" style={style}>Text</div>}
    </div>
  )
}

function ShapeView({ o, editing, onEditDone }: ViewProps) {
  const d = o.data
  const p = d.fill === 'none' ? { bg: 'transparent', ink: 'var(--wb-ink)' } : paper(d.fill || 'white')
  const tb = shapeTextBox(d.shape, o.w, o.h)
  const [draft, setDraft] = useState(d.text || '')
  useEffect(() => { if (!editing) setDraft(d.text || '') }, [editing, d.text])
  const size = d.size && d.size !== 22 ? d.size : fitSize(editing ? draft || ' ' : d.text || '', tb.w, tb.h, d.size || 28, 8, d.bold)
  const tstyle: React.CSSProperties = { left: tb.x, top: tb.y, width: tb.w, height: tb.h, fontSize: size, color: p.ink, fontWeight: d.bold ? 650 : 450, textAlign: d.align || 'center' }
  return (
    <div className={`ob ob-shape ${o.hidden ? 'is-hidden' : ''}`} data-id={o.id} style={place(o)}>
      <svg width={o.w} height={o.h} viewBox={`0 0 ${o.w} ${o.h}`} className="ob-shape-svg" aria-hidden>
        {d.shape && OUTLINE_ONLY.has(d.shape)
          ? <path d={actorPath(o.w, o.h)} fill="none" stroke={d.stroke || 'var(--wb-stroke)'} strokeWidth={d.width || 2.5} strokeLinecap="round" strokeLinejoin="round" />
          : <>
            <path d={shapePath(d.shape, o.w, o.h)} fill={p.bg} stroke={d.stroke || 'var(--wb-stroke)'} strokeWidth={d.width || 2} strokeDasharray={d.dash ? `${(d.width || 2) * 4} ${(d.width || 2) * 3}` : undefined} strokeLinejoin="round" />
            {shapeLines(d.shape, o.w, o.h) && <path d={shapeLines(d.shape, o.w, o.h)} fill="none" stroke={d.stroke || 'var(--wb-stroke)'} strokeWidth={d.width || 2} strokeLinejoin="round" />}
          </>}
      </svg>
      {editing
        ? <Editable id={o.id} text={d.text || ''} html={d.html} rich className="ob-sh-text" style={tstyle} onDone={onEditDone} onText={setDraft} multiline={!d.mind} />
        : <Body d={d} className="ob-sh-text" style={tstyle} />}
      <Tags tags={d.tags} />
    </div>
  )
}

function CardView({ o, editing, onEditDone }: ViewProps) {
  const d = o.data
  const today = new Date().toISOString().slice(0, 10)
  const late = !!d.due && !d.done && d.due < today
  const stripe = paper(d.color || 'blue').bg
  return (
    <div className={`ob ob-card ${d.done ? 'done' : ''} ${o.hidden ? 'is-hidden' : ''}`} data-id={o.id} style={place(o, { ['--stripe' as string]: stripe })}>
      {editing
        ? <Editable id={o.id} text={d.text || ''} className="ob-card-title" onDone={onEditDone} multiline />
        : <div className="ob-card-title">{d.text || 'Card'}</div>}
      <div className="ob-card-foot">
        {d.due && <span className={`ob-card-due ${late ? 'late' : ''}`}><Icon name="calendar" size={13} />{new Date(d.due + 'T12:00').toLocaleDateString(undefined, { day: 'numeric', month: 'short' })}</span>}
        {d.done && <span className="ob-card-done"><Icon name="check" size={13} />Done</span>}
        <span className="spacer" />
        {d.estimate && <span className="ob-card-points" title="Estimate">{d.estimate}</span>}
        {d.task && <span className="ob-card-task" title="In Pocket Tasks"><Icon name="check-square" size={14} /></span>}
        {d.issue && <span className="ob-card-task" title={`${d.issue.identifier} in Pocket Projects`}><Icon name="card" size={14} /></span>}
        {d.event && <span className="ob-card-task" title="On the calendar"><Icon name="calendar" size={14} /></span>}
        {d.assignee && <span title={d.assignee.name}><Avatar name={d.assignee.name} size={24} /></span>}
      </div>
      <Tags tags={d.tags} />
    </div>
  )
}

function EmbedView({ o, editing, imgSrc }: ViewProps) {
  const d = o.data
  const kind = d.embed || 'link'
  if (kind === 'youtube') {
    return (
      <div className={`ob ob-embed yt ${editing ? 'live' : ''}`} data-id={o.id} style={place(o)}>
        {editing && d.video
          ? <iframe src={`https://www.youtube-nocookie.com/embed/${d.video}?autoplay=1&rel=0`} title={d.title || 'YouTube'} allow="autoplay; encrypted-media; picture-in-picture; fullscreen" allowFullScreen />
          : <>
            {d.image && <img src={d.image} alt="" draggable={false} referrerPolicy="no-referrer" />}
            <span className="ob-embed-play"><Icon name="play" size={28} /></span>
            <span className="ob-embed-cap">{d.title || 'YouTube'}<small>Double-click to play</small></span>
          </>}
      </div>
    )
  }
  if (kind === 'pdf' && editing && imgSrc) {
    return <div className="ob ob-embed doc live" data-id={o.id} style={place(o)}><iframe src={imgSrc + '#view=FitH'} title={d.name || 'PDF'} /></div>
  }
  if (kind === 'pdf' || kind === 'file') {
    const kb = d.bytes ? (d.bytes > 1048576 ? `${(d.bytes / 1048576).toFixed(1)} MB` : `${Math.max(1, Math.round(d.bytes / 1024))} KB`) : ''
    return (
      <div className="ob ob-embed doc" data-id={o.id} style={place(o)}>
        <span className="ob-embed-doc"><Icon name={kind === 'pdf' ? 'file-pdf' : 'file'} size={40} /></span>
        <span className="ob-embed-text"><b>{d.name || 'File'}</b><small>{[kb, kind === 'pdf' ? 'Double-click to read' : 'Double-click to open'].filter(Boolean).join(' · ')}</small></span>
      </div>
    )
  }
  return (
    <div className="ob ob-embed link" data-id={o.id} style={place(o)}>
      {d.image && <img className="ob-embed-img" src={d.image} alt="" draggable={false} referrerPolicy="no-referrer" onError={e => { (e.target as HTMLElement).style.display = 'none' }} />}
      <span className="ob-embed-text">
        <b>{d.title || d.url}</b>
        {d.description && <span className="ob-embed-desc">{d.description}</span>}
        <small>{d.favicon && <img src={d.favicon} alt="" width={14} height={14} referrerPolicy="no-referrer" onError={e => { (e.target as HTMLElement).style.display = 'none' }} />}{d.site || (d.url ? new URL(d.url).hostname : '')}</small>
      </span>
    </div>
  )
}

function IconView({ o }: ViewProps) {
  return (
    <svg className={`ob ob-iconobj ${o.hidden ? 'is-hidden' : ''}`} style={place(o)} width={o.w} height={o.h} viewBox="0 0 24 24" aria-hidden>
      <rect data-id={o.id} x="0" y="0" width="24" height="24" fill="transparent" />
      <path d={iconPath(o.data.icon || 'star')} fill="none" stroke={o.data.color || 'var(--wb-ink)'} strokeWidth={1.75} strokeLinecap="round" strokeLinejoin="round" pointerEvents="none" />
    </svg>
  )
}

function StickerView({ o }: ViewProps) {
  return (
    <div className={`ob ob-sticker ${o.hidden ? 'is-hidden' : ''}`} data-id={o.id} style={place(o, { fontSize: Math.min(o.w, o.h) * 0.86 })}>
      <span>{o.data.emoji || '⭐'}</span>
    </div>
  )
}

function FrameView({ o, editing, onEditDone }: ViewProps) {
  const d = o.data
  return (
    <div className="ob ob-frame" style={{ left: o.x, top: o.y, width: o.w, height: o.h, background: tint(d.fill) }}>
      <div className="ob-frame-title" data-id={o.id} data-part="title">
        {editing
          ? <Editable id={o.id} text={d.title || ''} className="ob-fr-edit" onDone={onEditDone} multiline={false} />
          : <span>{d.title || 'Frame'}</span>}
      </div>
      <div className="ob-frame-edge" data-id={o.id} data-part="edge" />
    </div>
  )
}

function LineView({ o, geom, editing, onEditDone }: ViewProps) {
  if (!geom) return null
  const d = o.data
  const w = d.width || 2.5
  const colour = d.color || 'var(--wb-stroke)'
  const head = 10 + w * 2
  return (
    <>
      <svg className="ob ob-line" width="1" height="1" aria-hidden>
        <path d={geom.d} className="ob-line-hit" data-id={o.id} />
        <path d={geom.d} fill="none" stroke={colour} strokeWidth={w} strokeDasharray={d.dash ? `${w * 4} ${w * 3}` : undefined} strokeLinecap="round" strokeLinejoin="round" />
        {(d.end ?? 'arrow') === 'arrow' && <path d={arrowHead(geom.b, geom.angB, head)} fill="none" stroke={colour} strokeWidth={w} strokeLinecap="round" strokeLinejoin="round" />}
        {d.start === 'arrow' && <path d={arrowHead(geom.a, geom.angA, head)} fill="none" stroke={colour} strokeWidth={w} strokeLinecap="round" strokeLinejoin="round" />}
        {d.end === 'dot' && <circle cx={geom.b.x} cy={geom.b.y} r={w * 2} fill={colour} />}
        {d.start === 'dot' && <circle cx={geom.a.x} cy={geom.a.y} r={w * 2} fill={colour} />}
      </svg>
      {(d.text || editing) && (
        <div className="ob ob-line-label" data-id={o.id} style={{ left: geom.mid.x, top: geom.mid.y }}>
          {editing ? <Editable id={o.id} text={d.text || ''} className="ob-ll-edit" onDone={onEditDone} multiline={false} /> : d.text}
        </div>
      )}
    </>
  )
}

function PenView({ o }: ViewProps) {
  const d = o.data
  return (
    <svg className="ob ob-pen" style={{ left: o.x, top: o.y }} width={Math.max(1, o.w)} height={Math.max(1, o.h)} aria-hidden>
      <path d={penPath(d.points || [])} data-id={o.id} fill="none" stroke={d.color || 'var(--wb-ink)'} strokeWidth={d.width || 4} strokeLinecap="round" strokeLinejoin="round" opacity={d.highlight ? 0.35 : 1} className="ob-pen-path" />
    </svg>
  )
}

/** A cropped picture: the whole image, moved and scaled so only the kept part shows. */
export function cropStyle(o: Obj): React.CSSProperties | undefined {
  const c = o.data.crop
  if (!c || (c.w >= 1 && c.h >= 1)) return undefined
  const w = o.w / c.w, h = o.h / c.h
  return { position: 'absolute', maxWidth: 'none', width: w, height: h, left: -c.x * w, top: -c.y * h, objectFit: 'fill' }
}

export function tableCols(o: Obj): number[] {
  const cells = o.data.cells || [['']]
  const n = Math.max(1, ...cells.map(r => r.length))
  const w = o.data.cols && o.data.cols.length === n ? o.data.cols : Array(n).fill(1)
  const sum = w.reduce((a, b) => a + b, 0) || 1
  return w.map(x => x / sum)
}

function TableView({ o, editing, onEditDone, cell }: ViewProps) {
  const d = o.data
  const cells = d.cells || [['']]
  const cols = tableCols(o)
  const fill = d.fill && d.fill !== 'none' ? paper(d.fill).bg : undefined
  return (
    <div className={`ob ob-table ${o.hidden ? 'is-hidden' : ''}`} data-id={o.id} style={place(o, { gridTemplateColumns: cols.map(f => `${f}fr`).join(' '), gridTemplateRows: `repeat(${cells.length}, 1fr)`, fontSize: d.size || 16 })}>
      {cells.map((row, r) => cols.map((_, c) => {
        const head = d.header && r === 0
        const on = editing && (cell || '0,0') === `${r},${c}`
        return (
          <div key={`${r}-${c}`} className={`ob-cell ${head ? 'head' : ''}`} data-cell={`${r},${c}`} style={head && fill ? { background: fill } : undefined}>
            {on ? <Editable id={o.id} text={row[c] || ''} className="ob-cell-edit" onDone={onEditDone} multiline={false} /> : row[c]}
          </div>
        )
      }))}
    </div>
  )
}

function ImageView({ o, imgSrc }: ViewProps) {
  return (
    <div className={`ob ob-image ${o.hidden ? 'is-hidden' : ''}`} data-id={o.id} style={place(o)}>
      {imgSrc ? <img src={imgSrc} alt={o.data.name || ''} draggable={false} style={cropStyle(o)} /> : <span className="ob-image-wait" />}
    </div>
  )
}

function View(p: ViewProps) {
  const el = (() => {
    switch (p.o.kind) {
      case 'sticky': return <StickyView {...p} />
      case 'text': return <TextView {...p} />
      case 'shape': return <ShapeView {...p} />
      case 'frame': return <FrameView {...p} />
      case 'line': return <LineView {...p} />
      case 'pen': return <PenView {...p} />
      case 'image': return <ImageView {...p} />
      case 'sticker': return <StickerView {...p} />
      case 'card': return <CardView {...p} />
      case 'embed': return <EmbedView {...p} />
      case 'icon': return <IconView {...p} />
      case 'table': return <TableView {...p} />
    }
    return null
  })()
  if (p.remote && p.o.kind !== 'line') {
    return <>{el}<div className="ob-remote" style={{ left: p.o.x - 3, top: p.o.y - 3, width: p.o.w + 6, height: p.o.h + 6, borderColor: p.remote, ...(p.o.data.rot ? { transform: `rotate(${p.o.data.rot}deg)` } : {}) }} /></>
  }
  return el
}

export const ObjectView = memo(View, (a, b) =>
  a.o === b.o && a.editing === b.editing && a.cell === b.cell && a.imgSrc === b.imgSrc && a.remote === b.remote && a.showAuthor === b.showAuthor &&
  (a.o.kind !== 'line' || a.geom?.d === b.geom?.d) && a.onEditDone === b.onEditDone)
