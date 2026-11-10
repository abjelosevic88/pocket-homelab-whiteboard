// Planning poker and picture cropping.
import { useRef, useState } from 'react'
import type { Obj, Poker } from '../../lib/types'
import { Icon } from '../icons'
import { Modal } from '../ui'

export const POKER_CARDS = ['0', '½', '1', '2', '3', '5', '8', '13', '20', '40', '100', '?', '☕']

/** The bar everyone sees while a round of planning poker is on. */
export function PokerBar({ poker, what, mine, host, onPick, onShow, onAgain, onSave, onEnd, onFocus }: {
  poker: Poker; what: string; mine: string; host: boolean
  onPick: (v: string) => void; onShow: () => void; onAgain: () => void; onSave: (v: string) => void; onEnd: () => void; onFocus: () => void
}) {
  const n = poker.picked_n ?? poker.picked.length
  const picks = poker.picks || []
  // the most picked number is what the room seems to agree on
  const counts = new Map<string, number>()
  for (const p of picks) counts.set(p.value, (counts.get(p.value) || 0) + 1)
  const top = [...counts.entries()].sort((a, b) => b[1] - a[1])[0]
  const agree = picks.length > 1 && counts.size === 1
  const suggest = top ? top[0] : ''
  return (
    <div className="wb-poker" role="region" aria-label="Planning poker">
      <div className="wb-poker-head">
        <Icon name="poker" size={16} />
        <button type="button" className="linkish wb-poker-what" onClick={onFocus} title="Show it on the board">{what || 'This'}</button>
        <span className="muted small">{poker.revealed ? (agree ? 'Everyone agrees' : `${picks.length} ${picks.length === 1 ? 'pick' : 'picks'}`) : `${n} picked${n && poker.picked.length ? ': ' + poker.picked.join(', ') : ''}`}</span>
        <span className="spacer" />
        {host && !poker.revealed && <button type="button" className="btn sm primary" disabled={!n} onClick={onShow}>Show the picks</button>}
        {host && poker.revealed && <>
          <button type="button" className="btn sm" onClick={onAgain}>Again</button>
          {suggest && <button type="button" className="btn sm primary" onClick={() => onSave(suggest)}>Keep {suggest}</button>}
        </>}
        {host && <button type="button" className="btn ghost icon-only sm" onClick={onEnd} title="End the round" aria-label="End the round"><Icon name="x" size={15} /></button>}
      </div>
      {!poker.revealed ? (
        <div className="wb-poker-cards">
          {POKER_CARDS.map(v => <button key={v} type="button" className={`wb-poker-card ${mine === v ? 'on' : ''}`} onClick={() => onPick(mine === v ? '' : v)} aria-pressed={mine === v}>{v}</button>)}
        </div>
      ) : (
        <div className="wb-poker-results">
          {picks.map((p, i) => (
            <div key={i} className={`wb-poker-result ${p.value === suggest ? 'top' : ''}`}>
              <span className="wb-poker-card on">{p.value}</span>
              {p.name && <span className="small">{p.name}</span>}
              {host && <button type="button" className="btn ghost sm" onClick={() => onSave(p.value)} title={`Keep ${p.value} on it`}>Keep</button>}
            </div>
          ))}
          {poker.average !== undefined && <span className="muted small wb-poker-avg">Average {poker.average}</span>}
        </div>
      )}
    </div>
  )
}

type Box = { x: number; y: number; w: number; h: number }

/** Crop a picture: drag the box or its corners; the rest is cut off (and can be brought back). */
export function CropDialog({ src, o, onDone, onClose }: { src: string; o: Obj; onDone: (crop: Box | undefined, w: number, h: number) => void; onClose: () => void }) {
  const [box, setBox] = useState<Box>(o.data.crop || { x: 0, y: 0, w: 1, h: 1 })
  const [nat, setNat] = useState<{ w: number; h: number } | null>(null)
  const area = useRef<HTMLDivElement>(null)
  const drag = useRef<{ part: string; sx: number; sy: number; b: Box } | null>(null)
  const down = (part: string) => (e: React.PointerEvent) => {
    e.preventDefault(); e.stopPropagation()
    ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
    drag.current = { part, sx: e.clientX, sy: e.clientY, b: box }
  }
  const move = (e: React.PointerEvent) => {
    const d = drag.current, el = area.current
    if (!d || !el) return
    const r = el.getBoundingClientRect()
    const dx = (e.clientX - d.sx) / r.width, dy = (e.clientY - d.sy) / r.height
    let { x, y, w, h } = d.b
    const min = 0.05
    if (d.part === 'move') { x = Math.min(Math.max(0, x + dx), 1 - w); y = Math.min(Math.max(0, y + dy), 1 - h) }
    else {
      if (d.part.includes('w')) { const nx = Math.min(Math.max(0, x + dx), x + w - min); w += x - nx; x = nx }
      if (d.part.includes('e')) w = Math.min(Math.max(min, w + dx), 1 - x)
      if (d.part.includes('n')) { const ny = Math.min(Math.max(0, y + dy), y + h - min); h += y - ny; y = ny }
      if (d.part.includes('s')) h = Math.min(Math.max(min, h + dy), 1 - y)
    }
    setBox({ x, y, w, h })
  }
  const save = () => {
    const full = box.x < 0.001 && box.y < 0.001 && box.w > 0.999 && box.h > 0.999
    // keep the picture's scale on the board: the kept part is as big as it was in the whole picture
    const old = o.data.crop || { x: 0, y: 0, w: 1, h: 1 }
    const fullW = o.w / old.w, fullH = o.h / old.h
    onDone(full ? undefined : box, fullW * box.w, fullH * box.h)
  }
  const ratio = nat ? nat.w / nat.h : o.w / o.h
  return (
    <Modal title="Crop the picture" onClose={onClose} footer={<>
      <button type="button" className="btn" onClick={() => setBox({ x: 0, y: 0, w: 1, h: 1 })}>Whole picture</button>
      <span className="spacer" />
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" onClick={save}>Crop</button>
    </>}>
      <div className="stack">
        <div ref={area} className="wb-crop-area" style={{ aspectRatio: `${ratio}` }} onPointerMove={move} onPointerUp={() => { drag.current = null }}>
          <img src={src} alt="" draggable={false} onLoad={e => setNat({ w: e.currentTarget.naturalWidth, h: e.currentTarget.naturalHeight })} />
          <div className="wb-crop-box" style={{ left: `${box.x * 100}%`, top: `${box.y * 100}%`, width: `${box.w * 100}%`, height: `${box.h * 100}%` }} onPointerDown={down('move')}>
            {['nw', 'ne', 'sw', 'se', 'n', 's', 'w', 'e'].map(p => <i key={p} className={`wb-crop-h ${p}`} onPointerDown={down(p)} />)}
          </div>
        </div>
        <p className="muted small">Drag the box or its edges. Nothing is lost: crop again to bring the rest back.</p>
      </div>
    </Modal>
  )
}
