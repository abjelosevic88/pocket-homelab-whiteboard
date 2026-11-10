// All boards: start one from a template, find one, see who is on it now.
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useOnChange, announceChange } from '../lib/data'
import type { BoardRow, PreviewShape, Template } from '../lib/types'
import { ago } from '../lib/format'
import { paper, previewBounds, shapePath, tint } from '../lib/board'
import { Icon } from '../components/icons'
import { Confirm, Empty, Loading, type MenuItem, MenuButton, Segmented, errMsg, useToast } from '../components/ui'
import { Select } from '../components/Select'
import { useCrumb } from '../components/Layout'

type Show = 'all' | 'starred' | 'mine' | 'shared'

/** A board in miniature: where things are, in their colours (no text). */
export function Miniature({ shapes, bg = 'dots' }: { shapes?: PreviewShape[]; bg?: string }) {
  const b = shapes && shapes.length ? previewBounds(shapes) : null
  if (!b) return <div className={`mini empty bg-${bg}`}><Icon name="sticky" size={28} /></div>
  const pad = Math.max(b.w, b.h) * 0.06 + 20
  const vb = `${b.x - pad} ${b.y - pad} ${b.w + pad * 2} ${b.h + pad * 2}`
  return (
    <div className={`mini bg-${bg}`}>
      <svg viewBox={vb} aria-hidden>
        {shapes!.map((s, i) => {
          switch (s.k) {
            case 'frame': return <g key={i}><rect x={s.x} y={s.y} width={s.w} height={s.h} fill={tint(s.c)} stroke="var(--wb-frame-line)" strokeWidth={Math.max(2, b.w / 300)} /></g>
            case 'sticky': return <rect key={i} x={s.x} y={s.y} width={s.w} height={s.h} rx={2} fill={paper(s.c || 'yellow').bg} />
            case 'shape': return <path key={i} transform={`translate(${s.x} ${s.y})`} d={shapePath(s.s as never, s.w, s.h)} fill={s.c === 'none' ? 'none' : paper(s.c || 'white').bg} stroke="var(--wb-stroke)" strokeWidth={Math.max(2, b.w / 300)} />
            case 'image': return <rect key={i} x={s.x} y={s.y} width={s.w} height={s.h} fill="var(--wb-image)" />
            case 'text': return <rect key={i} x={s.x} y={s.y + s.h * 0.3} width={Math.min(s.w, 600)} height={Math.max(6, s.h * 0.4)} rx={3} fill="var(--wb-text-bar)" />
          }
          return null
        })}
      </svg>
    </div>
  )
}

