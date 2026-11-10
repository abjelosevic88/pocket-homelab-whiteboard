// The board's side panels: who is on it and the share link, voting, the
// timer, frames, and Pocket AI.
import { useEffect, useMemo, useState } from 'react'
import { api, V1 } from '../../lib/api'
import type { Board, Member, Obj, Votes } from '../../lib/types'
import { framesInOrder, textOf, paper } from '../../lib/board'
import { Avatar, CopyField, Segmented, Spinner, Switch, copyText, errMsg, useToast } from '../ui'
import { Select } from '../Select'
import { Icon } from '../icons'
import { QRCode } from '../QRCode'

export function Panel({ title, onClose, children, icon }: { title: string; onClose: () => void; children: React.ReactNode; icon?: string }) {
  return (
    <aside className="wb-panel" aria-label={title} onPointerDown={e => e.stopPropagation()} onWheel={e => e.stopPropagation()}>
      <header className="wb-panel-head">
        {icon && <Icon name={icon} size={16} />}
        <h3 className="grow">{title}</h3>
        <button type="button" className="btn ghost icon-only sm" onClick={onClose} aria-label="Close"><Icon name="x" size={16} /></button>
      </header>
      <div className="wb-panel-body">{children}</div>
    </aside>
  )
}

// ---- sharing ----

