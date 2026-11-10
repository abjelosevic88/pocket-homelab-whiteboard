// Small pieces of the board: find, the format bar, emoji, tags, floating reactions.
import { useEffect, useRef, useState } from 'react'
import { Icon } from '../icons'
import { activeEditor, tagColour } from './ObjectView'

export const REACTIONS = ['👍', '❤️', '😂', '🎉', '🔥', '👀', '✅', '❓']
export const STICKERS = ['⭐', '❤️', '👍', '👎', '✅', '❌', '❓', '❗', '💡', '🔥', '🎯', '🚀', '⚠️', '🏆', '💰', '⏰', '📌', '🎉', '😀', '😍', '🤔', '😬', '😢', '🙏', '👏', '💪', '👀', '🐞', '🔒', '📈', '📉', '🧠']

// ---- find on the board ----

export function FindBar({ q, setQ, n, i, onNext, onPrev, onClose }: { q: string; setQ: (v: string) => void; n: number; i: number; onNext: () => void; onPrev: () => void; onClose: () => void }) {
  const ref = useRef<HTMLInputElement>(null)
  useEffect(() => { ref.current?.focus(); ref.current?.select() }, [])
  return (
    <div className="wb-find" onPointerDown={e => e.stopPropagation()} role="search">
      <Icon name="search" size={15} />
      <input ref={ref} value={q} onChange={e => setQ(e.target.value)} placeholder="Find on this board (#tag for tags)" aria-label="Find on this board"
        onKeyDown={e => {
          e.stopPropagation()
          if (e.key === 'Enter') { e.preventDefault(); if (e.shiftKey) onPrev(); else onNext() }
          if (e.key === 'Escape') { e.preventDefault(); onClose() }
          if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'f') { e.preventDefault(); ref.current?.select() }
        }} />
      <span className="muted small nowrap">{q.trim() ? (n ? `${i + 1} of ${n}` : 'None') : ''}</span>
      <button type="button" className="btn ghost icon-only sm" onClick={onPrev} disabled={!n} aria-label="Previous" title="Previous (⇧Enter)"><Icon name="chevron-up" size={15} /></button>
      <button type="button" className="btn ghost icon-only sm" onClick={onNext} disabled={!n} aria-label="Next" title="Next (Enter)"><Icon name="chevron-down" size={15} /></button>
      <button type="button" className="btn ghost icon-only sm" onClick={onClose} aria-label="Close" title="Close (Esc)"><Icon name="x" size={15} /></button>
    </div>
  )
}

// ---- formatting while writing ----

const SIZES: { v: number; label: string }[] = [{ v: 0, label: 'Auto' }, { v: 12, label: '12' }, { v: 16, label: '16' }, { v: 20, label: '20' }, { v: 28, label: '28' }, { v: 36, label: '36' }, { v: 48, label: '48' }, { v: 64, label: '64' }, { v: 96, label: '96' }]

export function FormatBar({ pos, size, onSize }: { pos: { left: number; top: number }; size: number; onSize: (v: number) => void }) {
  const [sizes, setSizes] = useState(false)
  const run = (cmd: string, arg?: string) => { activeEditor?.focus(); document.execCommand(cmd, false, arg) }
  const keep = (e: React.PointerEvent | React.MouseEvent) => e.preventDefault() // the note keeps the focus
  const link = () => {
    const sel = window.getSelection()
    if (!sel || sel.isCollapsed) { window.alert('Pick some words first, then make them a link.'); return }
    const url = window.prompt('Link to (https://…)')
    if (url) run('createLink', /^(https?:|mailto:)/i.test(url) ? url : 'https://' + url)
  }
  const btn = (icon: string, label: string, onClick: () => void, k?: string) => (
    <button type="button" className="btn ghost icon-only sm" tabIndex={-1} title={k ? `${label} (${k})` : label} aria-label={label} onPointerDown={keep} onMouseDown={keep} onClick={onClick}><Icon name={icon} size={16} /></button>
  )
  return (
    <div className="wb-format wb-ctx" style={pos} onPointerDown={e => e.stopPropagation()} role="toolbar" aria-label="Formatting">
      {btn('bold', 'Bold', () => run('bold'), '⌘B')}
      {btn('italic', 'Italic', () => run('italic'), '⌘I')}
      {btn('underline', 'Underline', () => run('underline'), '⌘U')}
      {btn('strike', 'Strike through', () => run('strikeThrough'))}
      {btn('list', 'Bullet list', () => run('insertUnorderedList'))}
      {btn('list-ordered', 'Numbered list', () => run('insertOrderedList'))}
      {btn('link', 'Link', link)}
      <div className="wb-ctx-drop">
        <button type="button" className="btn ghost sm" tabIndex={-1} onPointerDown={keep} onMouseDown={keep} onClick={() => setSizes(v => !v)} title="Text size">{size ? size : 'Auto'}<Icon name="chevron-down" size={13} /></button>
        {sizes && <div className="wb-ctx-pop wb-sizes">{SIZES.map(s => <button key={s.v} type="button" tabIndex={-1} className={`btn ghost sm ${s.v === size ? 'on' : ''}`} onPointerDown={keep} onMouseDown={keep} onClick={() => { onSize(s.v); setSizes(false) }}>{s.label}</button>)}</div>}
      </div>
    </div>
  )
}

export { SIZES as TEXT_SIZES }

// ---- emoji ----

export function EmojiGrid({ list, onPick, cols = 8 }: { list: string[]; onPick: (e: string) => void; cols?: number }) {
  return (
    <div className="wb-emoji" style={{ gridTemplateColumns: `repeat(${cols}, 34px)` }}>
      {list.map(e => <button key={e} type="button" onClick={() => onPick(e)} aria-label={e}>{e}</button>)}
    </div>
  )
}

/** Reactions floating up the screen for everyone on the board. */
export interface Float { id: number; emoji: string; name: string; colour: string; x: number }
export function FloatLayer({ floats }: { floats: Float[] }) {
  return (
    <div className="wb-floats" aria-hidden>
      {floats.map(f => (
        <div key={f.id} className="wb-float" style={{ left: `${f.x}%`, ['--pc' as string]: f.colour }}>
          <span className="e">{f.emoji}</span>
          <span className="n">{f.name}</span>
        </div>
      ))}
    </div>
  )
}

// ---- tags ----

export function TagPicker({ all, on, onToggle, onClose }: { all: string[]; on: (t: string) => 'all' | 'some' | 'none'; onToggle: (t: string, add: boolean) => void; onClose: () => void }) {
  const [v, setV] = useState('')
  const ref = useRef<HTMLInputElement>(null)
  useEffect(() => { ref.current?.focus() }, [])
  const add = () => { const t = v.trim().replace(/^#/, '').slice(0, 30); if (t) { onToggle(t, true); setV('') } }
  return (
    <div className="wb-ctx-pop wb-tags" onPointerDown={e => e.stopPropagation()}>
      <input ref={ref} value={v} onChange={e => setV(e.target.value)} placeholder="New tag" aria-label="New tag"
        onKeyDown={e => { e.stopPropagation(); if (e.key === 'Enter') { e.preventDefault(); add() } if (e.key === 'Escape') onClose() }} />
      <div className="wb-tag-list">
        {all.length === 0 && <span className="muted small">No tags on this board yet.</span>}
        {all.map(t => {
          const st = on(t)
          return (
            <button key={t} type="button" className={`wb-tag ${st}`} onClick={() => onToggle(t, st !== 'all')}>
              <i style={{ background: tagColour(t) }} />{t}{st === 'all' && <Icon name="check" size={13} />}
            </button>
          )
        })}
      </div>
    </div>
  )
}