export default function Home() {
  const navigate = useNavigate()
  const { toast } = useToast()
  const [params, setParams] = useSearchParams()
  const show = (params.get('show') as Show) || 'all'
  const [boards, setBoards] = useState<BoardRow[] | null>(null)
  const [templates, setTemplates] = useState<Template[]>([])
  const [q, setQ] = useState('')
  const [sort, setSort] = useState(() => localStorage.getItem('pwb-sort') || 'opened')
  const [del, setDel] = useState<BoardRow | null>(null)
  const [making, setMaking] = useState('')
  useCrumb(undefined)
  const load = useCallback(() => { api.get<{ boards: BoardRow[] }>(`${V1}/boards`).then(r => setBoards(r.boards)).catch(e => { toast(errMsg(e), 'err'); setBoards([]) }) }, [toast])
  useEffect(() => { load() }, [load])
  useOnChange(load)
  useEffect(() => { api.get<{ templates: Template[] }>(`${V1}/templates`).then(r => setTemplates(r.templates)).catch(() => {}) }, [])
  useEffect(() => { const t = window.setInterval(() => { if (document.visibilityState === 'visible') load() }, 30000); return () => window.clearInterval(t) }, [load])

  const create = async (template: string) => {
    setMaking(template)
    try {
      const r = await api.post<{ board: BoardRow }>(`${V1}/boards`, { template })
      announceChange()
      navigate(`/b/${r.board.id}`)
    } catch (e) { toast(errMsg(e), 'err') } finally { setMaking('') }
  }

  const list = useMemo(() => {
    let l = boards || []
    if (show === 'starred') l = l.filter(b => b.starred)
    if (show === 'mine') l = l.filter(b => b.role === 'owner')
    if (show === 'shared') l = l.filter(b => b.role !== 'owner')
    if (q.trim()) { const s = q.trim().toLowerCase(); l = l.filter(b => (b.title + ' ' + b.about + ' ' + b.owner_name).toLowerCase().includes(s)) }
    const by = (b: BoardRow) => sort === 'opened' ? (b.opened_at || b.updated_at) : sort === 'changed' ? b.updated_at : sort === 'made' ? b.created_at : ''
    return [...l].sort((a, b) => sort === 'name' ? a.title.localeCompare(b.title) : by(b).localeCompare(by(a)))
  }, [boards, show, q, sort])

  const star = async (b: BoardRow) => {
    try { await api.put(`${V1}/boards/${b.id}/star`, { starred: !b.starred }); setBoards(l => l?.map(x => x.id === b.id ? { ...x, starred: !b.starred } : x) || null); announceChange() } catch (e) { toast(errMsg(e), 'err') }
  }
  const copy = async (b: BoardRow) => {
    try { const r = await api.post<{ board: BoardRow }>(`${V1}/boards/${b.id}/duplicate`); toast('Copy made'); load(); announceChange(); navigate(`/b/${r.board.id}`) } catch (e) { toast(errMsg(e), 'err') }
  }
  const remove = async (b: BoardRow) => {
    try {
      if (b.role === 'owner') await api.del(`${V1}/boards/${b.id}`)
      else await api.del(`${V1}/boards/${b.id}/members/${(await api.get<{ id: number }>(`${V1}/auth/me`)).id}`)
      setDel(null); load(); announceChange()
    } catch (e) { toast(errMsg(e), 'err') }
  }
  const titles: Record<Show, string> = { all: 'All boards', starred: 'Starred', mine: 'Made by me', shared: 'Shared with me' }
  return (
    <div className="stack loose">
      <div className="page-head">
        <div className="grow">
          <h2 className="page-title">{titles[show]}</h2>
          <p className="muted">{boards ? `${boards.length} ${boards.length === 1 ? 'board' : 'boards'}` : ' '}</p>
        </div>
        <button type="button" className="btn primary" onClick={() => create('blank')} disabled={!!making}><Icon name="plus" size={15} />New board</button>
      </div>

      {show === 'all' && templates.length > 0 && (
        <section>
          <h3 className="sec-h">Start from</h3>
          <div className="tpl-row">
            {templates.map(t => (
              <button key={t.key} type="button" className="tpl" onClick={() => create(t.key)} disabled={!!making} title={t.about}>
                <Miniature shapes={t.preview} />
                <span className="tpl-name"><span aria-hidden>{t.emoji}</span> {t.key === 'blank' ? 'Blank board' : t.title}</span>
                {making === t.key && <span className="tpl-wait">Making…</span>}
              </button>
            ))}
          </div>
        </section>
      )}

      <div className="row wrap toolbar">
        <div className="search-input grow"><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Find a board" aria-label="Find a board" /></div>
        <Segmented size="sm" value={show} onChange={v => setParams(v === 'all' ? {} : { show: v })} options={[{ value: 'all', label: 'All' }, { value: 'starred', label: 'Starred' }, { value: 'mine', label: 'Mine' }, { value: 'shared', label: 'Shared' }]} />
        <Select value={sort} onChange={v => { setSort(v); localStorage.setItem('pwb-sort', v) }} ariaLabel="Order" options={[{ value: 'opened', label: 'Last opened' }, { value: 'changed', label: 'Last changed' }, { value: 'made', label: 'Newest' }, { value: 'name', label: 'Name' }]} />
      </div>

      {!boards ? <Loading /> : list.length === 0 ? (
        <Empty title={q ? 'No board by that name' : show === 'starred' ? 'No starred boards' : show === 'shared' ? 'Nobody shared a board with you yet' : 'No boards yet'} icon="sticky">
          {show === 'all' && !q && <button type="button" className="btn primary" onClick={() => create('blank')}><Icon name="plus" size={15} />Make your first board</button>}
        </Empty>
      ) : (
        <div className="board-grid">
          {list.map(b => {
            const items: MenuItem[] = [
              { label: 'Open', icon: 'external', onSelect: () => navigate(`/b/${b.id}`) },
              { label: b.starred ? 'Unstar' : 'Star', icon: 'star', onSelect: () => star(b) },
              { label: 'Make a copy', icon: 'copy', onSelect: () => copy(b) },
              'sep',
              b.role === 'owner' ? { label: 'Delete', icon: 'trash', danger: true, onSelect: () => setDel(b) } : { label: 'Leave the board', icon: 'log-out', onSelect: () => setDel(b) },
            ]
            return (
              <article key={b.id} className="board-card">
                <a href={`/b/${b.id}`} className="board-link" onClick={e => { if (!e.metaKey && !e.ctrlKey) { e.preventDefault(); navigate(`/b/${b.id}`) } }}>
                  <Miniature shapes={b.preview} bg={b.background} />
                  <div className="board-meta">
                    <h3 className="ellipsis">{b.title}</h3>
                    <p className="muted small ellipsis">
                      {b.role !== 'owner' ? `${b.owner_name} · ` : ''}changed {ago(b.updated_at)}{b.objects_n ? ` · ${b.objects_n} ${b.objects_n === 1 ? 'thing' : 'things'}` : ''}
                    </p>
                  </div>
                </a>
                <div className="board-side">
                  {b.live > 0 && <span className="live-pill" title="Open right now"><i />{b.live} here</span>}
                  {b.members_n > 0 && <span className="muted small" title={`${b.members_n + 1} people on it`}><Icon name="users" size={13} />{b.members_n + 1}</span>}
                  {b.share_mode !== 'off' && <span className="muted small" title={`Share link: ${b.share_mode === 'edit' ? 'can edit' : 'can view'}`}><Icon name="link" size={13} /></span>}
                  <span className="spacer" />
                  <button type="button" className={`btn ghost icon-only sm star ${b.starred ? 'on' : ''}`} onClick={() => star(b)} aria-label={b.starred ? 'Unstar' : 'Star'} title={b.starred ? 'Unstar' : 'Star'}><Icon name="star" size={15} /></button>
                  <MenuButton label="Board" className="btn ghost icon-only sm" align="right" items={items}><Icon name="more" size={15} /></MenuButton>
                </div>
              </article>
            )
          })}
        </div>
      )}
      {del && <Confirm title={del.role === 'owner' ? 'Delete this board?' : 'Leave this board?'} confirmLabel={del.role === 'owner' ? 'Delete' : 'Leave'}
        message={del.role === 'owner' ? <>“{del.title}” and everything on it goes, for everyone on it.</> : <>You will no longer see “{del.title}”. {del.owner_name} can put you back on it.</>}
        onCancel={() => setDel(null)} onConfirm={() => remove(del)} />}
    </div>
  )
}
