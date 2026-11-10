// One thing on the board, drawn in board units (the board's transform
// scales everything at once). Memoised: only what changed draws again.
import { memo, useEffect, useLayoutEffect, useRef, useState } from 'react'
import type { Obj } from '../../lib/types'
import { arrowHead, fitSize, paper, penPath, shapePath, shapeTextBox, stickyBox, tint, type LineGeom } from '../../lib/board'

export interface ViewProps {
  o: Obj
  geom?: LineGeom
  editing: boolean
  imgSrc?: string
  remote?: string // the colour of someone else who has it selected
  showAuthor?: boolean
  onEditDone?: (id: string, text: string, how: 'done' | 'tab' | 'cancel', h?: number) => void
}

function Editable({ id, text, className, style, onDone, onText, multiline = true }: { id: string; text: string; className: string; style?: React.CSSProperties; onDone: ViewProps['onEditDone']; onText?: (t: string) => void; multiline?: boolean }) {
  const ref = useRef<HTMLDivElement>(null)
  const done = useRef(false)
  const born = useRef(performance.now())
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    el.innerText = text
    el.focus({ preventScroll: true })
    const r = document.createRange()
    r.selectNodeContents(el)
    if (text) r.collapse(false)
    const s = window.getSelection()
    s?.removeAllRanges()
    s?.addRange(r)
  }, []) // eslint-disable-line react-hooks/exhaustive-deps
  const finish = (how: 'done' | 'tab' | 'cancel') => {
    if (done.current) return
    done.current = true
    const el = ref.current
    const t = (el?.innerText || '').replace(/ /g, ' ').replace(/\n$/, '')
    const box = el?.closest('.ob') as HTMLElement | null
    onDone?.(id, how === 'cancel' ? text : t, how, box ? box.scrollHeight : undefined)
  }
  return (
    <div ref={ref} className={`ob-edit ${className}`} style={style} contentEditable={'plaintext-only' as unknown as boolean} suppressContentEditableWarning spellCheck
      onBlur={() => {
        // a tap's late mouse events can take focus right after the note opens: take it back
        if (performance.now() - born.current < 400) { setTimeout(() => ref.current?.focus({ preventScroll: true }), 0); return }
        finish('done')
      }}
      onInput={e => onText?.((e.target as HTMLElement).innerText)}
      onPointerDown={e => e.stopPropagation()}
      onKeyDown={e => {
        e.stopPropagation()
        if (e.key === 'Escape') { e.preventDefault(); finish('done') }
        else if (e.key === 'Tab') { e.preventDefault(); finish('tab') }
        else if (e.key === 'Enter' && (e.metaKey || e.ctrlKey || !multiline)) { e.preventDefault(); finish('done') }
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

function StickyView({ o, editing, onEditDone, showAuthor }: ViewProps) {
  const d = o.data
  const p = paper(d.color || 'light-yellow')
  const box = stickyBox(o)
  const [draft, setDraft] = useState(d.text || '')
  useEffect(() => { if (!editing) setDraft(d.text || '') }, [editing, d.text])
  const size = d.size || fitSize(editing ? draft || ' ' : d.text || '', box.w, box.h, Math.round(o.w * 0.2), 8)
  const style: React.CSSProperties = { fontSize: size, padding: `${box.y}px ${box.x}px`, textAlign: d.align || 'center' }
  return (
    <div className="ob ob-sticky" data-id={o.id} style={{ left: o.x, top: o.y, width: o.w, height: o.h, background: p.bg, color: p.ink }}>
      {editing
        ? <Editable id={o.id} text={d.text || ''} className="ob-st-text" style={style} onDone={onEditDone} onText={setDraft} />
        : <div className="ob-st-text" style={style}>{d.text}</div>}
      {showAuthor && o.author && !editing && <span className="ob-author">{o.author}</span>}
    </div>
  )
}

function TextView({ o, editing, onEditDone }: ViewProps) {
  const d = o.data
  const style: React.CSSProperties = { fontSize: d.size || 24, fontWeight: d.bold ? 650 : 400, fontStyle: d.italic ? 'italic' : undefined, color: d.color || undefined, textAlign: d.align || 'left' }
  return (
    <div className={`ob ob-text ${d.text ? '' : 'is-empty'}`} data-id={o.id} style={{ left: o.x, top: o.y, width: o.w, minHeight: o.h }}>
      {editing
        ? <Editable id={o.id} text={d.text || ''} className="ob-tx" style={style} onDone={onEditDone} />
        : <div className="ob-tx" style={style}>{d.text || 'Text'}</div>}
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
    <div className="ob ob-shape" data-id={o.id} style={{ left: o.x, top: o.y, width: o.w, height: o.h }}>
      <svg width={o.w} height={o.h} viewBox={`0 0 ${o.w} ${o.h}`} className="ob-shape-svg" aria-hidden>
        <path d={shapePath(d.shape, o.w, o.h)} fill={p.bg} stroke={d.stroke || 'var(--wb-stroke)'} strokeWidth={d.width || 2} vectorEffect="non-scaling-stroke" strokeLinejoin="round" />
      </svg>
      {editing
        ? <Editable id={o.id} text={d.text || ''} className="ob-sh-text" style={tstyle} onDone={onEditDone} onText={setDraft} />
        : <div className="ob-sh-text" style={tstyle}>{d.text}</div>}
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

function ImageView({ o, imgSrc }: ViewProps) {
  return (
    <div className="ob ob-image" data-id={o.id} style={{ left: o.x, top: o.y, width: o.w, height: o.h }}>
      {imgSrc ? <img src={imgSrc} alt={o.data.name || ''} draggable={false} /> : <span className="ob-image-wait" />}
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
    }
    return null
  })()
  if (p.remote && p.o.kind !== 'line') {
    return <>{el}<div className="ob-remote" style={{ left: p.o.x - 3, top: p.o.y - 3, width: p.o.w + 6, height: p.o.h + 6, borderColor: p.remote }} /></>
  }
  return el
}

export const ObjectView = memo(View, (a, b) =>
  a.o === b.o && a.editing === b.editing && a.imgSrc === b.imgSrc && a.remote === b.remote && a.showAuthor === b.showAuthor &&
  (a.o.kind !== 'line' || a.geom?.d === b.geom?.d) && a.onEditDone === b.onEditDone)
