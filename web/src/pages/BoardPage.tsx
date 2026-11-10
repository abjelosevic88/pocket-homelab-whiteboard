// A board: the canvas without edges, its tools and everyone on it live.
// The same editor serves people with an account (/b/:id) and guests
// through the share link (/s/:key).
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { ApiError, V1, api } from '../lib/api'
import { useApp } from '../lib/app-context'
import { useAppState, DataProvider } from '../lib/data'
import type { Board, BoardDoc, Comment, Member, Obj, ObjData, Op, Person, PrivateCount, Reaction, ShapeForm, View, Votes } from '../lib/types'
import {
  INKS, STICKY, STICKY_KEYS, clearFit, SHAPES, STICKY_SIZE, center, framesInOrder, frameOf, inside, linePath, nearLine, newID, overlaps, paper, rectOf, textOf, union, withData,
  type Pt, type Rect,
} from '../lib/board'
import { boardPNG } from '../lib/exportImage'
import { uploadPicture } from '../lib/upload'
import { ObjectView, VoteBadge, tagColour, type EditResult } from '../components/board/ObjectView'
import { EmojiGrid, FindBar, FloatLayer, FormatBar, REACTIONS, STICKERS, TEXT_SIZES, TagPicker, type Float } from '../components/board/Extras'
import { CommentsPanel, NewThread, ThreadCard, pinAt, threadsOf, type MentionPerson } from '../components/board/Comments'
import { HistoryPanel } from '../components/board/History'
import { AIPanel, ChatPicker, Countdown, FramesPanel, SharePanel, TimerPanel, VotePanel, chime } from '../components/board/Panels'
import { Icon } from '../components/icons'
import { AskButton } from '../components/AskButton'
import { Avatar, Confirm, ContextMenu, Loading, type MenuItem, MenuButton, Modal, Segmented, copyText, errMsg, useIsMobile, useToast } from '../components/ui'

type Tool = 'select' | 'hand' | 'sticky' | 'text' | 'shape' | 'line' | 'pen' | 'eraser' | 'frame' | 'comment' | 'sticker'
interface Cam { x: number; y: number; z: number }
type State = [string, Obj | null][]

/** How the page talks to the server: as a person, or as a guest on the share link. */
interface Conn {
  guest: boolean
  base: string
  headers: Record<string, string>
  stream: (client: string) => string
  uploadURL: string
  imgSrc: (o: Obj) => string
}

const CLIENT = newID()
const MIN_Z = 0.05, MAX_Z = 4
const GRID = 20

async function call<T>(conn: Conn, method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(conn.base + path, { method, credentials: 'same-origin', headers: { ...conn.headers, ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}) }, body: body !== undefined ? JSON.stringify(body) : undefined })
  if (!res.ok) {
    const j = await res.json().catch(() => ({}))
    throw new ApiError(res.status, j.error || res.statusText, j)
  }
  return res.status === 204 ? (undefined as T) : res.json()
}

const clamp = (v: number, a: number, b: number) => Math.max(a, Math.min(b, v))
const isEditable = (t: EventTarget | null) => !!(t as HTMLElement | null)?.closest?.('input, textarea, select, [contenteditable="true"], [contenteditable="plaintext-only"]')
const slim = (o: Obj): Obj => ({ id: o.id, kind: o.kind, x: Math.round(o.x * 10) / 10, y: Math.round(o.y * 10) / 10, w: Math.round(o.w * 10) / 10, h: Math.round(o.h * 10) / 10, z: o.z, data: o.data })

// ---------------------------------------------------------------- pages

export default function BoardPage() {
  const { id } = useParams()
  const [doc, setDoc] = useState<BoardDoc | null>(null)
  const [err, setErr] = useState('')
  const base = `${V1}/boards/${id}`
  const conn = useMemo<Conn>(() => ({
    guest: false, base, headers: { 'X-Client': CLIENT }, stream: c => `${base}/stream?client=${c}`, uploadURL: `${V1}/uploads?board=${id}`, imgSrc: o => o.data.src || '',
  }), [base, id])
  const load = useCallback(async () => { const d = await api.get<BoardDoc>(base); setDoc(d); return d }, [base])
  useEffect(() => { setDoc(null); load().catch(e => setErr(e.status === 404 ? 'This board does not exist, or you are not on it.' : errMsg(e))) }, [load])
  useEffect(() => { if (doc) document.title = `${doc.board.title} · Pocket Whiteboard` }, [doc?.board.title]) // eslint-disable-line react-hooks/exhaustive-deps
  if (err) return <BoardError text={err} />
  if (!doc) return <div className="boot"><img src="/favicon.svg" alt="" /><Loading label="Opening the board…" /></div>
  return <DataProvider><Editor key={doc.board.id} initial={doc} conn={conn} reload={load} /></DataProvider>
}

export function GuestBoardPage() {
  const { key } = useParams()
  const [name, setName] = useState(() => localStorage.getItem('pwb-guest-name') || '')
  const [ready, setReady] = useState(!!localStorage.getItem('pwb-guest-name'))
  const gid = useMemo(() => { let g = localStorage.getItem('pwb-guest-id'); if (!g) { g = newID() + newID(); localStorage.setItem('pwb-guest-id', g) } return g }, [])
  const [doc, setDoc] = useState<BoardDoc | null>(null)
  const [err, setErr] = useState('')
  const base = `${V1}/public/b/${key}`
  const conn = useMemo<Conn>(() => ({
    guest: true, base, headers: { 'X-Guest-Id': gid, 'X-Guest-Name': encodeURIComponent(name) },
    stream: c => `${base}/stream?client=${c}&guest=${gid}&name=${encodeURIComponent(name)}`, uploadURL: `${base}/uploads`,
    imgSrc: o => o.data.upload ? `${base}/uploads/${o.data.upload}` : o.data.src || '',
  }), [base, gid, name])
  const load = useCallback(async () => { const d = await call<BoardDoc>(conn, 'GET', ''); setDoc(d); return d }, [conn])
  useEffect(() => { if (ready) load().catch(e => setErr(e.status === 404 ? 'This link does not work any more. Ask for a new one.' : errMsg(e))) }, [ready, load])
  if (err) return <BoardError text={err} guest />
  if (!ready) {
    return (
      <div className="wb-guest-gate">
        <form className="card wb-guest-card" onSubmit={e => { e.preventDefault(); if (name.trim()) { localStorage.setItem('pwb-guest-name', name.trim()); setName(name.trim()); setReady(true) } }}>
          <img src="/favicon.svg" alt="" width={56} height={56} />
          <h1>Join the board</h1>
          <p className="muted">Type your name so the others know who is pointing and writing. No account needed.</p>
          <input autoFocus value={name} onChange={e => setName(e.target.value)} placeholder="Your name" maxLength={40} aria-label="Your name" />
          <button type="submit" className="btn primary lg" disabled={!name.trim()}>Open the board</button>
        </form>
      </div>
    )
  }
  if (!doc) return <div className="boot"><img src="/favicon.svg" alt="" /><Loading label="Opening the board…" /></div>
  return <Editor initial={doc} conn={conn} reload={load} />
}

