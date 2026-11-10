// Comments: threads pinned to a spot or a thing on the board, replies,
// @mentions (people on the board get a push), resolve.
import { useEffect, useMemo, useRef, useState } from 'react'
import type { Comment, Obj } from '../../lib/types'
import { ago } from '../../lib/format'
import { Avatar, errMsg, useToast } from '../ui'
import { Icon } from '../icons'
import { Panel } from './Panels'

export type Req = <T>(method: string, path: string, body?: unknown) => Promise<T>
export interface MentionPerson { id: number; name: string }
export interface Thread { root: Comment; replies: Comment[] }

export function threadsOf(list: Comment[]): Thread[] {
  const roots = list.filter(c => !c.parent_id)
  return roots.map(r => ({ root: r, replies: list.filter(c => c.parent_id === r.id) }))
}

/** Where a thread's pin sits: on its thing's corner, else its spot. */
export function pinAt(t: Thread, objs: Map<string, Obj>): { x: number; y: number } {
  const o = t.root.object_id ? objs.get(t.root.object_id) : undefined
  return o ? { x: o.x + o.w, y: o.y } : { x: t.root.x, y: t.root.y }
}

function Body({ text, people }: { text: string; people: MentionPerson[] }) {
  const names = people.map(p => p.name).filter(Boolean).sort((a, b) => b.length - a.length)
  if (!names.length) return <p className="wb-c-body">{text}</p>
  const re = new RegExp(`(@(?:${names.map(n => n.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|')}))`, 'g')
  return <p className="wb-c-body">{text.split(re).map((part, i) => part.startsWith('@') && names.includes(part.slice(1)) ? <b key={i} className="wb-mention">{part}</b> : part)}</p>
}

/** A text box that offers the people on the board after an @. */
export function Composer({ people, onSend, placeholder, autoFocus, busy }: { people: MentionPerson[]; onSend: (body: string, mentions: number[]) => Promise<void> | void; placeholder: string; autoFocus?: boolean; busy?: boolean }) {
  const [v, setV] = useState('')
  const [mentions, setMentions] = useState<number[]>([])
  const [q, setQ] = useState<string | null>(null)
  const [hi, setHi] = useState(0)
  const ref = useRef<HTMLTextAreaElement>(null)
  useEffect(() => { if (autoFocus) ref.current?.focus() }, [autoFocus])
  const matches = q === null ? [] : people.filter(p => p.name.toLowerCase().startsWith(q.toLowerCase()) || p.name.toLowerCase().includes(' ' + q.toLowerCase())).slice(0, 6)
  const onChange = (t: string) => {
    setV(t)
    const caret = ref.current?.selectionStart ?? t.length
    const m = /(^|\s)@([^\s@]{0,30})$/.exec(t.slice(0, caret))
    setQ(m ? m[2] : null)
    setHi(0)
  }
  const pick = (p: MentionPerson) => {
    const el = ref.current
    const caret = el?.selectionStart ?? v.length
    const before = v.slice(0, caret).replace(/@([^\s@]{0,30})$/, '@' + p.name + ' ')
    setV(before + v.slice(caret))
    setMentions(m => m.includes(p.id) ? m : [...m, p.id])
    setQ(null)
    setTimeout(() => { el?.focus(); el?.setSelectionRange(before.length, before.length) }, 0)
  }
  const send = async () => {
    const body = v.trim()
    if (!body) return
    const used = mentions.filter(id => body.includes('@' + (people.find(p => p.id === id)?.name || '\u0000')))
    await onSend(body, used)
    setV(''); setMentions([])
  }
  return (
    <div className="wb-composer">
      <textarea ref={ref} rows={2} value={v} placeholder={placeholder} onChange={e => onChange(e.target.value)} aria-label="Comment"
        onKeyDown={e => {
          e.stopPropagation()
          if (matches.length && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) { e.preventDefault(); setHi(h => (h + (e.key === 'ArrowDown' ? 1 : -1) + matches.length) % matches.length); return }
          if (matches.length && (e.key === 'Enter' || e.key === 'Tab')) { e.preventDefault(); pick(matches[hi]); return }
          if (e.key === 'Escape' && q !== null) { e.preventDefault(); setQ(null); return }
          if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send() }
        }} />
      {matches.length > 0 && (
        <div className="wb-mentions" role="listbox">
          {matches.map((p, i) => <button key={p.id} type="button" className={i === hi ? 'hi' : ''} onMouseDown={e => { e.preventDefault(); pick(p) }}><Avatar name={p.name} size={20} />{p.name}</button>)}
        </div>
      )}
      <div className="wb-composer-foot">
        <span className="muted small">@ to mention · Enter to send</span>
        <button type="button" className="btn primary sm" disabled={!v.trim() || busy} onClick={send}><Icon name="send" size={13} />Send</button>
      </div>
    </div>
  )
}