export function SharePanel({ board, members, setMembers, shareURL, setShareURL, onBoard, onClose, onLeave }: {
  board: Board; members: Member[]; setMembers: (m: Member[]) => void; shareURL: string; setShareURL: (u: string) => void
  onBoard: (b: Partial<Board>) => void; onClose: () => void; onLeave: () => void
}) {
  const { toast } = useToast()
  const owner = board.role === 'owner'
  const [people, setPeople] = useState<{ id: number; name: string; email: string }[]>([])
  const [pick, setPick] = useState('')
  const [role, setRole] = useState<'editor' | 'viewer'>('editor')
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (owner) api.get<{ people: { id: number; name: string; email: string }[] }>(`${V1}/people`).then(r => setPeople(r.people)).catch(() => {}) }, [owner])
  const base = `${V1}/boards/${board.id}`
  const add = async () => {
    if (!pick) return
    setBusy(true)
    try {
      const body = /^\d+$/.test(pick) ? { user_id: Number(pick), role } : { email: pick, role }
      setMembers((await api.post<{ members: Member[] }>(`${base}/members`, body)).members)
      setPick('')
      toast('Added to the board')
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  const setRoleOf = async (m: Member, r: string) => {
    try { setMembers((await api.post<{ members: Member[] }>(`${base}/members`, { user_id: m.user_id, role: r })).members) } catch (e) { toast(errMsg(e), 'err') }
  }
  const remove = async (m: Member) => {
    try { setMembers((await api.del<{ members: Member[] }>(`${base}/members/${m.user_id}`)).members) } catch (e) { toast(errMsg(e), 'err') }
  }
  const mode = async (v: Board['share_mode']) => {
    try { await api.put(base, { share_mode: v }); onBoard({ share_mode: v }) } catch (e) { toast(errMsg(e), 'err') }
  }
  const newLink = async () => {
    try { const r = await api.post<{ share_key: string; share_url: string }>(`${base}/share-key`); setShareURL(r.share_url); onBoard({ share_key: r.share_key }); toast('New link made — the old one no longer works') } catch (e) { toast(errMsg(e), 'err') }
  }
  const taken = new Set(members.map(m => m.user_id))
  const options = people.filter(p => !taken.has(p.id)).map(p => ({ value: String(p.id), label: p.name, hint: p.email }))
  return (
    <Panel title="Share" icon="users" onClose={onClose}>
      <section className="wb-sec">
        <h4>People on this board</h4>
        <ul className="wb-people">
          <li><Avatar name={board.owner_name} size={28} /><span className="grow"><b>{board.owner_name}</b><br /><span className="muted small">Made the board</span></span></li>
          {members.map(m => (
            <li key={m.user_id}>
              <Avatar name={m.name} email={m.email} size={28} />
              <span className="grow ellipsis">{m.name || m.email}<br /><span className="muted small">{m.email}</span></span>
              {owner ? (
                <>
                  <Segmented size="sm" value={m.role} onChange={v => setRoleOf(m, v)} options={[{ value: 'editor', label: 'Edit', title: 'Can change the board' }, { value: 'viewer', label: 'View', title: 'Can look, point and vote' }]} />
                  <button type="button" className="btn ghost icon-only sm" title="Take off the board" aria-label="Take off the board" onClick={() => remove(m)}><Icon name="x" size={14} /></button>
                </>
              ) : <span className="muted small">{m.role === 'editor' ? 'can edit' : 'can view'}</span>}
            </li>
          ))}
        </ul>
        {owner && (
          <div className="wb-add">
            <Select value={pick} onChange={setPick} options={options} placeholder="Someone on this box…" ariaLabel="Person" onCreate={label => label.trim()} createLabel="Add by email" />
            <Segmented size="sm" value={role} onChange={setRole} options={[{ value: 'editor', label: 'Edit' }, { value: 'viewer', label: 'View' }]} />
            <button type="button" className="btn primary sm" disabled={!pick || busy} onClick={add}>Add</button>
          </div>
        )}
        {!owner && <button type="button" className="btn sm" onClick={onLeave}><Icon name="log-out" size={14} />Leave this board</button>}
      </section>
      {owner && (
        <section className="wb-sec">
          <h4>Link for people without an account</h4>
          <Segmented value={board.share_mode} onChange={mode} options={[{ value: 'off', label: 'Off' }, { value: 'view', label: 'Can view' }, { value: 'edit', label: 'Can edit' }]} />
          {board.share_mode !== 'off' && shareURL && (
            <div className="stack">
              <CopyField value={shareURL} />
              <p className="muted small">Anyone with this link {board.share_mode === 'edit' ? 'can add and change things' : 'can look, point and vote'} — they type a name, no account needed. Your other boards and this box stay closed to them.</p>
              <div className="wb-qr"><QRCode value={shareURL} size={148} label="The board's link" /></div>
              <button type="button" className="btn sm" onClick={newLink}><Icon name="refresh" size={14} />Make a new link</button>
            </div>
          )}
        </section>
      )}
    </Panel>
  )
}

// ---- voting ----

export function VotePanel({ votes, canEdit, objs, onStart, onEnd, onClear, onFocus, onClose }: {
  votes: Votes; canEdit: boolean; objs: Map<string, Obj>
  onStart: (per: number, hide: boolean) => void; onEnd: () => void; onClear: () => void; onFocus: (id: string) => void; onClose: () => void
}) {
  const [per, setPer] = useState(votes.per_person || 3)
  const [hide, setHide] = useState(votes.hide)
  const ranked = useMemo(() => Object.entries(votes.counts).filter(([id]) => objs.has(id)).sort((a, b) => b[1] - a[1]), [votes.counts, objs])
  return (
    <Panel title="Voting" icon="vote" onClose={onClose}>
      {votes.open ? (
        <section className="wb-sec">
          <p><b>Voting is on.</b> Click a note to give it one of your dots; click your dot on it to take it back.</p>
          <p className="wb-big">{votes.left} <span className="muted">of {votes.per_person} {votes.per_person === 1 ? 'vote' : 'votes'} left</span></p>
          <p className="muted small">{votes.voters} {votes.voters === 1 ? 'person has' : 'people have'} voted{votes.hide ? ' · counts show when the vote ends' : ''}</p>
          {canEdit && <button type="button" className="btn primary" onClick={onEnd}><Icon name="check" size={15} />End the vote</button>}
        </section>
      ) : canEdit && (
        <section className="wb-sec">
          <h4>{votes.done ? 'Vote again' : 'Start a vote'}</h4>
          <p className="muted small">Everyone on the board gets dots to put on the notes that matter most.</p>
          <label className="field-lbl">Votes each</label>
          <Segmented value={per} onChange={setPer} options={[1, 2, 3, 5, 10].map(n => ({ value: n, label: String(n) }))} />
          <label className="check-row"><Switch checked={hide} onChange={setHide} /><span>Hide the counts until the vote ends</span></label>
          <button type="button" className="btn primary" onClick={() => onStart(per, hide)}><Icon name="play" size={15} />Start the vote</button>
        </section>
      )}
      {(votes.done || (votes.open && !votes.hide)) && (
        <section className="wb-sec">
          <h4>{votes.done ? 'Results' : 'So far'}</h4>
          {ranked.length === 0 && <p className="muted small">No votes yet.</p>}
          <ol className="wb-rank">
            {ranked.slice(0, 15).map(([id, n]) => {
              const o = objs.get(id)!
              return (
                <li key={id}>
                  <button type="button" className="wb-rank-row" onClick={() => onFocus(id)}>
                    <span className="wb-rank-dot" style={{ background: paper(o.data.color || o.data.fill || 'gray').bg }} />
                    <span className="grow ellipsis">{textOf(o) || (o.kind === 'image' ? 'A picture' : 'A note')}</span>
                    <b>{n}</b>
                  </button>
                </li>
              )
            })}
          </ol>
          {votes.done && canEdit && <button type="button" className="btn sm" onClick={onClear}><Icon name="eye-off" size={14} />Hide the results</button>}
        </section>
      )}
      {!canEdit && !votes.open && !votes.done && <p className="muted">There is no vote going on. Someone who can edit the board starts one.</p>}
    </Panel>
  )
}

// ---- timer ----

export function TimerPanel({ board, canEdit, onSet, onClose }: { board: Board; canEdit: boolean; onSet: (b: { seconds?: number; add?: number }) => void; onClose: () => void }) {
  const [min, setMin] = useState('5')
  const running = !!board.timer_ends
  return (
    <Panel title="Timer" icon="timer" onClose={onClose}>
      <section className="wb-sec">
        <p className="muted small">Everyone on the board sees the same countdown, and a soft chime when it ends.</p>
        {running && <Countdown ends={board.timer_ends} big />}
        {canEdit ? (
          running ? (
            <div className="row wrap">
              <button type="button" className="btn" onClick={() => onSet({ add: 60 })}>+1 min</button>
              <button type="button" className="btn" onClick={() => onSet({ add: 300 })}>+5 min</button>
              <button type="button" className="btn danger" onClick={() => onSet({ seconds: 0 })}><Icon name="stop" size={14} />Stop</button>
            </div>
          ) : (
            <>
              <div className="row wrap">{[1, 3, 5, 10, 15].map(m => <button key={m} type="button" className="btn" onClick={() => onSet({ seconds: m * 60 })}>{m} min</button>)}</div>
              <div className="row">
                <input type="number" min={1} max={360} value={min} onChange={e => setMin(e.target.value)} aria-label="Minutes" style={{ width: 90 }} />
                <span className="muted">minutes</span>
                <button type="button" className="btn primary" onClick={() => onSet({ seconds: Math.max(1, Number(min) || 5) * 60 })}><Icon name="play" size={14} />Start</button>
              </div>
            </>
          )
        ) : !running && <p className="muted">No timer is running.</p>}
      </section>
    </Panel>
  )
}

/** mm:ss until a moment, ticking each second. */
export function Countdown({ ends, big, onEnd }: { ends: string; big?: boolean; onEnd?: () => void }) {
  const [now, setNow] = useState(Date.now())
  const end = Date.parse(ends)
  useEffect(() => { const t = window.setInterval(() => setNow(Date.now()), 500); return () => window.clearInterval(t) }, [])
  const left = Math.max(0, Math.round((end - now) / 1000))
  useEffect(() => { if (left === 0 && onEnd) onEnd() }, [left === 0]) // eslint-disable-line react-hooks/exhaustive-deps
  const txt = left === 0 ? 'Time is up' : `${Math.floor(left / 60)}:${String(left % 60).padStart(2, '0')}`
  return <span className={`wb-count ${big ? 'big' : ''} ${left === 0 ? 'up' : left <= 30 ? 'soon' : ''}`}>{txt}</span>
}

/** A soft three-note chime. */
export function chime() {
  try {
    const ac = new AudioContext()
    ;[523.25, 659.25, 783.99].forEach((f, i) => {
      const o = ac.createOscillator(), g = ac.createGain()
      o.frequency.value = f
      o.connect(g); g.connect(ac.destination)
      const t = ac.currentTime + i * 0.18
      g.gain.setValueAtTime(0, t); g.gain.linearRampToValueAtTime(0.18, t + 0.02); g.gain.exponentialRampToValueAtTime(0.001, t + 0.9)
      o.start(t); o.stop(t + 1)
    })
  } catch { /* no sound */ }
}

// ---- frames ----

export function FramesPanel({ objs, onFocus, onPresent, onClose }: { objs: Map<string, Obj>; onFocus: (id: string) => void; onPresent: () => void; onClose: () => void }) {
  const frames = framesInOrder(objs.values())
  return (
    <Panel title="Frames" icon="frame" onClose={onClose}>
      {frames.length === 0 ? <p className="muted">No frames yet. Press F and drag on the board to draw one — frames hold notes together and are the slides when you present.</p> : (
        <>
          <ol className="wb-frames">
            {frames.map((f, i) => (
              <li key={f.id}><button type="button" className="wb-rank-row" onClick={() => onFocus(f.id)}><span className="muted small">{i + 1}</span><span className="grow ellipsis">{f.data.title || 'Frame'}</span></button></li>
            ))}
          </ol>
          <button type="button" className="btn primary" onClick={onPresent}><Icon name="presentation" size={15} />Present</button>
        </>
      )}
    </Panel>
  )
}

// ---- Pocket AI ----

export function AIPanel({ base, picked, canEdit, onGroups, onIdeas, onNote, onClose }: {
  base: string; picked: string[]; canEdit: boolean
  onGroups: (g: { title: string; ids: string[] }[]) => void; onIdeas: (ideas: string[]) => void; onNote: (text: string) => void; onClose: () => void
}) {
  const { toast } = useToast()
  const [busy, setBusy] = useState('')
  const [summary, setSummary] = useState('')
  const [topic, setTopic] = useState('')
  const [n, setN] = useState(8)
  const run = async (what: string, fn: () => Promise<void>) => {
    setBusy(what)
    try { await fn() } catch (e) { toast(errMsg(e), 'err') } finally { setBusy('') }
  }
  const scope = picked.length ? `the ${picked.length} you picked` : 'every note on the board'
  return (
    <Panel title="Pocket AI" icon="sparkles" onClose={onClose}>
      {canEdit && (
        <section className="wb-sec">
          <h4>Group by theme</h4>
          <p className="muted small">Sorts {scope} into frames by what they are about. Undo puts them back.</p>
          <button type="button" className="btn" disabled={!!busy} onClick={() => run('group', async () => onGroups((await api.post<{ groups: { title: string; ids: string[] }[] }>(`${base}/ai/group`, { ids: picked })).groups))}>
            {busy === 'group' ? <Spinner /> : <Icon name="grid-sort" size={15} />}Group the notes
          </button>
        </section>
      )}
      <section className="wb-sec">
        <h4>Sum it up</h4>
        <p className="muted small">The main points and the next steps of {scope}; votes count.</p>
        <button type="button" className="btn" disabled={!!busy} onClick={() => run('sum', async () => setSummary((await api.post<{ text: string }>(`${base}/ai/summary`, { ids: picked })).text))}>
          {busy === 'sum' ? <Spinner /> : <Icon name="text" size={15} />}Sum up
        </button>
        {summary && (
          <div className="wb-summary">
            <p className="pre">{summary}</p>
            <div className="row">
              <button type="button" className="btn sm" onClick={async () => { if (await copyText(summary)) toast('Copied') }}><Icon name="copy" size={14} />Copy</button>
              {canEdit && <button type="button" className="btn sm" onClick={() => onNote(summary)}><Icon name="plus" size={14} />Put on the board</button>}
            </div>
          </div>
        )}
      </section>
      {canEdit && (
        <section className="wb-sec">
          <h4>More ideas</h4>
          <input value={topic} onChange={e => setTopic(e.target.value)} placeholder="About… (else the board's name)" aria-label="Topic" />
          <Segmented value={n} onChange={setN} options={[4, 8, 12].map(v => ({ value: v, label: `${v} notes` }))} />
          <button type="button" className="btn" disabled={!!busy} onClick={() => run('ideas', async () => onIdeas((await api.post<{ ideas: string[] }>(`${base}/ai/ideas`, { topic, n })).ideas))}>
            {busy === 'ideas' ? <Spinner /> : <Icon name="lightbulb" size={15} />}Suggest notes
          </button>
        </section>
      )}
    </Panel>
  )
}

// ---- send to a chat ----

export function ChatPicker({ onSend, onClose }: { onSend: (to: string) => void; onClose: () => void }) {
  const [list, setList] = useState<{ id: string; name: string }[] | null>(null)
  const [to, setTo] = useState('me')
  const [err, setErr] = useState('')
  useEffect(() => { api.get<{ conversations: { id: string; name: string }[] }>(`${V1}/apps/chats`).then(r => setList(r.conversations)).catch(e => setErr(errMsg(e))) }, [])
  return (
    <Panel title="Send to Pocket Chat" icon="message" onClose={onClose}>
      <section className="wb-sec">
        {err ? <p className="bad">{err}</p> : !list ? <Spinner /> : (
          <>
            <Select value={to} onChange={setTo} options={[{ value: 'me', label: 'Notes to self' }, ...list.map(c => ({ value: c.id, label: c.name || 'Conversation' }))]} ariaLabel="Conversation" />
            <button type="button" className="btn primary" onClick={() => onSend(to)}><Icon name="send" size={14} />Send the link</button>
          </>
        )}
      </section>
    </Panel>
  )
}