function BoardError({ text, guest }: { text: string; guest?: boolean }) {
  const navigate = useNavigate()
  return (
    <div className="wb-guest-gate">
      <div className="card wb-guest-card">
        <img src="/favicon.svg" alt="" width={56} height={56} />
        <h1>Can't open the board</h1>
        <p className="muted">{text}</p>
        {!guest && <button type="button" className="btn primary" onClick={() => navigate('/')}>All boards</button>}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------- the editor

type Drag =
  | { mode: 'pan'; sx: number; sy: number; cam: Cam; moved: boolean }
  | { mode: 'marquee'; start: Pt; add: boolean; base: Set<string> }
  | { mode: 'move'; start: Pt; ids: string[]; orig: Map<string, Obj>; before: State; moved: boolean; hit: string; wasSel: boolean; shift: boolean; sx: number; sy: number }
  | { mode: 'resize'; handle: string; start: Pt; box: Rect; orig: Map<string, Obj>; before: State }
  | { mode: 'create'; id: string; start: Pt; before: State }
  | { mode: 'line'; id: string; before: State }
  | { mode: 'endpoint'; id: string; end: 'a' | 'b'; before: State }
  | { mode: 'pen'; id: string; origin: Pt; pts: number[][]; before: State }
  | { mode: 'erase'; before: State }
  | { mode: 'pinch'; d0: number; m0: Pt; cam: Cam }
  | { mode: 'rotate'; id: string; c: Pt; a0: number; r0: number; before: State }
  | { mode: 'plus'; dir: string; sx: number; sy: number }

interface Cursor { x: number; y: number; name: string; colour: string; sel: string[]; at: number; view?: View; voter?: string }
const CONNECTABLE = new Set(['sticky', 'shape', 'text', 'image', 'sticker'])

function Editor({ initial, conn, reload }: { initial: BoardDoc; conn: Conn; reload: () => Promise<BoardDoc> }) {
  const { toast } = useToast()
  const { settings } = useApp()
  const appState = useAppState()
  const mobile = useIsMobile()
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const me = initial.me
  const canEdit = me.can_edit
  const guest = conn.guest

  const [board, setBoard] = useState<Board>(initial.board)
  const [members, setMembers] = useState<Member[]>(initial.members || [])
  const [starred, setStarred] = useState(!!initial.starred)
  const [shareURL, setShareURL] = useState(initial.share_url || '')
  const [people, setPeople] = useState<Person[]>(initial.people || [])
  const [votes, setVotes] = useState<Votes>(initial.votes)
  const [live, setLive] = useState(false)
  const [saving, setSaving] = useState<'idle' | 'error'>('idle')

  // ---- objects, camera, selection
  const [objs, setObjsState] = useState(() => new Map(initial.objects.map(o => [o.id, o])))
  const objsRef = useRef(objs)
  const setObjs = (fn: (m: Map<string, Obj>) => Map<string, Obj>) => { objsRef.current = fn(objsRef.current); setObjsState(objsRef.current) }
  const [cam, setCamState] = useState<Cam>({ x: 0, y: 0, z: 1 })
  const camRef = useRef(cam)
  const setCam = (c: Cam) => { camRef.current = c; setCamState(c) }
  const [sel, setSelState] = useState<Set<string>>(new Set())
  const selRef = useRef(sel)
  const setSel = (s: Set<string>) => { selRef.current = s; setSelState(s); sendCursorSoon() }
  const [editing, setEditingState] = useState<string | null>(null)
  const editingRef = useRef<string | null>(null)
  const setEditing = (id: string | null) => { editingRef.current = id; setEditingState(id) }

  const [tool, setToolState] = useState<Tool>('select')
  const toolRef = useRef<Tool>('select')
  const setTool = (t: Tool) => { toolRef.current = t; setToolState(t); if (t !== 'select') setEditing(null) }
  const [shapeForm, setShapeForm] = useState<ShapeForm>('rect')
  const [lineCurve, setLineCurve] = useState<'straight' | 'elbow' | 'curve'>('curve')
  const [pen, setPen] = useState({ color: '#1d2024', width: 4, highlight: false })
  const [stickyColor, setStickyColor] = useState(settings?.sticky_color || 'light-yellow')
  const [flyout, setFlyout] = useState<Tool | null>(null)

  const [marquee, setMarquee] = useState<Rect | null>(null)
  const [guides, setGuides] = useState<{ x?: number; y?: number }[]>([])
  const [hoverTarget, setHoverTarget] = useState<string | null>(null)
  const [cursors, setCursors] = useState<Map<string, Cursor>>(new Map())
  const [panel, setPanel] = useState<'share' | 'vote' | 'timer' | 'frames' | 'ai' | 'chat' | 'comments' | 'history' | null>(null)
  // working together
  const [reactions, setReactions] = useState<Record<string, Reaction[]>>(initial.reactions || {})
  const [privCounts, setPrivCounts] = useState<PrivateCount[]>(initial.private_counts || [])
  const [comments, setComments] = useState<Comment[]>([])
  const [mentionPeople, setMentionPeople] = useState<MentionPerson[]>([])
  const [openThread, setOpenThread] = useState<number | null>(null)
  const [newThread, setNewThread] = useState<{ object_id: string; x: number; y: number } | null>(null)
  const [showResolved, setShowResolved] = useState(false)
  const [historyKey, setHistoryKey] = useState(0)
  const [hoverId, setHoverId] = useState<string | null>(null)
  const [find, setFind] = useState<{ q: string; i: number } | null>(null)
  const [floats, setFloats] = useState<Float[]>([])
  const [following, setFollowing] = useState<string | null>(null)
  const followRef = useRef<string | null>(null)
  followRef.current = following
  const [sticker, setSticker] = useState('⭐')
  const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number; p: Pt; on: string | null } | null>(null)
  const pressT = useRef(0)
  const [present, setPresent] = useState<number | null>(null)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [renaming, setRenaming] = useState(false)
  const [spaceDown, setSpaceDown] = useState(false)
  const vpRef = useRef<HTMLDivElement>(null)
  const [vpSize, setVpSize] = useState({ w: 1200, h: 800 })

  // ---- talking to the server
  const pending = useRef(new Map<string, Op>())
  const flushT = useRef(0)
  const chain = useRef<Promise<void>>(Promise.resolve())
  const flush = () => {
    flushT.current = 0
    const ops = [...pending.current.values()]
    pending.current.clear()
    if (!ops.length) return
    chain.current = chain.current.then(async () => {
      try {
        await call(conn, 'POST', '/ops', { client: CLIENT, ops: ops.map(op => op.op === 'put' ? { op: 'put', obj: slim(op.obj) } : op) })
        setSaving('idle')
      } catch (e) {
        setSaving('error')
        toast(`Not saved: ${errMsg(e)}`, 'err')
        resync()
      }
    })
  }
  const queue = (ops: Op[]) => {
    if (!canEdit || !ops.length) return
    for (const op of ops) pending.current.set(op.op === 'put' ? op.obj.id : op.id, op)
    if (!flushT.current) flushT.current = window.setTimeout(flush, 80)
  }
  useEffect(() => {
    const h = (e: BeforeUnloadEvent) => { if (pending.current.size) { flush(); e.preventDefault() } }
    window.addEventListener('beforeunload', h)
    return () => window.removeEventListener('beforeunload', h)
  })

  /** Changes things here and sends them (no undo step). Deleting a thing takes its lines with it. */
  const apply = (puts: Obj[], dels: string[] = []) => {
    const cur = objsRef.current
    const gone = new Set(dels)
    if (gone.size) for (const o of cur.values()) if (o.kind === 'line' && ((o.data.from && gone.has(o.data.from)) || (o.data.to && gone.has(o.data.to)))) gone.add(o.id)
    setObjs(m => {
      const n = new Map(m)
      for (const o of puts) n.set(o.id, o)
      for (const id of gone) n.delete(id)
      return n
    })
    queue([...puts.map(o => ({ op: 'put' as const, obj: o })), ...[...gone].map(id => ({ op: 'del' as const, id }))])
    return gone
  }

  // ---- undo
  const undoStack = useRef<{ before: State; after: State }[]>([])
  const redoStack = useRef<{ before: State; after: State }[]>([])
  const [, setHist] = useState(0)
  const snapshot = (ids: Iterable<string>): State => [...new Set(ids)].map(id => [id, objsRef.current.get(id) || null])
  const record = (before: State) => {
    const after: State = before.map(([id]) => [id, objsRef.current.get(id) || null])
    if (after.every(([, o], i) => o === before[i][1])) return
    undoStack.current.push({ before, after })
    if (undoStack.current.length > 200) undoStack.current.shift()
    redoStack.current = []
    setHist(h => h + 1)
  }
  const commit = (puts: Obj[], dels: string[] = []) => {
    const cur = objsRef.current
    const lines: string[] = []
    if (dels.length) { const d = new Set(dels); for (const o of cur.values()) if (o.kind === 'line' && ((o.data.from && d.has(o.data.from)) || (o.data.to && d.has(o.data.to)))) lines.push(o.id) }
    const before = snapshot([...puts.map(o => o.id), ...dels, ...lines])
    apply(puts, dels)
    record(before)
  }
  const restore = (st: State) => {
    const puts = st.filter(([, o]) => o).map(([, o]) => o!)
    const dels = st.filter(([id, o]) => !o && objsRef.current.has(id)).map(([id]) => id)
    apply(puts, dels)
    const s = new Set([...selRef.current].filter(id => objsRef.current.has(id)))
    setSel(s)
  }
  const undo = () => { const e = undoStack.current.pop(); if (!e) return; restore(e.before); redoStack.current.push(e); setHist(h => h + 1) }
  const redo = () => { const e = redoStack.current.pop(); if (!e) return; restore(e.after); undoStack.current.push(e); setHist(h => h + 1) }

  const resync = async () => {
    try {
      const d = await reload()
      const keep = new Map([...pending.current.values()].filter(o => o.op === 'put').map(o => [(o as { obj: Obj }).obj.id, (o as { obj: Obj }).obj]))
      setObjs(() => { const n = new Map(d.objects.map(o => [o.id, o])); for (const [id, o] of keep) n.set(id, o); return n })
      setBoard(d.board); setVotes(d.votes); setPeople(d.people)
      setReactions(d.reactions || {}); setPrivCounts(d.private_counts || [])
      if (d.members) setMembers(d.members)
    } catch { /* the stream reconnects and tries again */ }
  }

  // ---- live events
  const dragRef = useRef<Drag | null>(null)
  const activeIds = () => {
    const d = dragRef.current
    const s = new Set<string>()
    if (editingRef.current) s.add(editingRef.current)
    if (d && 'ids' in d) d.ids.forEach(i => s.add(i))
    if (d && (d.mode === 'resize')) d.orig.forEach((_, i) => s.add(i))
    if (d && 'id' in d) s.add(d.id)
    return s
  }
  const votesT = useRef(0)
  const loadVotes = () => {
    window.clearTimeout(votesT.current)
    votesT.current = window.setTimeout(() => call<{ votes: Votes }>(conn, 'GET', '/votes').then(r => setVotes(r.votes)).catch(() => {}), 150)
  }
  const handleEvent = useRef<(ev: Record<string, unknown>) => void>(() => {})
  handleEvent.current = ev => {
    switch (ev.type) {
      case 'ops': {
        const ops = ev.ops as Op[]
        const skip = activeIds()
        setObjs(m => {
          const n = new Map(m)
          for (const op of ops) {
            if (op.op === 'put') {
              if (skip.has(op.obj.id)) continue
              const old = n.get(op.obj.id)
              n.set(op.obj.id, { ...op.obj, author: old?.author || op.obj.author || (ev.by as string) })
            } else n.delete(op.id)
          }
          return n
        })
        if (ops.some(o => o.op === 'del' && selRef.current.has(o.id))) setSel(new Set([...selRef.current].filter(id => objsRef.current.has(id))))
        if (editingRef.current && !objsRef.current.has(editingRef.current)) setEditing(null)
        break
      }
      case 'cursor': {
        const c = ev as unknown as Cursor & { client: string; gone?: boolean }
        setCursors(m => { const n = new Map(m); if (c.gone) n.delete(c.client); else n.set(c.client, { x: c.x, y: c.y, name: c.name, colour: c.colour, sel: c.sel || [], at: Date.now(), view: c.view || m.get(c.client)?.view, voter: c.voter }); return n })
        if (c.view && followRef.current === c.client) setCam(camFor(c.view, 0, 8))
        break
      }
      case 'gone': setCursors(m => { const n = new Map(m); n.delete(ev.client as string); return n }); break
      case 'presence': setPeople(ev.people as Person[]); break
      case 'board': setBoard(b => ({ ...b, ...(ev.board as Board), share_key: b.share_key, role: b.role })); break
      case 'votes': loadVotes(); break
      case 'deleted': toast('This board was deleted'); if (!guest) navigate('/'); break
      case 'reactions': setReactions(r => ({ ...r, [ev.object_id as string]: ev.reactions as Reaction[] })); break
      case 'private_counts': setPrivCounts(ev.counts as PrivateCount[]); break
      case 'comments': loadComments(); break
      case 'reload':
        resync()
        setHistoryKey(k => k + 1)
        if (ev.why === 'revealed') toast(`${ev.by} revealed the private notes`)
        else if (ev.why === 'restored') toast(`${ev.by} took the board back to an earlier version`)
        break
      case 'float': addFloat(ev.emoji as string, ev.name as string, ev.colour as string); break
      case 'summon': {
        const v = ev.view as View
        animateTo(camFor(v, 0, 8))
        setFollowing(ev.client as string)
        toast(`${ev.by} brought everyone here — you follow their view until you move`)
        break
      }
    }
  }
  useEffect(() => {
    let es: EventSource | null = null, stopped = false, retry = 1000, first = true, t = 0
    const open = () => {
      if (stopped) return
      es = new EventSource(conn.stream(CLIENT), { withCredentials: true })
      es.addEventListener('hello', e => {
        const h = JSON.parse((e as MessageEvent).data)
        setPeople(h.people)
        if (!first) resync()
        first = false; retry = 1000; setLive(true)
      })
      es.onmessage = e => { try { handleEvent.current(JSON.parse(e.data)) } catch { /* ignore */ } }
      es.onerror = () => { setLive(false); es?.close(); t = window.setTimeout(open, retry); retry = Math.min(retry * 2, 15000) }
    }
    open()
    return () => { stopped = true; es?.close(); window.clearTimeout(t) }
  }, [conn]) // eslint-disable-line react-hooks/exhaustive-deps
  // once the board font has loaded, fit every note's text again with it
  useEffect(() => { document.fonts?.ready.then(() => { clearFit(); setObjsState(m => new Map(m)) }) }, [])

  // ---- comments, reactions, floating emoji, following
  const req = useCallback(<T,>(method: string, path: string, body?: unknown) => call<T>(conn, method, path, body), [conn])
  const loadComments = () => {
    req<{ comments: Comment[]; people: MentionPerson[] }>('GET', '/comments').then(r => { setComments(r.comments); if (r.people?.length) setMentionPeople(r.people) }).catch(() => {})
  }
  useEffect(() => {
    loadComments()
    const c = Number(params.get('c'))
    if (c) { setOpenThread(c); setPanel(null) }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps
  const threads = useMemo(() => threadsOf(comments), [comments])
  const floatN = useRef(0)
  function addFloat(emoji: string, name: string, colour: string) {
    const id = ++floatN.current
    setFloats(f => [...f.slice(-24), { id, emoji, name, colour, x: 8 + Math.random() * 30 }])
    window.setTimeout(() => setFloats(f => f.filter(x => x.id !== id)), 2600)
  }
  const lastFloat = useRef(0)
  const sendFloat = (emoji: string) => {
    if (Date.now() - lastFloat.current < 250) return
    lastFloat.current = Date.now()
    addFloat(emoji, me.name, me.colour)
    req('POST', '/float', { client: CLIENT, emoji }).catch(() => {})
  }
  const react = async (id: string, emoji: string) => {
    try { const r = await req<{ object_id: string; reactions: Reaction[] }>('POST', '/react', { client: CLIENT, object_id: id, emoji }); setReactions(x => ({ ...x, [r.object_id]: r.reactions })) } catch (e) { toast(errMsg(e), 'err') }
  }
  const summon = async () => {
    try { await req('POST', '/summon', { client: CLIENT, view: myView() }); toast('Everyone on the board now sees what you see') } catch (e) { toast(errMsg(e), 'err') }
  }
  const stopFollowing = () => { if (followRef.current) setFollowing(null) }
  // tell the others what this screen shows when it moves (for following)
  const viewT = useRef(0)
  useEffect(() => {
    window.clearTimeout(viewT.current)
    viewT.current = window.setTimeout(() => sendCursor(), 180)
  }, [cam]) // eslint-disable-line react-hooks/exhaustive-deps
  const togglePrivate = async (on: boolean) => {
    try { const r = await req<{ board: Board; revealed: number }>('PUT', '/private', { client: CLIENT, on }); setBoard(b => ({ ...b, ...r.board })); if (!on) { resync(); toast(r.revealed ? `${r.revealed} private ${r.revealed === 1 ? 'note' : 'notes'} revealed` : 'Private mode is off') } } catch (e) { toast(errMsg(e), 'err') }
  }
  // cursors that went quiet fade out
  useEffect(() => {
    const t = window.setInterval(() => setCursors(m => { const now = Date.now(); let changed = false; const n = new Map(m); for (const [k, c] of n) if (now - c.at > 20000) { n.delete(k); changed = true } return changed ? n : m }), 5000)
    return () => window.clearInterval(t)
  }, [])

  // my pointer for the others
  const lastPointer = useRef<Pt>({ x: 0, y: 0 })
  const cursorT = useRef(0)
  const cursorLast = useRef(0)
  const sendCursor = (gone = false) => {
    cursorLast.current = Date.now()
    call(conn, 'POST', '/cursor', { client: CLIENT, x: Math.round(lastPointer.current.x), y: Math.round(lastPointer.current.y), sel: [...selRef.current].slice(0, 50), gone, view: myView() }).catch(() => {})
  }
  /** What part of the board this screen shows, for anyone following. */
  function myView(): View {
    const c = camRef.current, el = vpRef.current
    const w = el?.clientWidth || 1200, h = el?.clientHeight || 800
    return { x: Math.round(-c.x / c.z), y: Math.round(-c.y / c.z), w: Math.round(w / c.z), h: Math.round(h / c.z) }
  }
  function sendCursorSoon() {
    if (cursorT.current) return
    const wait = Math.max(0, 60 - (Date.now() - cursorLast.current))
    cursorT.current = window.setTimeout(() => { cursorT.current = 0; sendCursor() }, wait)
  }

  // ---- camera
  const toWorld = (sx: number, sy: number): Pt => {
    const r = vpRef.current!.getBoundingClientRect()
    const c = camRef.current
    return { x: (sx - r.left - c.x) / c.z, y: (sy - r.top - c.y) / c.z }
  }
  const anim = useRef(0)
  const animateTo = (to: Cam, ms = 280) => {
    cancelAnimationFrame(anim.current)
    const from = camRef.current, t0 = performance.now()
    const step = (t: number) => {
      const k = Math.min(1, (t - t0) / ms), e = 1 - Math.pow(1 - k, 3)
      setCam({ x: from.x + (to.x - from.x) * e, y: from.y + (to.y - from.y) * e, z: from.z + (to.z - from.z) * e })
      if (k < 1) anim.current = requestAnimationFrame(step)
    }
    anim.current = requestAnimationFrame(step)
  }
  const camFor = (r: Rect, pad = 80, maxZ = 1): Cam => {
    // the tools on the left take room: centre in what is left (not when presenting)
    const left = present === null ? (mobile ? 48 : 64) : 0
    const vw = (vpRef.current?.clientWidth || vpSize.w) - left, vh = vpRef.current?.clientHeight || vpSize.h
    const z = clamp(Math.min((vw - pad * 2) / Math.max(r.w, 1), (vh - pad * 2) / Math.max(r.h, 1)), MIN_Z, maxZ)
    return { z, x: left + vw / 2 - (r.x + r.w / 2) * z, y: vh / 2 - (r.y + r.h / 2) * z }
  }
  const fitAll = (animate = true) => {
    const r = union([...objsRef.current.values()].map(o => rectOf(o, objsRef.current)))
    const c = r ? camFor(r, mobile ? 30 : 80) : { x: (vpRef.current?.clientWidth || 0) / 2, y: (vpRef.current?.clientHeight || 0) / 2, z: 1 }
    if (animate) animateTo(c); else setCam(c)
  }
  const focusOn = (id: string, select = true) => {
    const o = objsRef.current.get(id)
    if (!o) return
    const r = rectOf(o, objsRef.current)
    animateTo(camFor(r, o.kind === 'frame' ? 40 : 160, o.kind === 'frame' ? 2 : 1.2))
    if (select) setSel(new Set([id]))
  }
  const zoomAt = (sx: number, sy: number, factor: number) => {
    const c = camRef.current
    const r = vpRef.current!.getBoundingClientRect()
    const px = sx - r.left, py = sy - r.top
    const z = clamp(c.z * factor, MIN_Z, MAX_Z)
    const k = z / c.z
    setCam({ z, x: px - (px - c.x) * k, y: py - (py - c.y) * k })
  }
  const zoomBy = (f: number) => { const r = vpRef.current!.getBoundingClientRect(); zoomAt(r.left + r.width / 2, r.top + r.height / 2, f) }
  const viewCenter = (): Pt => { const r = vpRef.current!.getBoundingClientRect(); return toWorld(r.left + r.width / 2, r.top + r.height / 2) }

  useLayoutEffect(() => {
    const el = vpRef.current
    if (!el) return
    const ro = new ResizeObserver(() => setVpSize({ w: el.clientWidth, h: el.clientHeight }))
    ro.observe(el)
    const o = params.get('o')
    if (o && objsRef.current.has(o)) { setCam(camFor(rectOf(objsRef.current.get(o)!, objsRef.current), 160, 1.2)); setSel(new Set([o])) } else fitAll(false)
    return () => ro.disconnect()
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // wheel: move the board; pinch / Ctrl zooms (non-passive, so the page does not zoom)
  useEffect(() => {
    const el = vpRef.current
    if (!el) return
    const h = (e: WheelEvent) => {
      if ((e.target as HTMLElement).closest('.wb-panel, .wb-ctx, .wb-tools, .wb-zoom')) return
      e.preventDefault()
      cancelAnimationFrame(anim.current)
      stopFollowing()
      if (e.ctrlKey || e.metaKey || settings?.wheel_zoom) {
        const dy = e.deltaMode === 1 ? e.deltaY * 16 : e.deltaY
        zoomAt(e.clientX, e.clientY, Math.exp(-dy * (e.ctrlKey ? 0.01 : 0.0018)))
      } else {
        const c = camRef.current
        const k = e.deltaMode === 1 ? 16 : 1
        setCam({ ...c, x: c.x - (e.shiftKey ? e.deltaY : e.deltaX) * k, y: c.y - (e.shiftKey ? 0 : e.deltaY) * k })
      }
    }
    el.addEventListener('wheel', h, { passive: false })
    return () => el.removeEventListener('wheel', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  // ---- making things
  const zs = () => { let hi = 0, lo = 0; for (const o of objsRef.current.values()) { if (o.z > hi) hi = o.z; if (o.z < lo) lo = o.z } return { hi, lo } }
  const make = (kind: Obj['kind'], r: Rect, data: ObjData): Obj => ({ id: newID(), kind, x: r.x, y: r.y, w: r.w, h: r.h, z: kind === 'frame' ? zs().lo - 1 : zs().hi + 1, data, author: me.name })
  const addSticky = (p: Pt, opts: { colour?: string; text?: string; edit?: boolean; size?: number } = {}) => {
    const s = opts.size || STICKY_SIZE
    const o = make('sticky', { x: p.x - s / 2, y: p.y - s / 2, w: s, h: s }, { text: opts.text || '', color: opts.colour || stickyColor })
    commit([o])
    setSel(new Set([o.id]))
    if (opts.edit !== false) setEditing(o.id)
    return o
  }
  const snapV = (v: number) => (settings?.snap ? Math.round(v / GRID) * GRID : v)

  // ---- pointer handling
  const pointers = useRef(new Map<number, Pt>())
  const lastTap = useRef<{ t: number; x: number; y: number; id: string | null }>({ t: 0, x: 0, y: 0, id: null })

  const hitAt = (e: { target: EventTarget | null }): { id: string | null; part?: string; handle?: string; end?: string; plus?: string; rot?: boolean; dot?: string; pin?: number; react?: string; link?: string } => {
    const t = e.target as HTMLElement
    if (t.closest('[data-rot]')) return { id: null, rot: true }
    const pin = t.closest('[data-pin]') as HTMLElement | null
    if (pin) return { id: null, pin: Number(pin.dataset.pin) }
    const rc = t.closest('[data-react]') as HTMLElement | null
    if (rc) return { id: rc.dataset.obj || null, react: rc.dataset.react }
    const dot = t.closest('[data-dot]') as HTMLElement | null
    if (dot) return { id: dot.dataset.obj || null, dot: dot.dataset.dot }
    const h = t.closest('[data-handle]') as HTMLElement | null
    if (h) return { id: null, handle: h.dataset.handle }
    const end = t.closest('[data-end]') as HTMLElement | null
    if (end) return { id: end.dataset.line || null, end: end.dataset.end }
    const plus = t.closest('[data-plus]') as HTMLElement | null
    if (plus) return { id: null, plus: plus.dataset.plus }
    const el = t.closest('[data-id]') as HTMLElement | null
    const a = t.closest('a[href]') as HTMLAnchorElement | null
    return { id: el?.dataset.id || null, part: el?.dataset.part, link: a?.href }
  }

  /** A selection grows to whole groups (Alt picks just the one thing). */
  const withGroups = (ids: Iterable<string>): Set<string> => {
    const out = new Set(ids)
    const groups = new Set([...out].map(id => objsRef.current.get(id)?.data.group).filter(Boolean) as string[])
    if (groups.size) for (const o of objsRef.current.values()) if (o.data.group && groups.has(o.data.group)) out.add(o.id)
    return out
  }

  /** What a line end would join to here (not lines, not the line itself). */
  const targetAt = (p: Pt, not?: string): string | null => {
    const list = [...objsRef.current.values()].filter(o => o.kind !== 'line' && o.kind !== 'pen' && o.id !== not).sort((a, b) => b.z - a.z)
    for (const o of list) {
      if (o.kind === 'frame') { if (inside(p, o) && !inside(p, { x: o.x + 30, y: o.y + 30, w: o.w - 60, h: o.h - 60 })) return o.id; continue }
      if (inside(p, o, 4 / camRef.current.z)) return o.id
    }
    return null
  }

  const movingSet = (ids: string[]): string[] => {
    const all = objsRef.current
    const out = new Set(ids.filter(id => !all.get(id)?.data.locked))
    for (const id of [...out]) {
      const f = all.get(id)
      if (f?.kind !== 'frame') continue
      for (const o of all.values()) {
        if (o.id === f.id || o.data.locked || out.has(o.id)) continue
        if (o.kind === 'line' && (o.data.from || o.data.to)) continue
        if (o.kind === 'frame' ? (o.x >= f.x && o.y >= f.y && o.x + o.w <= f.x + f.w && o.y + o.h <= f.y + f.h) : inside(center(rectOf(o, all)), f)) out.add(o.id)
      }
    }
    return [...out]
  }

  const moved = (o: Obj, dx: number, dy: number): Obj => {
    if (o.kind === 'line') {
      const d = o.data
      return { ...o, x: o.x + dx, y: o.y + dy, data: { ...d, x1: (d.x1 ?? o.x) + (d.from ? 0 : dx), y1: (d.y1 ?? o.y) + (d.from ? 0 : dy), x2: (d.x2 ?? o.x + o.w) + (d.to ? 0 : dx), y2: (d.y2 ?? o.y + o.h) + (d.to ? 0 : dy) } }
    }
    return { ...o, x: o.x + dx, y: o.y + dy }
  }

  /** Lines up a moving box with the things around it (smart guides). */
  const guideSnap = (box: Rect, skip: Set<string>): { dx: number; dy: number; g: { x?: number; y?: number }[] } => {
    const tol = 6 / camRef.current.z
    const all = objsRef.current
    if (all.size > 1500) return { dx: 0, dy: 0, g: [] }
    const xs = [box.x, box.x + box.w / 2, box.x + box.w], ys = [box.y, box.y + box.h / 2, box.y + box.h]
    let bx: { d: number; v: number } | null = null, by: { d: number; v: number } | null = null
    for (const o of all.values()) {
      if (skip.has(o.id) || o.kind === 'line' || o.kind === 'pen') continue
      const r = rectOf(o)
      for (const v of [r.x, r.x + r.w / 2, r.x + r.w]) for (const x of xs) { const d = v - x; if (Math.abs(d) < tol && (!bx || Math.abs(d) < Math.abs(bx.d))) bx = { d, v } }
      for (const v of [r.y, r.y + r.h / 2, r.y + r.h]) for (const y of ys) { const d = v - y; if (Math.abs(d) < tol && (!by || Math.abs(d) < Math.abs(by.d))) by = { d, v } }
    }
    const g: { x?: number; y?: number }[] = []
    if (bx) g.push({ x: bx.v })
    if (by) g.push({ y: by.v })
    return { dx: bx?.d || 0, dy: by?.d || 0, g }
  }

  const onPointerDown = (e: React.PointerEvent) => {
    if ((e.target as HTMLElement).closest('.wb-ctx, .wb-tools, .wb-zoom, .wb-panel, .wb-vote-bar, .ob-edit')) return
    if (e.button === 2) return
    // keep the browser from moving focus (it would close a note just opened for writing)
    e.preventDefault()
    vpRef.current?.setPointerCapture(e.pointerId)
    cancelAnimationFrame(anim.current)
    setFlyout(null)
    stopFollowing()
    const p = toWorld(e.clientX, e.clientY)
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY })
    if (pointers.current.size === 2) {
      // two fingers: zoom and move
      const [a, b] = [...pointers.current.values()]
      finishDrag(true)
      dragRef.current = { mode: 'pinch', d0: Math.hypot(a.x - b.x, a.y - b.y), m0: { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 }, cam: camRef.current }
      return
    }
    if (editingRef.current) { (document.activeElement as HTMLElement | null)?.blur?.() }
    const t = toolRef.current
    const hit = hitAt(e)
    const touch = e.pointerType === 'touch'
    setCtxMenu(null)
    window.clearTimeout(pressT.current)
    if (touch) {
      const sx = e.clientX, sy = e.clientY, target = e.target
      pressT.current = window.setTimeout(() => {
        const d = dragRef.current
        if (pointers.current.size !== 1 || (d && 'moved' in d && d.moved)) return
        finishDrag(true)
        openMenuAt(sx, sy, target)
      }, 550)
    }
    if (e.button === 1 || t === 'hand' || spaceDown) {
      dragRef.current = { mode: 'pan', sx: e.clientX, sy: e.clientY, cam: camRef.current, moved: false }
      return
    }
    const all = objsRef.current
    if (hit.pin) { setOpenThread(hit.pin); setNewThread(null); return }
    if (hit.react && hit.id) { react(hit.id, hit.react); return }
    if (t === 'comment') {
      const o = hit.id ? all.get(hit.id) : undefined
      const on = o && o.kind !== 'line' && o.kind !== 'pen' ? o.id : ''
      setOpenThread(null)
      setNewThread({ object_id: on, x: Math.round(on ? o!.x + o!.w : p.x), y: Math.round(on ? o!.y : p.y) })
      setTool('select')
      return
    }
    if (t === 'select' || !canEdit) {
      // turning, joining dots, resize handles and line ends first
      if (hit.rot && canEdit && selRef.current.size === 1) {
        const [id] = [...selRef.current]
        const o = all.get(id)
        if (o) { const c = center(o); dragRef.current = { mode: 'rotate', id, c, a0: Math.atan2(p.y - c.y, p.x - c.x), r0: o.data.rot || 0, before: snapshot([id]) } }
        return
      }
      if (hit.dot && hit.id && canEdit) { connectFrom(hit.id, hit.dot); return }
      if (hit.handle && canEdit) {
        const ids = [...selRef.current].filter(id => !all.get(id)?.data.locked)
        const box = union(ids.map(id => rectOf(all.get(id)!, all)))
        if (box) dragRef.current = { mode: 'resize', handle: hit.handle, start: p, box, orig: new Map(ids.map(id => [id, all.get(id)!])), before: snapshot(ids) }
        return
      }
      if (hit.end && hit.id && canEdit) { dragRef.current = { mode: 'endpoint', id: hit.id, end: hit.end as 'a' | 'b', before: snapshot([hit.id]) }; return }
      if (hit.plus && canEdit) { dragRef.current = { mode: 'plus', dir: hit.plus, sx: e.clientX, sy: e.clientY }; return }
      if (hit.id && all.has(hit.id)) {
        const o = all.get(hit.id)!
        if (votes.open && !e.shiftKey && o.kind !== 'frame' && o.kind !== 'line' && o.kind !== 'pen') {
          dragRef.current = { mode: 'move', start: p, ids: [], orig: new Map(), before: [], moved: false, hit: hit.id, wasSel: false, shift: false, sx: e.clientX, sy: e.clientY }
          return
        }
        const wasSel = selRef.current.has(hit.id)
        let s = selRef.current
        const grow = (ids: Iterable<string>) => e.altKey ? new Set(ids) : withGroups(ids)
        if (e.shiftKey || e.metaKey) { s = new Set(s); if (wasSel) { for (const id of grow([hit.id])) s.delete(id) } else { for (const id of grow([hit.id])) s.add(id) } setSel(s) }
        else if (!wasSel) { s = grow([hit.id]); setSel(s) }
        linkRef.current = wasSel ? hit.link || null : null
        if (!canEdit) return
        const ids = movingSet([...s])
        dragRef.current = { mode: 'move', start: p, ids, orig: new Map(ids.map(id => [id, all.get(id)!])), before: snapshot(ids), moved: false, hit: hit.id, wasSel, shift: e.shiftKey, sx: e.clientX, sy: e.clientY }
        return
      }
      // empty board
      if (touch) { dragRef.current = { mode: 'pan', sx: e.clientX, sy: e.clientY, cam: camRef.current, moved: false }; return }
      const base = e.shiftKey ? new Set(selRef.current) : new Set<string>()
      if (!e.shiftKey) setSel(new Set())
      dragRef.current = { mode: 'marquee', start: p, add: e.shiftKey, base }
      return
    }
    // creating
    switch (t) {
      case 'sticky': addSticky({ x: snapV(p.x), y: snapV(p.y) }); setTool('select'); return
      case 'text': {
        const o = make('text', { x: snapV(p.x), y: snapV(p.y) - 18, w: 320, h: 36 }, { text: '', size: 24 })
        commit([o]); setSel(new Set([o.id])); setEditing(o.id); setTool('select'); return
      }
      case 'shape': case 'frame': {
        const n = [...all.values()].filter(o => o.kind === 'frame').length + 1
        const o = t === 'shape' ? make('shape', { x: snapV(p.x), y: snapV(p.y), w: 0, h: 0 }, { shape: shapeForm, fill: 'white', text: '' }) : make('frame', { x: snapV(p.x), y: snapV(p.y), w: 0, h: 0 }, { title: `Frame ${n}`, fill: 'gray' })
        apply([o])
        dragRef.current = { mode: 'create', id: o.id, start: p, before: [[o.id, null]] }
        return
      }
      case 'line': {
        const from = targetAt(p)
        const o = make('line', { x: p.x, y: p.y, w: 0, h: 0 }, { x1: p.x, y1: p.y, x2: p.x, y2: p.y, from: from || undefined, end: 'arrow', curve: lineCurve })
        apply([o])
        dragRef.current = { mode: 'line', id: o.id, before: [[o.id, null]] }
        return
      }
      case 'pen': {
        const o = make('pen', { x: p.x, y: p.y, w: 0, h: 0 }, { points: [[0, 0]], color: pen.color, width: pen.highlight ? pen.width * 4 : pen.width, highlight: pen.highlight || undefined })
        o.z = zs().hi + 1
        setObjs(m => new Map(m).set(o.id, o))
        dragRef.current = { mode: 'pen', id: o.id, origin: p, pts: [[0, 0]], before: [[o.id, null]] }
        return
      }
      case 'eraser': dragRef.current = { mode: 'erase', before: [] }; eraseAt(p); return
      case 'sticker': {
        const o = make('sticker', { x: snapV(p.x) - 60, y: snapV(p.y) - 60, w: 120, h: 120 }, { emoji: sticker })
        commit([o]); setSel(new Set([o.id])); setTool('select'); return
      }
    }
  }
  const linkRef = useRef<string | null>(null)
  /** Starts an arrow from the middle of one of a thing's sides. */
  function connectFrom(id: string, side: string) {
    const o = objsRef.current.get(id)
    if (!o) return
    const pt = ({ t: { x: o.x + o.w / 2, y: o.y }, u: { x: o.x + o.w / 2, y: o.y }, b: { x: o.x + o.w / 2, y: o.y + o.h }, d: { x: o.x + o.w / 2, y: o.y + o.h }, l: { x: o.x, y: o.y + o.h / 2 }, r: { x: o.x + o.w, y: o.y + o.h / 2 } } as Record<string, Pt>)[side] || center(o)
    const ln = make('line', { x: pt.x, y: pt.y, w: 0, h: 0 }, { x1: pt.x, y1: pt.y, x2: pt.x, y2: pt.y, from: o.id, end: 'arrow', curve: lineCurve })
    apply([ln])
    setHoverId(null); hoverRef.current = null
    dragRef.current = { mode: 'line', id: ln.id, before: [[ln.id, null]] }
  }

  const eraseAt = (p: Pt) => {
    const d = dragRef.current
    if (!d || d.mode !== 'erase') return
    const tol = 8 / camRef.current.z
    const hits: string[] = []
    for (const o of objsRef.current.values()) {
      if (o.kind !== 'pen' || !inside(p, o, tol + (o.data.width || 4))) continue
      const pts = o.data.points || []
      if (pts.some(q => Math.hypot(o.x + q[0] - p.x, o.y + q[1] - p.y) < tol + (o.data.width || 4) / 2)) hits.push(o.id)
    }
    if (!hits.length) return
    d.before.push(...snapshot(hits))
    apply([], hits)
  }

  const onPointerMove = (e: React.PointerEvent) => {
    if (pressT.current && e.pointerType === 'touch') { const d = dragRef.current; if (d && 'sx' in d && Math.hypot(e.clientX - d.sx, e.clientY - d.sy) > 8) { window.clearTimeout(pressT.current); pressT.current = 0 } }
    const p = toWorld(e.clientX, e.clientY)
    lastPointer.current = p
    sendCursorSoon()
    if (pointers.current.has(e.pointerId)) pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY })
    const d = dragRef.current
    if (!d) {
      if (toolRef.current === 'line') setHoverTarget(targetAt(p))
      if (toolRef.current === 'select' && canEdit && e.pointerType !== 'touch') {
        const h = hitAt(e)
        if (!h.dot) {
          const o = h.id ? objsRef.current.get(h.id) : undefined
          const id = o && CONNECTABLE.has(o.kind) && !o.data.locked ? o.id : null
          if (id !== hoverRef.current) { hoverRef.current = id; setHoverId(id) }
        }
      }
      return
    }
    const all = objsRef.current
    switch (d.mode) {
      case 'pinch': {
        const pts = [...pointers.current.values()]
        if (pts.length < 2) return
        const [a, b] = pts
        const dist = Math.hypot(a.x - b.x, a.y - b.y), m = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 }
        const r = vpRef.current!.getBoundingClientRect()
        const z = clamp(d.cam.z * dist / d.d0, MIN_Z, MAX_Z)
        const wx = (d.m0.x - r.left - d.cam.x) / d.cam.z, wy = (d.m0.y - r.top - d.cam.y) / d.cam.z
        setCam({ z, x: m.x - r.left - wx * z, y: m.y - r.top - wy * z })
        return
      }
      case 'pan': {
        if (Math.hypot(e.clientX - d.sx, e.clientY - d.sy) > 3) d.moved = true
        setCam({ ...d.cam, x: d.cam.x + e.clientX - d.sx, y: d.cam.y + e.clientY - d.sy })
        return
      }
      case 'marquee': {
        const r = { x: Math.min(d.start.x, p.x), y: Math.min(d.start.y, p.y), w: Math.abs(p.x - d.start.x), h: Math.abs(p.y - d.start.y) }
        setMarquee(r)
        const s = new Set(d.base)
        for (const o of all.values()) {
          const or = rectOf(o, all)
          const hit = o.kind === 'frame' ? (or.x >= r.x && or.y >= r.y && or.x + or.w <= r.x + r.w && or.y + or.h <= r.y + r.h) : overlaps(r, or)
          if (hit) s.add(o.id)
        }
        setSel(e.altKey ? s : withGroups(s))
        return
      }
      case 'move': {
        if (!d.moved && Math.hypot(e.clientX - d.sx, e.clientY - d.sy) < 4) return
        if (!d.ids.length) return
        d.moved = true
        let dx = p.x - d.start.x, dy = p.y - d.start.y
        const box = union(d.ids.map(id => rectOf(d.orig.get(id)!, all)))!
        if (settings?.snap) { dx = snapV(box.x + dx) - box.x; dy = snapV(box.y + dy) - box.y; setGuides([]) }
        else if (!e.altKey) {
          const g = guideSnap({ ...box, x: box.x + dx, y: box.y + dy }, new Set(d.ids))
          dx += g.dx; dy += g.dy
          setGuides(g.g)
        }
        const puts = d.ids.map(id => moved(d.orig.get(id)!, dx, dy))
        apply(puts)
        return
      }
      case 'resize': {
        const { box, handle } = d
        let x1 = box.x, y1 = box.y, x2 = box.x + box.w, y2 = box.y + box.h
        if (handle.includes('w')) x1 = Math.min(p.x, x2 - 10)
        if (handle.includes('e')) x2 = Math.max(p.x, x1 + 10)
        if (handle.includes('n')) y1 = Math.min(p.y, y2 - 10)
        if (handle.includes('s')) y2 = Math.max(p.y, y1 + 10)
        const only = d.orig.size === 1 ? [...d.orig.values()][0] : null
        const keep = e.shiftKey || d.orig.size > 1 || (only && (only.kind === 'sticky' || only.kind === 'image')) || handle.length === 2 && only?.kind === 'text'
        if (keep && handle.length === 2) {
          const sx = (x2 - x1) / box.w, sy = (y2 - y1) / box.h, s = Math.max(sx, sy)
          const w = box.w * s, h = box.h * s
          if (handle.includes('w')) x1 = x2 - w; else x2 = x1 + w
          if (handle.includes('n')) y1 = y2 - h; else y2 = y1 + h
        }
        const sx = (x2 - x1) / box.w, sy = (y2 - y1) / box.h
        const puts: Obj[] = []
        for (const o of d.orig.values()) {
          const nx = x1 + (o.x - box.x) * sx, ny = y1 + (o.y - box.y) * sy
          if (o.kind === 'line') {
            const dd = o.data
            const fx = (v: number) => x1 + (v - box.x) * sx, fy = (v: number) => y1 + (v - box.y) * sy
            puts.push({ ...o, data: { ...dd, x1: fx(dd.x1 ?? o.x), y1: fy(dd.y1 ?? o.y), x2: fx(dd.x2 ?? o.x + o.w), y2: fy(dd.y2 ?? o.y + o.h) } })
          } else if (o.kind === 'pen') {
            puts.push({ ...o, x: nx, y: ny, w: o.w * sx, h: o.h * sy, data: { ...o.data, points: (o.data.points || []).map(q => [Math.round(q[0] * sx * 10) / 10, Math.round(q[1] * sy * 10) / 10]) } })
          } else if (o.kind === 'text') {
            const ts = handle.length === 2 ? Math.min(sx, sy) : 1
            puts.push({ ...o, x: nx, y: ny, w: Math.max(40, o.w * sx), h: o.h * ts, data: ts !== 1 ? { ...o.data, size: Math.max(6, Math.round((o.data.size || 24) * ts * 10) / 10) } : o.data })
          } else puts.push({ ...o, x: nx, y: ny, w: Math.max(10, o.w * sx), h: Math.max(10, o.h * sy) })
        }
        apply(puts)
        return
      }
      case 'create': {
        const o = all.get(d.id)
        if (!o) return
        let w = Math.abs(p.x - d.start.x), h = Math.abs(p.y - d.start.y)
        if (e.shiftKey) w = h = Math.max(w, h)
        apply([{ ...o, x: snapV(Math.min(p.x, d.start.x)), y: snapV(Math.min(p.y, d.start.y)), w, h }])
        return
      }
      case 'line': case 'endpoint': {
        const o = all.get(d.id)
        if (!o) return
        const end = d.mode === 'line' ? 'b' : d.end
        const tgt = targetAt(p, end === 'b' ? o.data.from : o.data.to)
        setHoverTarget(tgt)
        const dd = end === 'b' ? { x2: p.x, y2: p.y, to: tgt || undefined } : { x1: p.x, y1: p.y, from: tgt || undefined }
        const n = withData(o, dd)
        const [a, b] = [{ x: n.data.x1!, y: n.data.y1! }, { x: n.data.x2!, y: n.data.y2! }]
        apply([{ ...n, x: Math.min(a.x, b.x), y: Math.min(a.y, b.y), w: Math.abs(a.x - b.x), h: Math.abs(a.y - b.y) }])
        return
      }
      case 'pen': {
        const q = [Math.round((p.x - d.origin.x) * 10) / 10, Math.round((p.y - d.origin.y) * 10) / 10]
        const l = d.pts[d.pts.length - 1]
        if (Math.hypot(q[0] - l[0], q[1] - l[1]) < 2 / camRef.current.z) return
        d.pts.push(q)
        const o = all.get(d.id)
        if (o) setObjs(m => new Map(m).set(d.id, { ...o, data: { ...o.data, points: [...d.pts] } }))
        return
      }
      case 'erase': eraseAt(p); return
      case 'plus': {
        if (Math.hypot(e.clientX - d.sx, e.clientY - d.sy) > 6) {
          const [id] = [...selRef.current]
          if (id) connectFrom(id, d.dir)
        }
        return
      }
      case 'rotate': {
        const o = all.get(d.id)
        if (!o) return
        let r = d.r0 + (Math.atan2(p.y - d.c.y, p.x - d.c.x) - d.a0) * 180 / Math.PI
        r = ((r % 360) + 540) % 360 - 180
        if (e.shiftKey) r = Math.round(r / 15) * 15
        else if (Math.abs(r) < 3) r = 0
        apply([withData(o, { rot: r ? Math.round(r * 10) / 10 : undefined })])
        return
      }
    }
  }
  const hoverRef = useRef<string | null>(null)

  const finishDrag = (cancelled = false) => {
    const d = dragRef.current
    dragRef.current = null
    setMarquee(null); setGuides([]); setHoverTarget(null)
    if (!d) return
    const all = objsRef.current
    switch (d.mode) {
      case 'move': {
        if (d.moved) { record(d.before); return }
        if (cancelled) return
        const o = all.get(d.hit)
        if (votes.open && o) { castVote(d.hit, 1); return }
        if (!o) return
        if (d.wasSel && linkRef.current) { window.open(linkRef.current, '_blank', 'noopener'); linkRef.current = null; return }
        if (!d.shift && d.wasSel && selRef.current.size > 1 && !o.data.group) setSel(new Set([d.hit]))
        else if (!d.shift && d.wasSel && canEdit && ['sticky', 'text', 'shape'].includes(o.kind) && !o.data.locked) setEditing(d.hit)
        return
      }
      case 'resize': case 'endpoint': case 'rotate': record(d.before); return
      case 'plus': if (!cancelled) plusAdd(d.dir); return
      case 'erase': if (d.before.length) record(d.before); return
      case 'create': {
        const o = all.get(d.id)
        if (!o) return
        if (o.w < 12 || o.h < 12) {
          const w = o.kind === 'frame' ? 800 : 200, h = o.kind === 'frame' ? 560 : 130
          apply([{ ...o, x: d.start.x - w / 2, y: d.start.y - h / 2, w, h }])
        }
        record(d.before)
        setSel(new Set([o.id]))
        setTool('select')
        if (o.kind === 'shape') setEditing(o.id)
        return
      }
      case 'line': {
        const o = all.get(d.id)
        if (!o) return
        const len = Math.hypot((o.data.x2 ?? 0) - (o.data.x1 ?? 0), (o.data.y2 ?? 0) - (o.data.y1 ?? 0))
        if (len < 8 && !o.data.to) { apply([], [o.id]); return }
        record(d.before)
        setSel(new Set([o.id]))
        setTool('select')
        return
      }
      case 'pen': {
        const o = all.get(d.id)
        if (!o) return
        const xs = d.pts.map(q => q[0]), ys = d.pts.map(q => q[1])
        const mx = Math.min(...xs), my = Math.min(...ys)
        const pts = d.pts.length === 1 ? [[0, 0], [0.1, 0.1]] : d.pts.map(q => [Math.round((q[0] - mx) * 10) / 10, Math.round((q[1] - my) * 10) / 10])
        const n: Obj = { ...o, x: d.origin.x + mx, y: d.origin.y + my, w: Math.max(...xs) - mx, h: Math.max(...ys) - my, data: { ...o.data, points: pts } }
        apply([n])
        record(d.before)
        return
      }
    }
  }

  const onPointerUp = (e: React.PointerEvent) => {
    window.clearTimeout(pressT.current); pressT.current = 0
    pointers.current.delete(e.pointerId)
    const d = dragRef.current
    if (d?.mode === 'pinch') { if (pointers.current.size < 2) dragRef.current = null; return }
    // a double tap / double click
    const now = Date.now()
    const hit = hitAt(e)
    const tap = d && ((d.mode === 'pan' && !d.moved) || (d.mode === 'move' && !d.moved) || (d.mode === 'marquee' && !marquee))
    finishDrag()
    if (!tap || toolRef.current !== 'select') return
    const lt = lastTap.current
    if (now - lt.t < 350 && Math.hypot(e.clientX - lt.x, e.clientY - lt.y) < 12 && lt.id === hit.id) {
      lastTap.current = { t: 0, x: 0, y: 0, id: null }
      onDouble(hit.id, toWorld(e.clientX, e.clientY))
    } else lastTap.current = { t: now, x: e.clientX, y: e.clientY, id: hit.id }
  }

  const onDouble = (id: string | null, p: Pt) => {
    if (!canEdit || votes.open) return
    const o = id ? objsRef.current.get(id) : undefined
    if (o && !o.data.locked && ['sticky', 'text', 'shape', 'frame', 'line'].includes(o.kind)) { setSel(new Set([o.id])); setEditing(o.id); return }
    if (!o) addSticky({ x: snapV(p.x), y: snapV(p.y) })
  }

  // the + beside one selected note: another one next to it (shapes: joined by a line)
  const plusAdd = (dir: string) => {
    const [id] = [...selRef.current]
    const o = objsRef.current.get(id)
    if (!o) return
    const gap = o.kind === 'sticky' ? 24 : 80
    const off = { r: [o.w + gap, 0], l: [-o.w - gap, 0], d: [0, o.h + gap], u: [0, -o.h - gap] }[dir] || [o.w + gap, 0]
    const n: Obj = { ...o, id: newID(), x: o.x + off[0], y: o.y + off[1], z: zs().hi + 1, data: { ...o.data, text: '' }, author: me.name }
    const puts = [n]
    if (o.kind === 'shape') {
      const c1 = center(o), c2 = center(n)
      puts.push(make('line', { x: Math.min(c1.x, c2.x), y: Math.min(c1.y, c2.y), w: Math.abs(c2.x - c1.x), h: Math.abs(c2.y - c1.y) }, { x1: c1.x, y1: c1.y, x2: c2.x, y2: c2.y, from: o.id, to: n.id, end: 'arrow', curve: 'elbow' }))
    }
    commit(puts)
    setSel(new Set([n.id]))
    setEditing(n.id)
  }

  // ---- editing text
  const onEditDone = useCallback((id: string, r: EditResult, how: 'done' | 'tab' | 'cancel') => H.current.editDone(id, r, how), [])
  const editDone = (id: string, r: EditResult, how: 'done' | 'tab' | 'cancel') => {
    if (editingRef.current === id) setEditing(null)
    const o = objsRef.current.get(id)
    if (!o) return
    const { text, html, h } = r
    const key = o.kind === 'frame' ? 'title' : 'text'
    const before = (o.data[key as 'text'] || '') as string
    if (o.kind === 'text' && !text.trim()) { commit([], [o.id]); return }
    if (how !== 'cancel' && (before !== text || (o.data.html || '') !== (html || ''))) {
      const n = withData(o, key === 'title' ? { title: text } : { text, html })
      if (o.kind === 'text' && h) n.h = Math.max(20, h)
      commit([n])
    }
    if (how === 'tab' && o.kind === 'sticky') {
      const n: Obj = { ...o, id: newID(), x: o.x + o.w + 24, z: zs().hi + 1, data: { ...o.data, text: '', html: undefined }, author: me.name }
      commit([n]); setSel(new Set([n.id])); setEditing(n.id)
    }
  }

  // ---- votes, timer
  const castVote = async (id: string, delta: number) => {
    try { setVotes((await call<{ votes: Votes }>(conn, 'POST', '/vote', { client: CLIENT, object_id: id, delta })).votes) } catch (e) { toast(errMsg(e), 'err') }
  }
  const setVote = async (body: Record<string, unknown>) => {
    try { const r = await call<{ board: Board; votes: Votes }>(conn, 'PUT', '/vote', { client: CLIENT, ...body }); setBoard(b => ({ ...b, ...r.board })); setVotes(r.votes) } catch (e) { toast(errMsg(e), 'err') }
  }
  const setTimer = async (body: { seconds?: number; add?: number }) => {
    try { const r = await call<{ board: Board }>(conn, 'PUT', '/timer', { client: CLIENT, ...body }); setBoard(b => ({ ...b, ...r.board })) } catch (e) { toast(errMsg(e), 'err') }
  }

  // ---- selection actions
  const selected = () => [...selRef.current].map(id => objsRef.current.get(id)).filter(Boolean) as Obj[]
  const del = () => { const ids = [...selRef.current].filter(id => !objsRef.current.get(id)?.data.locked); if (!ids.length) return; commit([], ids); setSel(new Set()) }
  const duplicate = (list = selected(), off = 30) => {
    if (!list.length) return
    const map = new Map<string, string>()
    for (const o of list) map.set(o.id, newID())
    const { hi } = zs()
    const puts = list.map((o, i) => {
      const n = moved({ ...o, id: map.get(o.id)!, z: o.kind === 'frame' ? o.z : hi + 1 + i, author: me.name }, off, off)
      if (o.kind === 'line') n.data = { ...n.data, from: o.data.from && map.get(o.data.from), to: o.data.to && map.get(o.data.to) }
      return n
    })
    commit(puts)
    setSel(new Set(puts.map(o => o.id)))
  }
  const restyle = (d: Partial<ObjData>, only?: (o: Obj) => boolean) => {
    const puts = selected().filter(o => !only || only(o)).map(o => withData(o, d))
    if (puts.length) commit(puts)
  }
  const colourAll = (c: string) => {
    const puts = selected().map(o => {
      switch (o.kind) {
        case 'sticky': return withData(o, { color: c })
        case 'shape': return withData(o, { fill: c })
        case 'frame': return withData(o, { fill: c })
        default: return o
      }
    }).filter((o, i) => o !== selected()[i])
    if (puts.length) commit(puts)
    if (selected().some(o => o.kind === 'sticky')) setStickyColor(c)
  }
  const order = (front: boolean) => {
    const list = selected().sort((a, b) => a.z - b.z)
    const { hi, lo } = zs()
    commit(list.map((o, i) => ({ ...o, z: front ? hi + 1 + i : lo - list.length + i })))
  }
  const toggleLock = () => { const l = selected(); const lock = !l.every(o => o.data.locked); commit(l.map(o => withData(o, { locked: lock || undefined }))) }
  function group() {
    const l = selected().filter(o => o.kind !== 'frame')
    if (l.length < 2) { toast('Pick two or more things to group them'); return }
    const g = newID()
    commit(l.map(o => withData(o, { group: g })))
    toast('Grouped — they move together. Alt-click picks one of them.')
  }
  function ungroup() {
    const l = selected().filter(o => o.data.group)
    if (!l.length) return
    commit(l.map(o => withData(o, { group: undefined })))
  }
  // tags
  const allTags = useMemo(() => { const t = new Set<string>(); for (const o of objs.values()) for (const x of o.data.tags || []) t.add(x); return [...t].sort((a, b) => a.localeCompare(b)) }, [objs])
  const toggleTag = (tag: string, add: boolean) => {
    const l = selected().filter(o => o.kind === 'sticky' || o.kind === 'shape')
    if (!l.length) return
    commit(l.map(o => {
      const cur = o.data.tags || []
      const next = add ? (cur.includes(tag) ? cur : [...cur, tag]) : cur.filter(x => x !== tag)
      return withData(o, { tags: next.length ? next : undefined })
    }))
  }
  const groupByTag = () => {
    const list = (sel.size ? selected() : [...objsRef.current.values()]).filter(o => o.kind === 'sticky' && !o.data.locked)
    const by = new Map<string, string[]>()
    for (const o of list) { const t = o.data.tags?.[0] || 'No tag'; by.set(t, [...(by.get(t) || []), o.id]) }
    if (by.size < 1 || list.length < 2) { toast('Tag some notes first'); return }
    placeGroups([...by.entries()].sort((a, b) => (a[0] === 'No tag' ? 1 : b[0] === 'No tag' ? -1 : a[0].localeCompare(b[0]))).map(([title, ids]) => ({ title, ids })))
  }
  // find on the board
  const matches = useMemo(() => {
    const q = find?.q.trim().toLowerCase()
    if (!q) return [] as Obj[]
    const tag = q.startsWith('#') ? q.slice(1) : ''
    return framesInOrder([]).concat([...objs.values()].filter(o => tag ? (o.data.tags || []).some(t => t.toLowerCase().includes(tag)) : textOf(o).toLowerCase().includes(q) || (o.data.tags || []).some(t => t.toLowerCase() === q))
      .sort((a, b) => (Math.abs(a.y - b.y) > 60 ? a.y - b.y : a.x - b.x)))
  }, [find?.q, objs])
  const findGo = (dir: number) => {
    if (!find || !matches.length) return
    const i = ((find.i + dir) % matches.length + matches.length) % matches.length
    setFind({ ...find, i })
    focusOn(matches[i].id)
  }
  useEffect(() => { if (find && matches.length) { setFind(f => f && ({ ...f, i: 0 })); focusOn(matches[0].id, false) } }, [find?.q]) // eslint-disable-line react-hooks/exhaustive-deps
  const arrangeGrid = (byColour = false) => {
    const l = selected().filter(o => o.kind !== 'line' && o.kind !== 'frame')
    if (l.length < 2) return
    const box = union(l.map(o => rectOf(o)))!
    const cell = Math.max(...l.map(o => o.w)) + 20, rowH = Math.max(...l.map(o => o.h)) + 20
    let puts: Obj[] = []
    if (byColour) {
      const groups = new Map<string, Obj[]>()
      for (const o of l) { const k = o.data.color || o.data.fill || 'other'; groups.set(k, [...(groups.get(k) || []), o]) }
      let col = 0
      for (const [, g] of [...groups.entries()].sort((a, b) => STICKY_KEYS.indexOf(a[0]) - STICKY_KEYS.indexOf(b[0]))) {
        g.forEach((o, i) => puts.push({ ...o, x: box.x + col * (cell + 40), y: box.y + i * rowH }))
        col++
      }
    } else {
      const cols = Math.ceil(Math.sqrt(l.length))
      puts = [...l].sort((a, b) => (Math.abs(a.y - b.y) > 40 ? a.y - b.y : a.x - b.x)).map((o, i) => ({ ...o, x: box.x + (i % cols) * cell, y: box.y + Math.floor(i / cols) * rowH }))
    }
    commit(puts)
  }
  const align = (how: 'left' | 'top' | 'hcenter' | 'vcenter') => {
    const l = selected().filter(o => o.kind !== 'line')
    if (l.length < 2) return
    const box = union(l.map(o => rectOf(o)))!
    commit(l.map(o => ({ ...o, x: how === 'left' ? box.x : how === 'hcenter' ? box.x + box.w / 2 - o.w / 2 : o.x, y: how === 'top' ? box.y : how === 'vcenter' ? box.y + box.h / 2 - o.h / 2 : o.y })))
  }

  // ---- clipboard
  const clip = useRef<Obj[]>([])
  const pasteObjs = (list: Obj[], at?: Pt) => {
    const box = union(list.map(o => rectOf(o)))
    if (!box) return
    const p = at || viewCenter()
    const dx = p.x - (box.x + box.w / 2), dy = p.y - (box.y + box.h / 2)
    duplicate(list, 0)
    const ids = [...selRef.current]
    apply(ids.map(id => moved(objsRef.current.get(id)!, dx, dy)))
    // fold the move into the same undo step
    const e = undoStack.current[undoStack.current.length - 1]
    if (e) e.after = e.after.map(([id]) => [id, objsRef.current.get(id) || null])
  }
  const addImage = async (file: File, at: Pt) => {
    try {
      const r = await uploadPicture(file, conn.uploadURL, conn.headers)
      const s = Math.min(1, 480 / Math.max(r.w || 480, 1))
      const w = (r.w || 480) * s, h = (r.h || 320) * s
      const o = make('image', { x: at.x - w / 2, y: at.y - h / 2, w, h }, { upload: r.upload.id, src: r.url, name: r.upload.name })
      commit([o])
      setSel(new Set([o.id]))
    } catch (e) { toast(errMsg(e), 'err') }
  }
  const pasteText = (text: string, at: Pt) => {
    const lines = text.split(/\r?\n/).map(s => s.replace(/^\s*([-*•]|\d+[.)])\s+/, '').trim()).filter(Boolean).slice(0, 200)
    if (!lines.length) return
    if (lines.length === 1) { addSticky(at, { text: lines[0], edit: false }); return }
    const cols = Math.ceil(Math.sqrt(lines.length))
    const puts = lines.map((t, i) => make('sticky', { x: at.x + (i % cols) * 220 - cols * 110, y: at.y + Math.floor(i / cols) * 220, w: 200, h: 200 }, { text: t, color: stickyColor }))
    puts.forEach((o, i) => { o.z += i })
    commit(puts)
    setSel(new Set(puts.map(o => o.id)))
  }
  useEffect(() => {
    const copy = (e: ClipboardEvent) => {
      if (isEditable(e.target) || !selRef.current.size) return
      const list = selected()
      clip.current = list
      e.clipboardData?.setData('text/plain', 'pocket-whiteboard:' + JSON.stringify(list.map(slim)))
      e.preventDefault()
      if (e.type === 'cut' && canEdit) del()
    }
    const paste = (e: ClipboardEvent) => {
      if (isEditable(e.target) || !canEdit) return
      const at = lastPointer.current && vpRef.current?.matches(':hover') ? lastPointer.current : viewCenter()
      const files = [...(e.clipboardData?.files || [])].filter(f => f.type.startsWith('image/'))
      if (files.length) { e.preventDefault(); files.forEach((f, i) => addImage(f, { x: at.x + i * 40, y: at.y + i * 40 })); return }
      const text = e.clipboardData?.getData('text/plain') || ''
      if (!text) return
      e.preventDefault()
      if (text.startsWith('pocket-whiteboard:')) {
        try { pasteObjs(JSON.parse(text.slice(18)) as Obj[], at) } catch { /* not ours after all */ }
        return
      }
      pasteText(text, at)
    }
    window.addEventListener('copy', copy)
    window.addEventListener('cut', copy)
    window.addEventListener('paste', paste)
    return () => { window.removeEventListener('copy', copy); window.removeEventListener('cut', copy); window.removeEventListener('paste', paste) }
  })

  // ---- keys
  useEffect(() => {
    const down = (e: KeyboardEvent) => {
      if (isEditable(e.target) || document.querySelector('.modal-backdrop, .palette-backdrop')) return
      const mod = e.metaKey || e.ctrlKey
      const k = e.key.toLowerCase()
      if (present !== null) {
        if (k === 'escape') { setPresent(null); fitAll() }
        if (k === 'arrowright' || k === ' ' || k === 'pagedown') { e.preventDefault(); presentGo(1) }
        if (k === 'arrowleft' || k === 'pageup') { e.preventDefault(); presentGo(-1) }
        return
      }
      if (k === ' ' && !e.repeat) { setSpaceDown(true); e.preventDefault(); return }
      if (mod && k === 'z') { e.preventDefault(); if (e.shiftKey) redo(); else undo(); return }
      if (mod && k === 'y') { e.preventDefault(); redo(); return }
      if (mod && k === 'a') { e.preventDefault(); setSel(new Set(objsRef.current.keys())); return }
      if (mod && k === 'd') { e.preventDefault(); if (canEdit) duplicate(); return }
      if (mod && k === 'f') { e.preventDefault(); setFind(f => f || { q: '', i: 0 }); return }
      if (mod && k === 'g') { e.preventDefault(); if (canEdit) { if (e.shiftKey) ungroup(); else group() } return }
      if (mod && (k === '=' || k === '+')) { e.preventDefault(); zoomBy(1.25); return }
      if (mod && k === '-') { e.preventDefault(); zoomBy(0.8); return }
      if (mod && k === '0') { e.preventDefault(); const c = viewCenter(); const r = vpRef.current!; setCam({ z: 1, x: r.clientWidth / 2 - c.x, y: r.clientHeight / 2 - c.y }); return }
      if (mod) return
      if (e.shiftKey && (k === '!' || e.code === 'Digit1')) { e.preventDefault(); fitAll(); return }
      if (e.shiftKey && (k === '@' || e.code === 'Digit2')) { e.preventDefault(); const r = union(selected().map(o => rectOf(o, objsRef.current))); if (r) animateTo(camFor(r, 120, 2)); return }
      if (k === 'escape') { setSel(new Set()); setTool('select'); setPanel(null); setFlyout(null); return }
      if ((k === 'delete' || k === 'backspace') && canEdit) { e.preventDefault(); del(); return }
      if (k === 'enter' && selRef.current.size === 1 && canEdit) { const [id] = [...selRef.current]; const o = objsRef.current.get(id); if (o && ['sticky', 'text', 'shape', 'frame', 'line'].includes(o.kind)) { e.preventDefault(); setEditing(id) } return }
      if (k.startsWith('arrow') && selRef.current.size && canEdit) {
        e.preventDefault()
        const s = e.shiftKey ? 10 : 1
        const dx = k === 'arrowleft' ? -s : k === 'arrowright' ? s : 0, dy = k === 'arrowup' ? -s : k === 'arrowdown' ? s : 0
        commit(movingSet([...selRef.current]).map(id => moved(objsRef.current.get(id)!, dx, dy)))
        return
      }
      if (k === ']' && canEdit) { order(true); return }
      if (k === '[' && canEdit) { order(false); return }
      if (k === '=' || k === '+') { zoomBy(1.25); return }
      if (k === '-') { zoomBy(0.8); return }
      const tools: Record<string, Tool> = { v: 'select', h: 'hand', n: 'sticky', t: 'text', s: 'shape', r: 'shape', l: 'line', p: 'pen', e: 'eraser', f: 'frame', c: 'comment', k: 'sticker' }
      if (k === 'c' && guest && !canEdit) return
      if (tools[k] && (canEdit || k === 'v' || k === 'h' || k === 'c')) { setTool(tools[k]); setFlyout(['sticker', 'shape', 'line', 'pen'].includes(tools[k]) ? tools[k] : null); return }
      if (k === 'i' && canEdit) { pickImage(); return }
      if (k === '?') { setPanel(null); setShortcuts(true) }
    }
    const up = (e: KeyboardEvent) => { if (e.key === ' ') setSpaceDown(false) }
    window.addEventListener('keydown', down)
    window.addEventListener('keyup', up)
    return () => { window.removeEventListener('keydown', down); window.removeEventListener('keyup', up) }
  })
  const [shortcuts, setShortcuts] = useState(false)

  const fileInput = useRef<HTMLInputElement>(null)
  const pickImage = () => fileInput.current?.click()

  // ---- present
  const frames = useMemo(() => framesInOrder(objs.values()), [objs])
  const presentGo = (dir: number) => {
    setPresent(i => {
      if (i === null || !frames.length) return i
      const n = clamp(i + dir, 0, frames.length - 1)
      animateTo(camFor(frames[n], 24, 4), 380)
      return n
    })
  }
  const startPresent = () => {
    if (!frames.length) { toast('Draw a frame first: each frame is a slide'); return }
    setPanel(null); setSel(new Set()); setPresent(0)
    animateTo(camFor(frames[0], 24, 4), 380)
    document.documentElement.requestFullscreen?.().catch(() => {})
  }
  useEffect(() => { if (present === null && document.fullscreenElement) document.exitFullscreen?.().catch(() => {}) }, [present])
  // Esc in full screen is the browser's: leaving full screen ends presenting too
  useEffect(() => {
    const h = () => { if (!document.fullscreenElement) setPresent(p => { if (p !== null) window.setTimeout(() => fitAll(), 0); return null }) }
    document.addEventListener('fullscreenchange', h)
    return () => document.removeEventListener('fullscreenchange', h)
  }, []) // eslint-disable-line react-hooks/exhaustive-deps
  // the board never scrolls as a page: if the browser scrolls it (to show a text caret), put it back
  const rootRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const els = [rootRef.current, vpRef.current].filter(Boolean) as HTMLElement[]
    const h = (e: Event) => { const el = e.currentTarget as HTMLElement; if (el.scrollTop || el.scrollLeft) { el.scrollTop = 0; el.scrollLeft = 0 } }
    els.forEach(el => el.addEventListener('scroll', h))
    return () => els.forEach(el => el.removeEventListener('scroll', h))
  }, [])

  // ---- the board's own actions
  const rename = async (title: string) => {
    setRenaming(false)
    if (!title.trim() || title === board.title) return
    try { await call(conn, 'PUT', '', { title }); setBoard(b => ({ ...b, title })) } catch (e) { toast(errMsg(e), 'err') }
  }
  const savePicture = async (toFiles = false) => {
    const list = sel.size ? selected() : [...objs.values()]
    try {
      const blob = await boardPNG(list, objsRef.current, { imageSrc: conn.imgSrc, background: '#ffffff' })
      if (toFiles) {
        const fd = new FormData()
        fd.append('file', blob, 'board.png')
        const res = await fetch(`${conn.base}/files`, { method: 'POST', body: fd, credentials: 'same-origin' })
        const j = await res.json().catch(() => ({}))
        if (!res.ok) throw new Error(j.error || res.statusText)
        toast(j.text || 'Kept in Pocket Files', 'ok', j.url ? { label: 'Open', run: () => window.open(j.url, '_blank', 'noopener') } : undefined)
        return
      }
      const a = document.createElement('a')
      a.href = URL.createObjectURL(blob)
      a.download = `${board.title.replace(/[\\/:*?"<>|]/g, '-')}.png`
      a.click()
      setTimeout(() => URL.revokeObjectURL(a.href), 4000)
    } catch (e) { toast(errMsg(e), 'err') }
  }
  const toApp = async (path: string, body?: unknown) => {
    try {
      const r = await call<{ text?: string; url?: string }>(conn, 'POST', path, body)
      toast(r.text || 'Done', 'ok', r.url ? { label: 'Open', run: () => window.open(r.url, '_blank', 'noopener') } : undefined)
    } catch (e) { toast(errMsg(e), 'err') }
  }
  /** The right-click menu: for the thing under the pointer (picking it first), or for the board there. */
  function openMenuAt(x: number, y: number, target: EventTarget | null) {
    if (present !== null) return
    const h = hitAt({ target })
    const p = toWorld(x, y)
    let on: string | null = h.id && objsRef.current.has(h.id) ? h.id : null
    if (on && !selRef.current.has(on)) setSel(withGroups([on]))
    if (!on) on = null
    setEditing(null)
    setCtxMenu({ x, y, p, on })
  }
  const copySel = async (cut = false) => {
    const list = selected()
    if (!list.length) return
    clip.current = list
    await copyText('pocket-whiteboard:' + JSON.stringify(list.map(slim)))
    if (cut && canEdit) del()
    toast(cut ? 'Cut' : `Copied ${list.length === 1 ? '' : list.length + ' things'}`.trim())
  }
  const pasteAt = async (p: Pt) => {
    let text = ''
    try { text = await navigator.clipboard.readText() } catch { /* no permission: use what was copied here */ }
    if (text.startsWith('pocket-whiteboard:')) { try { pasteObjs(JSON.parse(text.slice(18)) as Obj[], p); return } catch { /* not ours */ } }
    if (text.trim()) { pasteText(text, p); return }
    if (clip.current.length) pasteObjs(clip.current, p)
    else toast('Nothing copied yet')
  }
  const menuItems = (m: { p: Pt; on: string | null }): MenuItem[] => {
    const list = m.on ? selected() : []
    if (!list.length) {
      // the board at the pointer
      return [
        ...(canEdit ? [
          { label: 'Paste here', icon: 'copy', hint: '⌘V', onSelect: () => pasteAt(m.p) },
          'sep' as const,
          { label: 'Sticky note here', icon: 'sticky', hint: 'N', onSelect: () => addSticky({ x: snapV(m.p.x), y: snapV(m.p.y) }) },
          { label: 'Text here', icon: 'text', hint: 'T', onSelect: () => { const o = make('text', { x: m.p.x, y: m.p.y - 18, w: 320, h: 36 }, { text: '', size: 24 }); commit([o]); setSel(new Set([o.id])); setEditing(o.id) } },
          { label: 'Sticker here', icon: 'sticker', submenu: STICKERS.slice(0, 16).map(e => ({ label: <span className="wb-react-item">{e}</span>, onSelect: () => { const o = make('sticker', { x: m.p.x - 60, y: m.p.y - 60, w: 120, h: 120 }, { emoji: e }); commit([o]); setSel(new Set([o.id])) } })) },
        ] : []),
        ...(!guest || canEdit ? [{ label: 'Comment here', icon: 'comment', hint: 'C', onSelect: () => { setOpenThread(null); setNewThread({ object_id: '', x: Math.round(m.p.x), y: Math.round(m.p.y) }) } }] : []),
        'sep',
        { label: 'Pick everything', icon: 'grid', hint: '⌘A', onSelect: () => setSel(new Set(objsRef.current.keys())) },
        { label: 'Fit everything', icon: 'maximize', hint: '⇧1', onSelect: () => fitAll() },
        { label: 'Find on this board', icon: 'search', hint: '⌘F', onSelect: () => setFind({ q: '', i: 0 }) },
        ...(!guest ? [{ label: 'Bring everyone here', icon: 'magnet', onSelect: summon }] : []),
        ...(canEdit && !guest ? [{ label: 'Background', icon: 'grid-sort', submenu: (['dots', 'grid', 'plain'] as const).map(b => ({ label: b === 'dots' ? 'Dots' : b === 'grid' ? 'Grid' : 'Plain', checked: board.background === b, onSelect: async () => { try { await call(conn, 'PUT', '', { background: b }); setBoard(x => ({ ...x, background: b })) } catch (e) { toast(errMsg(e), 'err') } } })) }] : []),
      ]
    }
    const one = list.length === 1 ? list[0] : null
    const locked = list.every(o => o.data.locked)
    const coloured = list.some(o => ['sticky', 'shape', 'frame'].includes(o.kind))
    const grouped = list.length > 1 && list.every(o => o.data.group && o.data.group === list[0].data.group)
    const textish = list.filter(o => ['sticky', 'text', 'shape'].includes(o.kind) && textOf(o))
    const taggable = list.filter(o => o.kind === 'sticky' || o.kind === 'shape')
    const reactable = one && !['line', 'frame'].includes(one.kind)
    const editable = one && ['sticky', 'text', 'shape', 'frame', 'line'].includes(one.kind) && !locked
    return [
      { heading: one ? ({ sticky: 'Sticky note', text: 'Text', shape: 'Shape', line: 'Line', frame: 'Frame', pen: 'Drawing', image: 'Picture', sticker: 'Sticker' } as Record<string, string>)[one.kind] + (one.author ? ` · ${one.author}` : '') : `${list.length} things` },
      ...(canEdit && editable ? [{ label: one!.kind === 'frame' ? 'Rename' : one!.kind === 'line' ? 'Label' : 'Write', icon: 'edit', hint: '↩', onSelect: () => setEditing(one!.id) }] : []),
      { label: 'Copy', icon: 'copy', hint: '⌘C', onSelect: () => copySel() },
      ...(canEdit ? [
        { label: 'Cut', icon: 'scissors', hint: '⌘X', disabled: locked, onSelect: () => copySel(true) },
        { label: 'Paste here', icon: 'copy', hint: '⌘V', onSelect: () => pasteAt(m.p) },
        { label: 'Duplicate', icon: 'copy', hint: '⌘D', onSelect: () => duplicate() },
      ] : []),
      'sep',
      ...(canEdit && coloured && !locked ? [{ label: 'Colour', icon: 'palette', submenu: STICKY_KEYS.map(c => ({ label: <span className="wb-menu-colour"><i style={{ background: STICKY[c].bg }} />{STICKY[c].name}</span>, checked: !!one && (one.data.color || one.data.fill) === c, onSelect: () => colourAll(c) })) }] : []),
      ...(canEdit && one?.kind === 'sticker' && !locked ? [{ label: 'Change the sticker', icon: 'sticker', submenu: STICKERS.map(e => ({ label: <span className="wb-react-item">{e}</span>, checked: one.data.emoji === e, onSelect: () => restyle({ emoji: e }) })) }] : []),
      ...(reactable ? [{ label: 'React', icon: 'smile', submenu: REACTIONS.map(e => ({ label: <span className="wb-react-item">{e}</span>, checked: (reactions[one!.id] || []).some(r => r.emoji === e && r.voters.includes(me.voter)), onSelect: () => react(one!.id, e) })) }] : []),
      ...(canEdit && taggable.length && !locked ? [{ label: 'Tags', icon: 'tag', submenu: [
        ...allTags.map(t => { const all = taggable.every(o => o.data.tags?.includes(t)); return { label: t, checked: all, onSelect: () => toggleTag(t, !all) } }),
        ...(allTags.length ? ['sep' as const] : []),
        { label: 'New tag…', icon: 'plus', onSelect: () => { const t = window.prompt('New tag')?.trim().replace(/^#/, '').slice(0, 30); if (t) toggleTag(t, true) } },
      ] }] : []),
      ...(one && (!guest || canEdit) ? [{ label: 'Comment', icon: 'comment', onSelect: () => { setOpenThread(null); setNewThread({ object_id: one.kind === 'line' ? '' : one.id, x: Math.round(one.x + one.w), y: Math.round(one.y) }) } }] : []),
      'sep',
      ...(canEdit ? [{ label: 'Arrange', icon: 'layers', submenu: [
        { label: 'Bring to front', icon: 'bring-front', hint: ']', onSelect: () => order(true) },
        { label: 'Send to back', icon: 'send-back', hint: '[', onSelect: () => order(false) },
        ...(list.length > 1 && !grouped ? [{ label: 'Group', icon: 'group', hint: '⌘G', onSelect: group }] : []),
        ...(list.some(o => o.data.group) ? [{ label: 'Ungroup', icon: 'ungroup', hint: '⇧⌘G', onSelect: ungroup }] : []),
        ...(list.length > 1 ? [{ label: 'Arrange in a grid', icon: 'grid', onSelect: () => arrangeGrid(false) }, { label: 'Sort into columns by colour', icon: 'grid-sort', onSelect: () => arrangeGrid(true) }] : []),
        { label: locked ? 'Unlock' : 'Lock in place', icon: 'lock', onSelect: toggleLock },
      ] }] : []),
      { label: 'Zoom to it', icon: 'zoom-in', hint: '⇧2', onSelect: () => { const r = union(list.map(o => rectOf(o, objsRef.current))); if (r) animateTo(camFor(r, 120, 2)) } },
      { label: 'More', icon: 'more', submenu: [
        ...(!guest && one ? [{ label: 'Copy a link to it', icon: 'link', onSelect: async () => { if (await copyText(`${window.location.origin}/b/${board.id}?o=${one.id}`)) toast('Link copied') } }] : []),
        { label: 'Download a picture of it', icon: 'image', onSelect: () => savePicture() },
        ...(!guest && textish.length ? [{ label: `Make ${textish.length > 1 ? `${textish.length} tasks` : 'a task'} in Pocket Tasks`, icon: 'check-square', disabled: !tasksOK, hint: tasksOK ? undefined : 'off', onSelect: () => toApp('/tasks', { ids: list.map(o => o.id) }) }] : []),
        ...(!guest ? [{ label: 'Pocket AI on these', icon: 'sparkles', onSelect: () => setPanel('ai') }] : []),
      ] },
      ...(canEdit ? ['sep' as const, { label: 'Delete', icon: 'trash', hint: 'Del', danger: true, disabled: locked, onSelect: del }] : []),
    ]
  }
  const tasksOK = !guest && appState('tasks', 'tasks:write').ok
  const notesOK = !guest && appState('notes', 'notes:write').ok
  const filesOK = !guest && appState('files', 'files:write').ok
  const chatOK = !guest && appState('chat', 'chat:write').ok
  const needs = (ok: boolean, app: string) => ok ? undefined : `Connect Pocket ${app} in Settings → Connected apps`

  const boardMenu = (): MenuItem[] => [
    { label: 'Fit everything', icon: 'maximize', hint: '⇧1', onSelect: () => fitAll() },
    { label: 'Frames', icon: 'frame', onSelect: () => setPanel('frames') },
    { label: 'Present', icon: 'presentation', onSelect: startPresent },
    { label: 'Find on this board', icon: 'search', hint: '⌘F', onSelect: () => setFind({ q: '', i: 0 }) },
    ...(canEdit ? [{ label: 'Group the notes by tag', icon: 'tag', onSelect: groupByTag }] : []),
    ...(canEdit && !guest ? [{ label: board.private?.on ? 'Private mode: reveal all' : 'Private mode', icon: 'eye-off', onSelect: () => togglePrivate(!board.private?.on) }] : []),
    ...(!guest ? [{ label: 'Bring everyone to me', icon: 'magnet', onSelect: summon }, { label: 'History and deleted things', icon: 'history', onSelect: () => setPanel('history') }] : []),
    'sep',
    { label: sel.size ? 'Download a picture of what is picked' : 'Download a picture of the board', icon: 'image', onSelect: () => savePicture() },
    ...(!guest ? [
      { label: 'Keep a picture in Pocket Files', icon: 'download', disabled: !filesOK, hint: needs(filesOK, 'Files') ? 'off' : undefined, onSelect: () => savePicture(true) },
      { label: 'Keep the notes in Pocket Notes', icon: 'note', disabled: !notesOK, hint: needs(notesOK, 'Notes') ? 'off' : undefined, onSelect: () => toApp('/to-notes') },
      { label: 'Send the link to Pocket Chat', icon: 'message', disabled: !chatOK, hint: needs(chatOK, 'Chat') ? 'off' : undefined, onSelect: () => setPanel('chat') },
      { label: 'Download the notes (CSV)', icon: 'table', onSelect: () => { window.location.href = `${V1}/export.csv?board=${board.id}` } },
      'sep' as const,
      { label: starred ? 'Unstar' : 'Star', icon: 'star', onSelect: async () => { try { await api.put(`${conn.base}/star`, { starred: !starred }); setStarred(!starred) } catch (e) { toast(errMsg(e), 'err') } } },
      { label: 'Make a copy', icon: 'copy', onSelect: async () => { try { const r = await api.post<{ board: Board }>(`${conn.base}/duplicate`); navigate(`/b/${r.board.id}`); window.location.reload() } catch (e) { toast(errMsg(e), 'err') } } },
      ...(canEdit ? [{ label: 'Board settings', icon: 'settings', onSelect: () => setSettingsOpen(true) }] : []),
      ...(board.role === 'owner' ? [{ label: 'Delete the board', icon: 'trash', danger: true, onSelect: () => setConfirmDelete(true) }] : []),
    ] : []),
    'sep',
    { label: 'Keyboard shortcuts', icon: 'keyboard', hint: '?', onSelect: () => setShortcuts(true) },
  ]

  // AI results become ordinary changes (one undo step each)
  const placeGroups = (groups: { title: string; ids: string[] }[]) => {
    const all = objsRef.current
    const bounds = union([...all.values()].map(o => rectOf(o, all)))
    let x = (bounds ? bounds.x + bounds.w : 0) + 300
    const y = bounds ? bounds.y : 0
    const puts: Obj[] = []
    const { lo } = zs()
    groups.forEach((g, gi) => {
      const notes = g.ids.map(id => all.get(id)).filter(Boolean) as Obj[]
      if (!notes.length) return
      const cols = Math.min(3, Math.ceil(Math.sqrt(notes.length)))
      const cw = Math.max(...notes.map(o => o.w)) + 24, ch = Math.max(...notes.map(o => o.h)) + 24
      const fw = cols * cw + 56, fh = Math.ceil(notes.length / cols) * ch + 72
      puts.push({ id: newID(), kind: 'frame', x, y, w: fw, h: fh, z: lo - 1 - gi, data: { title: g.title, fill: STICKY_KEYS[(gi * 3) % STICKY_KEYS.length] }, author: me.name })
      notes.forEach((o, i) => puts.push(moved(o, x + 28 + (i % cols) * cw - o.x, y + 48 + Math.floor(i / cols) * ch - o.y)))
      x += fw + 80
    })
    commit(puts)
    const r = union(puts.map(o => rectOf(o)))
    if (r) animateTo(camFor(r, 60, 1))
    toast(`Sorted into ${groups.length} groups — Undo puts them back`)
  }
  const placeIdeas = (ideas: string[]) => {
    const at = sel.size ? (() => { const r = union(selected().map(o => rectOf(o, objsRef.current)))!; return { x: r.x + r.w + 260, y: r.y + 100 } })() : viewCenter()
    const cols = Math.min(4, ideas.length)
    const puts = ideas.map((t, i) => make('sticky', { x: at.x + (i % cols) * 220 - 100, y: at.y + Math.floor(i / cols) * 220 - 100, w: 200, h: 200 }, { text: t, color: 'violet' }))
    puts.forEach((o, i) => { o.z += i })
    commit(puts)
    setSel(new Set(puts.map(o => o.id)))
    const r = union(puts.map(o => rectOf(o)))
    if (r) animateTo(camFor(r, 80, 1))
  }
  const placeNote = (text: string) => {
    const c = viewCenter()
    const o = make('text', { x: c.x - 260, y: c.y - 100, w: 520, h: 200 }, { text, size: 20 })
    commit([o]); setSel(new Set([o.id]))
  }

  // ---- what to draw
  const visible = useMemo<Rect>(() => {
    const m = 300 / cam.z
    return { x: -cam.x / cam.z - m, y: -cam.y / cam.z - m, w: vpSize.w / cam.z + 2 * m, h: vpSize.h / cam.z + 2 * m }
  }, [cam, vpSize])
  const sorted = useMemo(() => [...objs.values()].sort((a, b) => a.z - b.z || (a.id < b.id ? -1 : 1)), [objs])
  const remoteSel = useMemo(() => { const m = new Map<string, string>(); for (const c of cursors.values()) for (const id of c.sel) m.set(id, c.colour); return m }, [cursors])
  const showCounts = votes.done || (votes.open && !votes.hide)
  const selList = [...sel].map(id => objs.get(id)).filter(Boolean) as Obj[]
  const selBox = union(selList.map(o => rectOf(o, objs)))
  const oneLine = selList.length === 1 && selList[0].kind === 'line' ? selList[0] : null
  const bg = board.background
  const gridStep = cam.z < 0.35 ? 120 : cam.z < 0.7 ? 40 : 20
  const vpStyle: React.CSSProperties = bg === 'plain' ? {} : { backgroundSize: `${gridStep * cam.z}px ${gridStep * cam.z}px`, backgroundPosition: `${cam.x}px ${cam.y}px` }
  const cursorClass = spaceDown || tool === 'hand' ? 'is-hand' : tool === 'select' ? '' : tool === 'eraser' ? 'is-erase' : 'is-cross'
  const showAuthor = !mobile && cam.z > 0.6
  const h = 10 / cam.z

  // the selection bar sits above what is picked, in screen space
  // floating bars sit just above what they are for (below it when there is no room); the board
  // area starts under the top bar, so its offset is added
  const vpTop = vpRef.current?.offsetTop ?? 52
  const barAt = (r: Rect, half = 160, room = 0) => {
    const above = cam.y + r.y * cam.z - 50 - room
    const top = above < 8 ? cam.y + (r.y + r.h) * cam.z + 10 + room : above
    // never outside the board area (a bar past the edge lets the browser scroll the page away)
    return { left: clamp(cam.x + (r.x + r.w / 2) * cam.z, half, Math.max(half, vpSize.w - half)), top: vpTop + clamp(top, 8, Math.max(8, vpSize.h - 50)) }
  }
  // the + buttons above a single picked note or shape need room too
  const plusShown = canEdit && selList.length === 1 && ['sticky', 'shape'].includes(selList[0].kind) && !selList[0].data.locked && cam.z > 0.25
  const ctxPos = selBox && !editing && !ctxMenu && present === null && !dragRef.current ? barAt(selBox, 160, plusShown ? 40 : 0) : null

  const H = useRef({ editDone })
  H.current = { editDone }

  const noteCount = useMemo(() => sorted.filter(o => o.kind === 'sticky').length, [sorted])

  return (
    <div ref={rootRef} className={`wb ${present !== null ? 'presenting' : ''} ${mobile ? 'is-mobile' : ''}`}>
      {/* ---- top bar */}
      <header className="wb-top">
        {!guest ? <button type="button" className="btn ghost icon-only wb-home" onClick={() => navigate('/')} title="All boards" aria-label="All boards"><img src="/favicon.svg" alt="" width={26} height={26} /></button>
          : <img src="/favicon.svg" alt="" width={26} height={26} className="wb-home" />}
        {renaming
          ? <input className="wb-title-input" autoFocus defaultValue={board.title} onBlur={e => rename(e.target.value)} onKeyDown={e => { if (e.key === 'Enter') (e.target as HTMLInputElement).blur(); if (e.key === 'Escape') setRenaming(false) }} aria-label="Board name" />
          : <button type="button" className="wb-title" disabled={!canEdit} onClick={() => setRenaming(true)} title={canEdit ? 'Rename' : board.title}>{board.title}</button>}
        {!live && <span className="wb-offline" title="Reconnecting — your changes are kept and sent when the connection is back"><Icon name="cloud" size={14} />Reconnecting</span>}
        {saving === 'error' && <span className="wb-offline bad"><Icon name="alert" size={14} />Not saved</span>}
        {!canEdit && <span className="wb-badge">View only</span>}
        <span className="spacer" />
        {board.timer_ends && <button type="button" className="wb-timer-pill" onClick={() => setPanel('timer')}><Icon name="timer" size={14} /><Countdown ends={board.timer_ends} onEnd={chime} /></button>}
        <div className="wb-people-row" aria-label={`${people.length} on the board now`}>
          {people.slice(0, mobile ? 2 : 5).map(p => {
            const mine = p.voter === me.voter
            const theirs = [...cursors.entries()].find(([, c]) => c.voter === p.voter)
            const items = (): MenuItem[] => mine ? [
              { heading: `${p.name} (you)` },
              ...(!guest ? [{ label: 'Bring everyone to me', icon: 'magnet', onSelect: summon }] : []),
            ] : [
              { heading: p.name },
              { label: following === theirs?.[0] ? 'Stop following' : 'Follow their view', icon: 'eye-follow', disabled: !theirs, hint: theirs ? undefined : 'not moving yet', onSelect: () => { if (!theirs) return; if (following === theirs[0]) setFollowing(null); else { setFollowing(theirs[0]); if (theirs[1].view) animateTo(camFor(theirs[1].view, 0, 8)) } } },
              { label: 'Go to their pointer', icon: 'cursor', disabled: !theirs, onSelect: () => { const c = theirs?.[1]; if (c) { const r = vpRef.current!; animateTo({ z: camRef.current.z, x: r.clientWidth / 2 - c.x * camRef.current.z, y: r.clientHeight / 2 - c.y * camRef.current.z }) } } },
            ]
            return (
              <MenuButton key={p.voter} label={p.name} className={`wb-person ${theirs && following === theirs[0] ? 'following' : ''}`} align="right" width={230} items={items}>
                <span style={{ ['--pc' as string]: p.colour }} className="wb-person-ring" title={`${p.name}${mine ? ' (you)' : ''}`}><Avatar name={p.name} size={28} color={p.colour} /></span>
              </MenuButton>
            )
          })}
          {people.length > (mobile ? 2 : 5) && <span className="wb-more-people">+{people.length - (mobile ? 2 : 5)}</span>}
        </div>
        {!mobile && <button type="button" className={`btn ghost icon-only ${panel === 'timer' ? 'on' : ''}`} onClick={() => setPanel(panel === 'timer' ? null : 'timer')} title="Timer" aria-label="Timer"><Icon name="timer" size={18} /></button>}
        <MenuButton label="Send a reaction" className="btn ghost icon-only" align="right" width={316} items={() => [{ heading: 'Everyone sees it float up' }, ...REACTIONS.map(e => ({ label: <span className="wb-react-item">{e}</span>, onSelect: () => sendFloat(e) }))]}><Icon name="smile" size={18} /></MenuButton>
        <button type="button" className={`btn ghost icon-only wb-has-count ${panel === 'comments' ? 'on' : ''}`} onClick={() => setPanel(panel === 'comments' ? null : 'comments')} title="Comments (C to add one)" aria-label="Comments">
          <Icon name="comment" size={18} />{threads.filter(t => !t.root.resolved).length > 0 && <i>{threads.filter(t => !t.root.resolved).length}</i>}
        </button>
        <button type="button" className={`btn ghost icon-only ${panel === 'vote' || votes.open ? 'on' : ''}`} onClick={() => setPanel(panel === 'vote' ? null : 'vote')} title="Voting" aria-label="Voting"><Icon name="vote" size={18} /></button>
        {!guest && !mobile && <button type="button" className={`btn ghost icon-only ${panel === 'ai' ? 'on' : ''}`} onClick={() => setPanel(panel === 'ai' ? null : 'ai')} title="Pocket AI: group, sum up, more ideas" aria-label="Pocket AI"><Icon name="sparkles" size={18} /></button>}
        {!guest && <button type="button" className="btn primary sm wb-share" onClick={() => setPanel(panel === 'share' ? null : 'share')}><Icon name="users" size={15} />{mobile ? '' : 'Share'}</button>}
        {!guest && <AskButton />}
        <MenuButton label="Board menu" align="right" width={290} items={() => [
          ...(mobile ? [{ label: 'Timer', icon: 'timer', onSelect: () => setPanel('timer') }, ...(!guest ? [{ label: 'Pocket AI', icon: 'sparkles', onSelect: () => setPanel('ai') }] : []), 'sep' as const] : []),
          ...boardMenu(),
        ]}><Icon name="more" size={18} /></MenuButton>
      </header>

      {board.private?.on && present === null && (
        <div className="wb-private-bar">
          <Icon name="eye-off" size={16} />
          <span><b>Private mode</b> — what you add stays hidden from the others until it is revealed.{privCounts.length > 0 && <> {privCounts.map(c => `${c.name || 'Someone'} ${c.n}`).join(' · ')}</>}</span>
          {canEdit && !guest && <button type="button" className="btn sm primary" onClick={() => togglePrivate(false)}>Reveal all</button>}
        </div>
      )}
      {following && present === null && (() => { const c = cursors.get(following); return (
        <div className="wb-follow-bar" style={{ ['--pc' as string]: c?.colour || 'var(--wb-sel)' }}>
          <span>Following <b>{c?.name || 'someone'}</b></span>
          <button type="button" className="btn sm" onClick={() => setFollowing(null)}>Stop</button>
        </div>
      ) })()}
      {following && present === null && <div className="wb-follow-frame" style={{ ['--pc' as string]: cursors.get(following)?.colour || 'var(--wb-sel)' }} />}
      {find && present === null && <FindBar q={find.q} setQ={q => setFind({ q, i: 0 })} n={matches.length} i={find.i} onNext={() => findGo(1)} onPrev={() => findGo(-1)} onClose={() => setFind(null)} />}
      {votes.open && present === null && (
        <div className="wb-vote-bar">
          <Icon name="vote" size={16} />
          <span><b>Voting</b> — click the notes that matter most. <b>{votes.left}</b> of {votes.per_person} left.</span>
          {canEdit && !guest && <button type="button" className="btn sm" onClick={() => setVote({ action: 'end' })}>End vote</button>}
        </div>
      )}

      {/* ---- the canvas */}
      <div ref={vpRef} className={`wb-vp bg-${bg} ${cursorClass}`} style={vpStyle}
        onPointerDown={onPointerDown} onPointerMove={onPointerMove} onPointerUp={onPointerUp} onPointerCancel={e => { pointers.current.delete(e.pointerId); finishDrag(true) }}
        onPointerLeave={() => { if (!dragRef.current) sendCursor(true) }}
        onContextMenu={e => { e.preventDefault(); if (!e.shiftKey) openMenuAt(e.clientX, e.clientY, e.target) }}
        onDragOver={e => { if (canEdit && e.dataTransfer.types.includes('Files')) e.preventDefault() }}
        onDrop={e => { if (!canEdit) return; e.preventDefault(); const p = toWorld(e.clientX, e.clientY); [...e.dataTransfer.files].filter(f => f.type.startsWith('image/')).forEach((f, i) => addImage(f, { x: p.x + i * 40, y: p.y + i * 40 })) }}>
        <div className="wb-world" style={{ transform: `translate(${cam.x}px, ${cam.y}px) scale(${cam.z})` }}>
          {sorted.map(o => {
            if (o.kind !== 'line' && !sel.has(o.id) && editing !== o.id && !overlaps(visible, o)) return null
            return (
              <ObjectView key={o.id} o={o} geom={o.kind === 'line' ? linePath(o, objs) : undefined} editing={editing === o.id} onEditDone={onEditDone}
                imgSrc={o.kind === 'image' ? conn.imgSrc(o) : undefined} remote={remoteSel.get(o.id)} showAuthor={showAuthor} />
            )
          })}
          {/* dots and counts sit above everything */}
          {(showCounts || votes.open) && sorted.map(o => {
            const n = showCounts ? votes.counts[o.id] : undefined, m = votes.open ? votes.mine[o.id] : undefined
            if (!n && !m) return null
            return <div key={'v' + o.id} className="wb-vote-at" style={{ left: o.x + o.w, top: o.y, ['--vs' as string]: `${clamp(1 / cam.z, 0.6, 3)}` }}><VoteBadge votes={n} mine={m} /></div>
          })}
          {/* reactions under the things */}
          {sorted.map(o => {
            const r = reactions[o.id]
            if (!r?.length) return null
            return (
              <div key={'r' + o.id} className="wb-react-at" style={{ left: o.x + o.w, top: o.y + o.h, ['--vs' as string]: `${clamp(1 / cam.z, 0.6, 3)}` }}>
                {r.map(x => <button key={x.emoji} type="button" className={`wb-react ${x.voters.includes(me.voter) ? 'mine' : ''}`} data-react={x.emoji} data-obj={o.id} title={x.names.join(', ')}>{x.emoji}<b>{x.names.length}</b></button>)}
              </div>
            )
          })}
          {/* what find found */}
          {find && matches.map((o, i) => <div key={'f' + o.id} className={`wb-match ${i === find.i ? 'cur' : ''}`} style={{ left: o.x - 6 / cam.z, top: o.y - 6 / cam.z, width: o.w + 12 / cam.z, height: o.h + 12 / cam.z, borderWidth: (i === find.i ? 3 : 2) / cam.z }} />)}
          {/* the dots to join things with an arrow */}
          {canEdit && tool === 'select' && present === null && !editing && !dragRef.current && (() => {
            const plusOn = selList.length === 1 && ['sticky', 'shape'].includes(selList[0].kind) && cam.z > 0.25
            const id = hoverId && objs.has(hoverId) && !(plusOn && selList[0].id === hoverId) ? hoverId : !plusOn && selList.length === 1 && CONNECTABLE.has(selList[0].kind) ? selList[0].id : null
            const o = id ? objs.get(id) : undefined
            if (!o || o.data.locked || cam.z < 0.2) return null
            const off = 14 / cam.z
            const pts: [string, number, number][] = [['t', o.x + o.w / 2, o.y - off], ['r', o.x + o.w + off, o.y + o.h / 2], ['b', o.x + o.w / 2, o.y + o.h + off], ['l', o.x - off, o.y + o.h / 2]]
            return pts.map(([k, x, y]) => <span key={k} className="wb-dot" data-dot={k} data-obj={o.id} title="Drag to draw an arrow from here" style={{ left: x, top: y, ['--h' as string]: `${12 / cam.z}px` }} />)
          })()}
          {/* the selection */}
          {selBox && present === null && !oneLine && (
            <div className={`wb-sel ${selList.some(o => o.data.locked) ? 'locked' : ''} ${selList.length > 1 && selList.every(o => o.data.group && o.data.group === selList[0].data.group) ? 'grouped' : ''}`} style={{ left: selBox.x, top: selBox.y, width: selBox.w, height: selBox.h, ['--h' as string]: `${h}px`, ['--bw' as string]: `${1.5 / cam.z}px`, ...(selList.length === 1 && selList[0].data.rot ? { transform: `rotate(${selList[0].data.rot}deg)` } : {}) }}>
              {canEdit && !editing && selList.length === 1 && !['line', 'frame', 'pen'].includes(selList[0].kind) && !selList[0].data.locked && <span className="wb-rot" data-rot="1" title="Turn (Shift: 15° steps)" style={{ ['--s' as string]: `${18 / cam.z}px` }}><Icon name="rotate" size={12} /></span>}
              {canEdit && !editing && !selList.every(o => o.data.locked) && (selList.length === 1 && selList[0].kind === 'text' ? ['w', 'e', 'nw', 'ne', 'sw', 'se'] : ['nw', 'ne', 'sw', 'se', ...(selList.length === 1 && ['shape', 'frame'].includes(selList[0].kind) ? ['n', 's', 'w', 'e'] : [])]).map(hd => <span key={hd} className={`wb-handle h-${hd}`} data-handle={hd} />)}
              {canEdit && !editing && selList.length === 1 && ['sticky', 'shape'].includes(selList[0].kind) && !selList[0].data.locked && cam.z > 0.25 && ['r', 'd', 'l', 'u'].map(dir => (
                <button key={dir} type="button" className={`wb-plus p-${dir}`} data-plus={dir} title="Click: another one here · drag: an arrow from here" style={{ ['--s' as string]: `${22 / cam.z}px` }}>+</button>
              ))}
            </div>
          )}
          {oneLine && present === null && (() => {
            const g = linePath(oneLine, objs)
            return canEdit ? <>
              <span className="wb-end" data-end="a" data-line={oneLine.id} style={{ left: g.a.x, top: g.a.y, ['--h' as string]: `${h * 1.3}px` }} />
              <span className="wb-end" data-end="b" data-line={oneLine.id} style={{ left: g.b.x, top: g.b.y, ['--h' as string]: `${h * 1.3}px` }} />
            </> : null
          })()}
          {hoverTarget && objs.get(hoverTarget) && (() => { const r = rectOf(objs.get(hoverTarget)!); return <div className="wb-target" style={{ left: r.x - 4 / cam.z, top: r.y - 4 / cam.z, width: r.w + 8 / cam.z, height: r.h + 8 / cam.z, borderWidth: 2 / cam.z }} /> })()}
          {marquee && <div className="wb-marquee" style={{ left: marquee.x, top: marquee.y, width: marquee.w, height: marquee.h, borderWidth: 1 / cam.z }} />}
          {guides.map((g, i) => g.x !== undefined
            ? <div key={i} className="wb-guide" style={{ left: g.x, top: visible.y, width: 1 / cam.z, height: visible.h }} />
            : <div key={i} className="wb-guide" style={{ top: g.y, left: visible.x, height: 1 / cam.z, width: visible.w }} />)}
        </div>
        {/* other people's pointers */}
        {settings?.cursors !== false && [...cursors.entries()].map(([k, c]) => (
          <div key={k} className="wb-cursor" style={{ transform: `translate(${cam.x + c.x * cam.z}px, ${cam.y + c.y * cam.z}px)`, ['--pc' as string]: c.colour }}>
            <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden><path d="M4 3l7 17 2.5-7.5L21 10z" fill={c.colour} stroke="#fff" strokeWidth="1.5" strokeLinejoin="round" /></svg>
            <span>{c.name}</span>
          </div>
        ))}
        {/* comment pins */}
        {present === null && threads.filter(t => showResolved || !t.root.resolved || openThread === t.root.id).map(t => {
          const at = pinAt(t, objs)
          return (
            <button key={t.root.id} type="button" className={`wb-pin ${t.root.resolved ? 'resolved' : ''} ${openThread === t.root.id ? 'open' : ''}`} data-pin={t.root.id}
              style={{ transform: `translate(${cam.x + at.x * cam.z}px, ${cam.y + at.y * cam.z}px)` }} title={`${t.root.author}: ${t.root.body.slice(0, 80)}`}>
              <Avatar name={t.root.author} size={24} />{t.replies.length > 0 && <i>{t.replies.length + 1}</i>}
            </button>
          )
        })}
        {newThread && <span className="wb-pin new" style={{ transform: `translate(${cam.x + newThread.x * cam.z}px, ${cam.y + newThread.y * cam.z}px)` }}><Icon name="plus" size={14} /></span>}
        {sorted.length === 0 && canEdit && (
          <div className="wb-empty">
            <p><b>An empty board.</b> Double-click (or double-tap) anywhere for a sticky note, or pick a tool on the left.</p>
            <p className="muted small">Paste a list and each line becomes a note. Drop pictures straight on the board.</p>
          </div>
        )}
      </div>

      {/* ---- tools */}
      {present === null && (
        <nav className="wb-tools" aria-label="Tools" onPointerDown={e => e.stopPropagation()}>
          <ToolBtn icon="cursor" label="Select" k="V" on={tool === 'select'} onClick={() => setTool('select')} />
          <ToolBtn icon="hand" label="Move the board" k="H" on={tool === 'hand'} onClick={() => setTool('hand')} />
          {(canEdit || !guest) && <ToolBtn icon="comment" label="Comment" k="C" on={tool === 'comment'} onClick={() => setTool(tool === 'comment' ? 'select' : 'comment')} />}
          {canEdit && <>
            <span className="wb-tools-sep" />
            <ToolBtn icon="sticky" label="Sticky note" k="N" on={tool === 'sticky'} onClick={() => { setTool('sticky'); setFlyout(flyout === 'sticky' ? null : 'sticky') }} dot={paper(stickyColor).bg} />
            <ToolBtn icon="text" label="Text" k="T" on={tool === 'text'} onClick={() => setTool('text')} />
            <ToolBtn icon={SHAPES.find(s => s.form === shapeForm)?.icon || 'square'} label="Shape" k="S" on={tool === 'shape'} onClick={() => { setTool('shape'); setFlyout(flyout === 'shape' ? null : 'shape') }} more />
            <ToolBtn icon={lineCurve === 'straight' ? 'arrow-line' : lineCurve === 'elbow' ? 'elbow' : 'curve'} label="Line or arrow" k="L" on={tool === 'line'} onClick={() => { setTool('line'); setFlyout(flyout === 'line' ? null : 'line') }} more />
            <ToolBtn icon="pen" label="Pen" k="P" on={tool === 'pen'} onClick={() => { setTool('pen'); setFlyout(flyout === 'pen' ? null : 'pen') }} more dot={pen.color} />
            <ToolBtn icon="eraser" label="Eraser (pen lines)" k="E" on={tool === 'eraser'} onClick={() => setTool('eraser')} />
            <ToolBtn icon="frame" label="Frame" k="F" on={tool === 'frame'} onClick={() => setTool('frame')} />
            <ToolBtn icon="image" label="Picture" k="I" on={false} onClick={pickImage} />
            <ToolBtn icon="sticker" label="Sticker" k="K" on={tool === 'sticker'} onClick={() => { setTool('sticker'); setFlyout(flyout === 'sticker' ? null : 'sticker') }} more />
            <span className="wb-tools-sep" />
            <ToolBtn icon="undo" label="Undo" k="⌘Z" on={false} disabled={!undoStack.current.length} onClick={undo} />
            <ToolBtn icon="redo" label="Redo" k="⇧⌘Z" on={false} disabled={!redoStack.current.length} onClick={redo} />
          </>}
          {flyout === 'sticky' && (
            <div className="wb-fly">
              <div className="wb-swatches">{STICKY_KEYS.map(c => <button key={c} type="button" className={`wb-sw ${c === stickyColor ? 'on' : ''}`} style={{ background: STICKY[c].bg }} title={STICKY[c].name} aria-label={STICKY[c].name} onClick={() => { setStickyColor(c); setFlyout(null) }} />)}</div>
              <p className="muted small">Click the board to put a note there. Tip: double-click anywhere.</p>
            </div>
          )}
          {flyout === 'shape' && (
            <div className="wb-fly wb-fly-grid">
              {SHAPES.map(s => <button key={s.form} type="button" className={`btn ghost icon-only ${s.form === shapeForm ? 'on' : ''}`} title={s.name} aria-label={s.name} onClick={() => { setShapeForm(s.form); setFlyout(null) }}><Icon name={s.icon} size={18} /></button>)}
            </div>
          )}
          {flyout === 'line' && (
            <div className="wb-fly wb-fly-grid">
              {([['straight', 'arrow-line', 'Straight'], ['elbow', 'elbow', 'Elbow'], ['curve', 'curve', 'Curved']] as const).map(([v, ic, n]) => <button key={v} type="button" className={`btn ghost icon-only ${v === lineCurve ? 'on' : ''}`} title={n} aria-label={n} onClick={() => { setLineCurve(v); setFlyout(null) }}><Icon name={ic} size={18} /></button>)}
              <p className="muted small">Drag from one thing to another: the line stays joined when they move.</p>
            </div>
          )}
          {flyout === 'sticker' && (
            <div className="wb-fly wb-fly-pen">
              <EmojiGrid list={STICKERS} cols={5} onPick={e => { setSticker(e); setFlyout(null) }} />
              <p className="muted small">Then click the board: {sticker}</p>
            </div>
          )}
          {flyout === 'pen' && (
            <div className="wb-fly wb-fly-pen">
              <div className="wb-swatches">{INKS.map(c => <button key={c} type="button" className={`wb-sw round ${c === pen.color ? 'on' : ''}`} style={{ background: c }} aria-label={c} onClick={() => setPen(p => ({ ...p, color: c }))} />)}</div>
              <div className="wb-widths" role="radiogroup" aria-label="Thickness">
                {([[2, 'Thin'], [4, 'Medium'], [8, 'Thick']] as const).map(([w, n]) => (
                  <button key={w} type="button" role="radio" aria-checked={pen.width === w} className={`wb-width ${pen.width === w ? 'on' : ''}`} title={n} aria-label={n} onClick={() => setPen(p => ({ ...p, width: w }))}>
                    <i style={{ width: w * 2 + 2, height: w * 2 + 2, background: pen.color === '#ffffff' ? '#c8ccd2' : pen.color, opacity: pen.highlight ? 0.45 : 1 }} />
                  </button>
                ))}
              </div>
              <Segmented size="sm" className="wb-full" value={pen.highlight ? 'hl' : 'pen'} onChange={v => setPen(p => ({ ...p, highlight: v === 'hl' }))} options={[{ value: 'pen', label: 'Pen' }, { value: 'hl', label: 'Highlighter' }]} />
            </div>
          )}
        </nav>
      )}
      <input ref={fileInput} type="file" accept="image/*" multiple hidden onChange={e => { const c = viewCenter(); [...(e.target.files || [])].forEach((f, i) => addImage(f, { x: c.x + i * 40, y: c.y + i * 40 })); e.target.value = '' }} />

      {/* ---- what is picked */}
      {ctxPos && selList.length > 0 && (
        <ContextBar pos={ctxPos} list={selList} canEdit={canEdit} guest={guest}
          onColour={colourAll} onStyle={restyle} onFront={() => order(true)} onBack={() => order(false)} onDuplicate={() => duplicate()} onDelete={del} onLock={toggleLock}
          onGrid={() => arrangeGrid(false)} onByColour={() => arrangeGrid(true)} onAlign={align} onFocus={() => { const r = union(selList.map(o => rectOf(o, objs))); if (r) animateTo(camFor(r, 120, 2)) }}
          onTasks={tasksOK ? () => toApp('/tasks', { ids: selList.map(o => o.id) }) : undefined} tasksHint={needs(tasksOK, 'Tasks')}
          onAI={!guest ? () => setPanel('ai') : undefined} onVote={votes.open ? () => castVote(selList[0].id, 1) : undefined} onUnvote={votes.open && votes.mine[selList[0].id] ? () => castVote(selList[0].id, -1) : undefined}
          onEdit={() => setEditing(selList[0].id)}
          onGroup={group} onUngroup={ungroup} allTags={allTags} onTag={toggleTag} onByTag={groupByTag}
          onReact={e => react(selList[0].id, e)} myReactions={(reactions[selList[0].id] || []).filter(r => r.voters.includes(me.voter)).map(r => r.emoji)}
          onComment={() => { const o = selList[0]; setOpenThread(null); setNewThread({ object_id: o.kind === 'line' ? '' : o.id, x: Math.round(o.x + o.w), y: Math.round(o.y) }) }} />
      )}

      {/* ---- zoom */}
      {present === null && (
        <div className="wb-zoom" onPointerDown={e => e.stopPropagation()}>
          <span className="muted small wb-count-notes" title="Sticky notes on the board">{noteCount} {noteCount === 1 ? 'note' : 'notes'}</span>
          <button type="button" className="btn ghost icon-only sm" onClick={() => zoomBy(0.8)} aria-label="Zoom out" title="Zoom out (−)"><Icon name="minus" size={16} /></button>
          <button type="button" className="wb-zoom-val" onClick={() => fitAll()} title="Fit everything (⇧1)">{Math.round(cam.z * 100)}%</button>
          <button type="button" className="btn ghost icon-only sm" onClick={() => zoomBy(1.25)} aria-label="Zoom in" title="Zoom in (+)"><Icon name="plus" size={16} /></button>
          <button type="button" className="btn ghost icon-only sm" onClick={() => fitAll()} aria-label="Fit everything" title="Fit everything (⇧1)"><Icon name="maximize" size={15} /></button>
        </div>
      )}

      {/* ---- present */}
      {present !== null && (
        <div className="wb-present-bar">
          <button type="button" className="btn ghost icon-only" onClick={() => presentGo(-1)} disabled={present === 0} aria-label="Back"><Icon name="chevron-left" size={18} /></button>
          <span>{frames[present]?.data.title || 'Frame'} · {present + 1} / {frames.length}</span>
          <button type="button" className="btn ghost icon-only" onClick={() => presentGo(1)} disabled={present >= frames.length - 1} aria-label="Next"><Icon name="chevron-right" size={18} /></button>
          <button type="button" className="btn sm" onClick={() => { setPresent(null); fitAll() }}>Stop</button>
        </div>
      )}

      {/* ---- comments beside their pins */}
      {openThread !== null && present === null && (() => {
        const t = threads.find(x => x.root.id === openThread)
        if (!t) return null
        const at = pinAt(t, objs)
        const pos = { left: clamp(cam.x + at.x * cam.z + 20, 8, vpSize.w - 330), top: vpTop + clamp(cam.y + at.y * cam.z + 10, 8, Math.max(8, vpSize.h - 430)) }
        return <ThreadCard thread={t} pos={pos} people={mentionPeople} req={req} myVoter={me.voter} canResolve={canEdit || t.root.voter === me.voter} isOwner={board.role === 'owner'} guest={guest}
          onClose={() => setOpenThread(null)} onChanged={loadComments} />
      })()}
      {newThread && present === null && (
        <NewThread at={newThread} people={mentionPeople} req={req} pos={{ left: clamp(cam.x + newThread.x * cam.z + 20, 8, vpSize.w - 330), top: vpTop + clamp(cam.y + newThread.y * cam.z + 10, 8, Math.max(8, vpSize.h - 230)) }}
          onClose={() => setNewThread(null)} onCreated={id => { setNewThread(null); loadComments(); setOpenThread(id) }} />
      )}
      {/* ---- formatting while writing */}
      {editing && objs.get(editing) && ['sticky', 'shape', 'text'].includes(objs.get(editing)!.kind) && present === null && (() => {
        const o = objs.get(editing)!
        const pos = barAt(o, 190)
        return <FormatBar pos={pos} size={o.data.size || 0} onSize={v => { const cur = objsRef.current.get(o.id); if (cur) commit([withData(cur, { size: v || undefined })]) }} />
      })()}
      <FloatLayer floats={floats} />
      {ctxMenu && <ContextMenu at={{ x: ctxMenu.x, y: ctxMenu.y }} items={menuItems(ctxMenu)} width={260} onClose={() => setCtxMenu(null)} />}

      {/* ---- panels */}
      {panel === 'share' && !guest && <SharePanel board={board} members={members} setMembers={setMembers} shareURL={shareURL} setShareURL={setShareURL} onBoard={b => setBoard(x => ({ ...x, ...b }))} onClose={() => setPanel(null)}
        onLeave={async () => { try { await api.del(`${conn.base}/members/${(await api.get<{ id: number }>(`${V1}/auth/me`)).id}`); navigate('/') } catch (e) { toast(errMsg(e), 'err') } }} />}
      {panel === 'vote' && <VotePanel votes={votes} canEdit={canEdit} objs={objs} onStart={(per, hide) => setVote({ action: 'start', per_person: per, hide })} onEnd={() => setVote({ action: 'end' })} onClear={() => setVote({ action: 'clear' })} onFocus={id => focusOn(id)} onClose={() => setPanel(null)} />}
      {panel === 'timer' && <TimerPanel board={board} canEdit={canEdit} onSet={setTimer} onClose={() => setPanel(null)} />}
      {panel === 'frames' && <FramesPanel objs={objs} onFocus={id => focusOn(id)} onPresent={startPresent} onClose={() => setPanel(null)} />}
      {panel === 'ai' && !guest && <AIPanel base={conn.base} picked={[...sel]} canEdit={canEdit} onGroups={placeGroups} onIdeas={placeIdeas} onNote={placeNote} onClose={() => setPanel(null)} />}
      {panel === 'comments' && <CommentsPanel threads={threads} people={mentionPeople} objs={objs} showResolved={showResolved} setShowResolved={setShowResolved} onClose={() => setPanel(null)}
        onOpen={id => { const t = threads.find(x => x.root.id === id); if (t) { const at = pinAt(t, objs); animateTo(camFor({ x: at.x - 300, y: at.y - 200, w: 600, h: 400 }, 40, 1.2)); setOpenThread(id); if (mobile) setPanel(null) } }} />}
      {panel === 'history' && !guest && <HistoryPanel req={req} canEdit={canEdit} background={board.background} reloadKey={historyKey} onClose={() => setPanel(null)} />}
      {panel === 'chat' && !guest && <ChatPicker onClose={() => setPanel(null)} onSend={to => { setPanel(null); toApp('/chat', { to }) }} />}

      {settingsOpen && <BoardSettings board={board} onClose={() => setSettingsOpen(false)} onSave={async b => { try { await call(conn, 'PUT', '', b); setBoard(x => ({ ...x, ...b })); setSettingsOpen(false) } catch (e) { toast(errMsg(e), 'err') } }} />}
      {confirmDelete && <Confirm title="Delete this board?" message={<>“{board.title}” and everything on it goes, for everyone on it. This cannot be undone.</>} confirmLabel="Delete" onCancel={() => setConfirmDelete(false)}
        onConfirm={async () => { try { await api.del(conn.base); navigate('/') } catch (e) { toast(errMsg(e), 'err') } }} />}
      {shortcuts && <BoardShortcuts onClose={() => setShortcuts(false)} />}
    </div>
  )
}

function ToolBtn({ icon, label, k, on, onClick, disabled, more, dot }: { icon: string; label: string; k: string; on: boolean; onClick: () => void; disabled?: boolean; more?: boolean; dot?: string }) {
  return (
    <button type="button" className={`wb-tool ${on ? 'on' : ''}`} onClick={onClick} disabled={disabled} title={`${label} (${k})`} aria-label={label} aria-pressed={on}>
      <Icon name={icon} size={20} />
      {dot && <i className="wb-tool-dot" style={{ background: dot }} />}
      {more && <i className="wb-tool-more" />}
    </button>
  )
}

// ---- the bar over what is picked

function ContextBar({ pos, list, canEdit, guest, onColour, onStyle, onFront, onBack, onDuplicate, onDelete, onLock, onGrid, onByColour, onAlign, onFocus, onTasks, tasksHint, onAI, onVote, onUnvote, onEdit,
  onGroup, onUngroup, allTags, onTag, onByTag, onReact, myReactions, onComment }: {
  pos: { left: number; top: number }; list: Obj[]; canEdit: boolean; guest: boolean
  onColour: (c: string) => void; onStyle: (d: Partial<ObjData>, only?: (o: Obj) => boolean) => void; onFront: () => void; onBack: () => void; onDuplicate: () => void; onDelete: () => void; onLock: () => void
  onGrid: () => void; onByColour: () => void; onAlign: (h: 'left' | 'top' | 'hcenter' | 'vcenter') => void; onFocus: () => void
  onTasks?: () => void; tasksHint?: string; onAI?: () => void; onVote?: () => void; onUnvote?: () => void; onEdit: () => void
  onGroup: () => void; onUngroup: () => void; allTags: string[]; onTag: (t: string, add: boolean) => void; onByTag: () => void
  onReact: (e: string) => void; myReactions: string[]; onComment: () => void
}) {
  const [open, setOpen] = useState<'colour' | 'shape' | 'ink' | 'tags' | 'react' | 'size' | 'emoji' | null>(null)
  const grouped = list.length > 1 && list.every(o => o.data.group && o.data.group === list[0].data.group)
  const anyGroup = list.some(o => o.data.group)
  const taggable = list.filter(o => o.kind === 'sticky' || o.kind === 'shape')
  const tagState = (t: string): 'all' | 'some' | 'none' => { const n = taggable.filter(o => o.data.tags?.includes(t)).length; return n === 0 ? 'none' : n === taggable.length ? 'all' : 'some' }
  const reactBtn = list.length === 1 && list[0].kind !== 'line' && list[0].kind !== 'frame' && (
    <div className="wb-ctx-drop">
      <button type="button" className="btn ghost icon-only sm" onClick={() => setOpen(open === 'react' ? null : 'react')} title="React" aria-label="React"><Icon name="smile" size={16} /></button>
      {open === 'react' && <div className="wb-ctx-pop"><EmojiGrid list={REACTIONS} cols={4} onPick={e => { onReact(e); setOpen(null) }} />{myReactions.length > 0 && <p className="muted small">Click yours again to take it back.</p>}</div>}
    </div>
  )
  const kinds = new Set(list.map(o => o.kind))
  const one = list.length === 1 ? list[0] : null
  const locked = list.every(o => o.data.locked)
  const coloured = kinds.has('sticky') || kinds.has('shape') || kinds.has('frame')
  const cur = one ? (one.data.color || one.data.fill || '') : ''
  const textish = list.filter(o => ['sticky', 'text', 'shape'].includes(o.kind) && textOf(o))
  if (!canEdit) {
    return (
      <div className="wb-ctx" style={pos} onPointerDown={e => e.stopPropagation()}>
        {onVote && <button type="button" className="btn ghost sm" onClick={onVote}><Icon name="vote" size={15} />Vote</button>}
        {onUnvote && <button type="button" className="btn ghost sm" onClick={onUnvote}>Take my vote back</button>}
        <button type="button" className="btn ghost icon-only sm" onClick={onFocus} title="Zoom to it" aria-label="Zoom to it"><Icon name="zoom-in" size={16} /></button>
        {reactBtn}
        {!guest && list.length === 1 && <button type="button" className="btn ghost icon-only sm" onClick={onComment} title="Comment" aria-label="Comment"><Icon name="comment" size={16} /></button>}
        {one?.author && <span className="muted small wb-ctx-author">by {one.author}</span>}
      </div>
    )
  }
  const more = (): MenuItem[] => [
    { label: 'Zoom to it', icon: 'zoom-in', hint: '⇧2', onSelect: onFocus },
    ...(list.length > 1 ? [
      { label: 'Arrange in a grid', icon: 'grid', onSelect: onGrid },
      { label: 'Sort into columns by colour', icon: 'grid-sort', onSelect: onByColour },
      { label: 'Line up left edges', icon: 'align-left', onSelect: () => onAlign('left') },
      { label: 'Line up centres', icon: 'align-center', onSelect: () => onAlign('hcenter') },
      { label: 'Line up top edges', icon: 'arrow-up', onSelect: () => onAlign('top') },
    ] : []),
    ...(list.length > 1 && !grouped ? [{ label: 'Group', icon: 'group', hint: '⌘G', onSelect: onGroup }] : []),
    ...(anyGroup ? [{ label: 'Ungroup', icon: 'ungroup', hint: '⇧⌘G', onSelect: onUngroup }] : []),
    ...(taggable.length > 1 ? [{ label: 'Sort into frames by tag', icon: 'tag', onSelect: onByTag }] : []),
    'sep',
    { label: 'Bring to front', icon: 'bring-front', hint: ']', onSelect: onFront },
    { label: 'Send to back', icon: 'send-back', hint: '[', onSelect: onBack },
    { label: locked ? 'Unlock' : 'Lock in place', icon: 'lock', onSelect: onLock },
    ...(!guest ? ['sep' as const,
      { label: `Make ${textish.length > 1 ? `${textish.length} tasks` : 'a task'} in Pocket Tasks`, icon: 'check-square', disabled: !onTasks || !textish.length, hint: onTasks ? undefined : 'off', onSelect: () => onTasks?.() },
      ...(onAI ? [{ label: 'Pocket AI on these', icon: 'sparkles', onSelect: onAI }] : []),
    ] : []),
  ]
  return (
    <div className="wb-ctx" style={pos} onPointerDown={e => e.stopPropagation()} role="toolbar" aria-label="What is picked">
      {onVote && <button type="button" className="btn ghost sm" onClick={onVote}><Icon name="vote" size={15} />Vote</button>}
      {onUnvote && <button type="button" className="btn ghost sm" onClick={onUnvote}>−1</button>}
      {coloured && !locked && (
        <div className="wb-ctx-drop">
          <button type="button" className="wb-ctx-colour" onClick={() => setOpen(open === 'colour' ? null : 'colour')} title="Colour" aria-label="Colour" style={{ background: paper(cur || 'light-yellow').bg }} />
          {open === 'colour' && <div className="wb-ctx-pop wb-swatches">
            {kinds.has('shape') && <button type="button" className="wb-sw none" title="No fill" aria-label="No fill" onClick={() => { onStyle({ fill: 'none' }, o => o.kind === 'shape'); setOpen(null) }} />}
            {(kinds.has('shape') && !kinds.has('sticky') ? [...STICKY_KEYS, 'white'] : STICKY_KEYS).map(c => <button key={c} type="button" className={`wb-sw ${c === cur ? 'on' : ''}`} style={{ background: STICKY[c].bg }} title={STICKY[c].name} aria-label={STICKY[c].name} onClick={() => { onColour(c); setOpen(null) }} />)}
          </div>}
        </div>
      )}
      {list.every(o => o.kind === 'sticker') && !locked && (
        <div className="wb-ctx-drop">
          <button type="button" className="btn ghost sm wb-sticker-btn" onClick={() => setOpen(open === 'emoji' ? null : 'emoji')} title="Change the sticker" aria-label="Change the sticker">{one?.data.emoji || '⭐'}<Icon name="chevron-down" size={12} /></button>
          {open === 'emoji' && <div className="wb-ctx-pop"><EmojiGrid list={STICKERS} cols={8} onPick={e => { onStyle({ emoji: e }, o => o.kind === 'sticker'); setOpen(null) }} /></div>}
        </div>
      )}
      {one?.kind === 'shape' && !locked && (
        <div className="wb-ctx-drop">
          <button type="button" className="btn ghost icon-only sm" onClick={() => setOpen(open === 'shape' ? null : 'shape')} title="Shape" aria-label="Shape"><Icon name={SHAPES.find(s => s.form === one.data.shape)?.icon || 'square'} size={16} /></button>
          {open === 'shape' && <div className="wb-ctx-pop wb-fly-grid">{SHAPES.map(s => <button key={s.form} type="button" className={`btn ghost icon-only ${s.form === one.data.shape ? 'on' : ''}`} title={s.name} aria-label={s.name} onClick={() => { onStyle({ shape: s.form }); setOpen(null) }}><Icon name={s.icon} size={18} /></button>)}</div>}
        </div>
      )}
      {(kinds.has('text') || kinds.has('line') || kinds.has('pen')) && !locked && (
        <div className="wb-ctx-drop">
          <button type="button" className="wb-ctx-colour round" onClick={() => setOpen(open === 'ink' ? null : 'ink')} title="Colour of the text or line" aria-label="Ink colour" style={{ background: one?.data.color || 'var(--wb-ink)' }} />
          {open === 'ink' && <div className="wb-ctx-pop wb-swatches">{INKS.map(c => <button key={c} type="button" className="wb-sw round" style={{ background: c }} aria-label={c} onClick={() => { onStyle({ color: c }, o => ['text', 'line', 'pen'].includes(o.kind)); setOpen(null) }} />)}</div>}
        </div>
      )}
      {one?.kind === 'text' && !locked && <>
        <button type="button" className="btn ghost icon-only sm" onClick={() => onStyle({ size: Math.max(8, Math.round((one.data.size || 24) / 1.25)) })} title="Smaller" aria-label="Smaller"><span className="wb-aa sm">A</span></button>
        <button type="button" className="btn ghost icon-only sm" onClick={() => onStyle({ size: Math.min(400, Math.round((one.data.size || 24) * 1.25)) })} title="Bigger" aria-label="Bigger"><span className="wb-aa">A</span></button>
        <button type="button" className={`btn ghost icon-only sm ${one.data.bold ? 'on' : ''}`} onClick={() => onStyle({ bold: !one.data.bold })} title="Bold" aria-label="Bold"><Icon name="bold" size={16} /></button>
      </>}
      {one?.kind === 'line' && !locked && <>
        <Segmented size="sm" value={one.data.curve || 'straight'} onChange={v => onStyle({ curve: v })} options={[{ value: 'straight', label: <Icon name="line-plain" size={14} />, title: 'Straight' }, { value: 'elbow', label: <Icon name="elbow" size={14} />, title: 'Elbow' }, { value: 'curve', label: <Icon name="curve" size={14} />, title: 'Curved' }]} />
        <button type="button" className={`btn ghost icon-only sm ${(one.data.end ?? 'arrow') === 'arrow' ? 'on' : ''}`} onClick={() => onStyle({ end: (one.data.end ?? 'arrow') === 'arrow' ? 'none' : 'arrow' })} title="Arrow at the end" aria-label="Arrow at the end"><Icon name="arrow-line" size={16} /></button>
        <button type="button" className={`btn ghost icon-only sm ${one.data.start === 'arrow' ? 'on' : ''}`} onClick={() => onStyle({ start: one.data.start === 'arrow' ? 'none' : 'arrow' })} title="Arrow at the start" aria-label="Arrow at the start"><Icon name="arrow-left" size={16} /></button>
        <button type="button" className={`btn ghost icon-only sm ${one.data.dash ? 'on' : ''}`} onClick={() => onStyle({ dash: !one.data.dash })} title="Dashed" aria-label="Dashed"><Icon name="minus" size={16} /></button>
        <button type="button" className="btn ghost icon-only sm" onClick={onEdit} title="Label" aria-label="Label"><Icon name="text" size={16} /></button>
      </>}
      {one && ['sticky', 'shape'].includes(one.kind) && !locked && <button type="button" className="btn ghost icon-only sm" onClick={onEdit} title="Write (Enter)" aria-label="Write"><Icon name="edit" size={16} /></button>}
      {list.every(o => o.kind === 'sticky' || o.kind === 'shape') && !locked && (
        <div className="wb-ctx-drop">
          <button type="button" className="btn ghost sm wb-size-btn" onClick={() => setOpen(open === 'size' ? null : 'size')} title="Text size">{one?.data.size || 'Auto'}<Icon name="chevron-down" size={12} /></button>
          {open === 'size' && <div className="wb-ctx-pop wb-sizes">{TEXT_SIZES.map(s => <button key={s.v} type="button" className={`btn ghost sm ${(one?.data.size || 0) === s.v ? 'on' : ''}`} onClick={() => { onStyle({ size: s.v || undefined }); setOpen(null) }}>{s.label}</button>)}</div>}
        </div>
      )}
      {taggable.length > 0 && !locked && (
        <div className="wb-ctx-drop">
          <button type="button" className={`btn ghost icon-only sm ${taggable.some(o => o.data.tags?.length) ? 'on' : ''}`} onClick={() => setOpen(open === 'tags' ? null : 'tags')} title="Tags" aria-label="Tags"><Icon name="tag" size={16} /></button>
          {open === 'tags' && <TagPicker all={allTags} on={tagState} onToggle={onTag} onClose={() => setOpen(null)} />}
        </div>
      )}
      {list.length > 1 && <button type="button" className={`btn ghost icon-only sm ${grouped ? 'on' : ''}`} onClick={grouped ? onUngroup : onGroup} title={grouped ? 'Ungroup (⇧⌘G)' : 'Group (⌘G)'} aria-label={grouped ? 'Ungroup' : 'Group'}><Icon name={grouped ? 'ungroup' : 'group'} size={16} /></button>}
      {reactBtn}
      {!guest && list.length === 1 && <button type="button" className="btn ghost icon-only sm" onClick={onComment} title="Comment" aria-label="Comment"><Icon name="comment" size={16} /></button>}
      {!guest && textish.length > 0 && <button type="button" className="btn ghost icon-only sm" disabled={!onTasks} onClick={onTasks} title={onTasks ? `Make ${textish.length > 1 ? 'tasks' : 'a task'} in Pocket Tasks` : tasksHint} aria-label="Make tasks"><Icon name="check-square" size={16} /></button>}
      <button type="button" className="btn ghost icon-only sm" onClick={onDuplicate} title="Copy (⌘D)" aria-label="Copy"><Icon name="copy" size={16} /></button>
      {!locked && <button type="button" className="btn ghost icon-only sm" onClick={onDelete} title="Delete (Del)" aria-label="Delete"><Icon name="trash" size={16} /></button>}
      {locked && <button type="button" className="btn ghost icon-only sm on" onClick={onLock} title="Unlock" aria-label="Unlock"><Icon name="lock" size={16} /></button>}
      <MenuButton label="More" className="btn ghost icon-only sm" align="right" width={260} items={more}><Icon name="more" size={16} /></MenuButton>
      {one?.author && <span className="muted small wb-ctx-author">{one.author}</span>}
    </div>
  )
}

function BoardSettings({ board, onClose, onSave }: { board: Board; onClose: () => void; onSave: (b: Partial<Board>) => void }) {
  const [title, setTitle] = useState(board.title)
  const [about, setAbout] = useState(board.about)
  const [bg, setBg] = useState(board.background)
  return (
    <Modal title="Board settings" onClose={onClose} size="sm" footer={<><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="button" className="btn primary" onClick={() => onSave({ title, about, background: bg })}>Save</button></>}>
      <div className="stack">
        <label className="field"><span className="field-lbl">Name</span><input value={title} onChange={e => setTitle(e.target.value)} maxLength={120} /></label>
        <label className="field"><span className="field-lbl">What it is for</span><input value={about} onChange={e => setAbout(e.target.value)} maxLength={300} placeholder="A line for the people on it" /></label>
        <div className="field"><span className="field-lbl">Background</span><Segmented value={bg} onChange={setBg} options={[{ value: 'dots', label: 'Dots' }, { value: 'grid', label: 'Grid' }, { value: 'plain', label: 'Plain' }]} /></div>
      </div>
    </Modal>
  )
}

function BoardShortcuts({ onClose }: { onClose: () => void }) {
  const rows: [string, string][] = [
    ['V', 'Select'], ['H or hold Space', 'Move the board'], ['C', 'Comment'], ['K', 'Sticker'], ['⌘F', 'Find on the board (#tag for tags)'], ['⌘G / ⇧⌘G', 'Group / ungroup'], ['Alt-click', 'One thing inside a group'], ['⌘B ⌘I ⌘U', 'Bold, italic, underline while writing'], ['N', 'Sticky note'], ['T', 'Text'], ['S', 'Shape'], ['L', 'Line or arrow'], ['P', 'Pen'], ['E', 'Eraser'], ['F', 'Frame'], ['I', 'Picture'],
    ['Double-click', 'New sticky note / write on it'], ['Enter', 'Write on what is picked'], ['Tab (while writing)', 'Next sticky note to the right'], ['Esc', 'Done / pick nothing'],
    ['⌘Z / ⇧⌘Z', 'Undo / redo'], ['⌘C ⌘X ⌘V', 'Copy, cut, paste (a pasted list becomes notes)'], ['⌘D', 'Make a copy'], ['⌘A', 'Pick everything'], ['Del', 'Delete'],
    ['Arrows (⇧ ×10)', 'Nudge'], [']  [', 'Bring to front / send to back'], ['Shift-drag', 'Add to the pick; keep proportions when resizing'], ['Alt while dragging', 'No lining up'],
    ['Scroll / pinch', 'Move / zoom'], ['⌘ scroll, + −', 'Zoom'], ['⇧1', 'Fit everything'], ['⇧2', 'Zoom to what is picked'], ['⌘0', '100%'],
  ]
  return (
    <Modal title="Keyboard shortcuts" onClose={onClose}>
      <dl className="wb-keys">{rows.map(([k, v]) => <div key={k}><dt><kbd>{k}</kbd></dt><dd>{v}</dd></div>)}</dl>
    </Modal>
  )
}

// keep the type-only imports used
export type { Cam, Drag }
void frameOf; void nearLine