/** One thread, opened beside its pin. */
export function ThreadCard({ thread, pos, people, req, myVoter, canResolve, isOwner, guest, onClose, onChanged }: {
  thread: Thread | null; pos: { left: number; top: number }; people: MentionPerson[]; req: Req; myVoter: string; canResolve: boolean; isOwner: boolean; guest: boolean
  onClose: () => void; onChanged: (opened?: number) => void
  newAt?: { object_id: string; x: number; y: number }
} & { newAt?: { object_id: string; x: number; y: number } }) {
  const { toast } = useToast()
  const [busy, setBusy] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const h = (e: PointerEvent) => { if (ref.current && !ref.current.contains(e.target as Node) && !(e.target as HTMLElement).closest('[data-pin]')) onClose() }
    window.addEventListener('pointerdown', h, true)
    return () => window.removeEventListener('pointerdown', h, true)
  }, [onClose])
  return (
    <div ref={ref} className="wb-thread" style={pos} onPointerDown={e => e.stopPropagation()} onWheel={e => e.stopPropagation()} role="dialog" aria-label="Comments">
      {thread ? (
        <>
          <header className="wb-thread-head">
            <span className="grow muted small">{thread.replies.length ? `${thread.replies.length + 1} comments` : 'Comment'}</span>
            {canResolve && !guest && <button type="button" className={`btn ghost icon-only sm ${thread.root.resolved ? 'on' : ''}`} title={thread.root.resolved ? 'Open again' : 'Resolve'} aria-label="Resolve"
              onClick={async () => { try { await req('PUT', `/comments/${thread.root.id}`, { resolved: !thread.root.resolved }); onChanged(); if (!thread.root.resolved) onClose() } catch (e) { toast(errMsg(e), 'err') } }}><Icon name="check-circle" size={16} /></button>}
            <button type="button" className="btn ghost icon-only sm" onClick={onClose} aria-label="Close"><Icon name="x" size={15} /></button>
          </header>
          <div className="wb-thread-list">
            {[thread.root, ...thread.replies].map(c => (
              <div key={c.id} className="wb-c">
                <Avatar name={c.author} size={26} />
                <div className="grow">
                  <div className="wb-c-meta"><b>{c.author}</b><span className="muted small">{ago(c.created_at)}</span>
                    {!guest && (c.voter === myVoter || isOwner) && <button type="button" className="btn ghost icon-only xs" title="Delete" aria-label="Delete"
                      onClick={async () => { if (!window.confirm(c.id === thread.root.id ? 'Delete the whole thread?' : 'Delete this reply?')) return; try { await req('DELETE', `/comments/${c.id}`); onChanged(); if (c.id === thread.root.id) onClose() } catch (e) { toast(errMsg(e), 'err') } }}><Icon name="trash" size={13} /></button>}
                  </div>
                  <Body text={c.body} people={people} />
                </div>
              </div>
            ))}
          </div>
          <Composer people={people} placeholder="Reply" busy={busy} autoFocus onSend={async (body, mentions) => {
            setBusy(true)
            try { await req('POST', '/comments', { parent_id: thread.root.id, body, mentions }); onChanged() } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
          }} />
        </>
      ) : null}
    </div>
  )
}

/** A new thread being written at a spot. */
export function NewThread({ at, pos, people, req, onClose, onCreated }: { at: { object_id: string; x: number; y: number }; pos: { left: number; top: number }; people: MentionPerson[]; req: Req; onClose: () => void; onCreated: (id: number) => void }) {
  const { toast } = useToast()
  const [busy, setBusy] = useState(false)
  return (
    <div className="wb-thread" style={pos} onPointerDown={e => e.stopPropagation()} role="dialog" aria-label="New comment">
      <header className="wb-thread-head"><span className="grow muted small">New comment</span><button type="button" className="btn ghost icon-only sm" onClick={onClose} aria-label="Close"><Icon name="x" size={15} /></button></header>
      <Composer people={people} placeholder="Add a comment — @ to mention someone" busy={busy} autoFocus onSend={async (body, mentions) => {
        setBusy(true)
        try { const r = await req<{ comment: Comment }>('POST', '/comments', { ...at, body, mentions }); onCreated(r.comment.id) } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
      }} />
    </div>
  )
}

/** Every thread on the board. */
export function CommentsPanel({ threads, people, objs, onOpen, onClose, showResolved, setShowResolved }: {
  threads: Thread[]; people: MentionPerson[]; objs: Map<string, Obj>; onOpen: (id: number) => void; onClose: () => void; showResolved: boolean; setShowResolved: (v: boolean) => void
}) {
  const list = useMemo(() => [...threads].filter(t => showResolved || !t.root.resolved).sort((a, b) => (a.root.resolved === b.root.resolved ? b.root.id - a.root.id : a.root.resolved ? 1 : -1)), [threads, showResolved])
  return (
    <Panel title="Comments" icon="comment" onClose={onClose}>
      <section className="wb-sec">
        <label className="check-row"><input type="checkbox" checked={showResolved} onChange={e => setShowResolved(e.target.checked)} /><span>Show resolved</span></label>
        {list.length === 0 && <p className="muted">No comments{showResolved ? '' : ' to answer'}. Press C and click the board, or a note, to start one.</p>}
        <ol className="wb-frames">
          {list.map(t => {
            const o = t.root.object_id ? objs.get(t.root.object_id) : undefined
            const on = o ? (o.data.text || o.data.title || '').slice(0, 40) : ''
            return (
              <li key={t.root.id}>
                <button type="button" className={`wb-c-row ${t.root.resolved ? 'resolved' : ''}`} onClick={() => onOpen(t.root.id)}>
                  <Avatar name={t.root.author} size={24} />
                  <span className="grow">
                    <b>{t.root.author}</b> <span className="muted small">{ago(t.root.created_at)}{t.replies.length ? ` · ${t.replies.length} ${t.replies.length === 1 ? 'reply' : 'replies'}` : ''}</span>
                    <Body text={t.root.body.slice(0, 140)} people={people} />
                    {on && <span className="muted small">on “{on}”</span>}
                  </span>
                  {t.root.resolved && <Icon name="check-circle" size={14} className="ok" />}
                </button>
              </li>
            )
          })}
        </ol>
      </section>
    </Panel>
  )
}
