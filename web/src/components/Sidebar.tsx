// Left column: a new board, the lists of boards, and the ones opened last.
import { useCallback, useEffect, useState } from 'react'
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { useOnChange } from '../lib/data'
import type { BoardRow, Folder } from '../lib/types'
import { Icon } from './icons'

export function Sidebar({ onCreate, onNavigate }: { onCreate: () => void; onNavigate: () => void }) {
  const { status, settings } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const [rows, setRows] = useState<BoardRow[] | null>(null)
  const [folders, setFolders] = useState<Folder[]>([])
  const load = useCallback(() => {
    api.get<{ boards: BoardRow[] }>(`${V1}/boards?plain=1`).then(r => setRows(r.boards)).catch(() => {})
    api.get<{ folders: Folder[] }>(`${V1}/folders`).then(r => setFolders(r.folders)).catch(() => {})
  }, [])
  useEffect(() => { load() }, [load, loc.pathname])
  useOnChange(load)

  const title = settings?.title || status?.title || 'Pocket Whiteboard'
  const path = loc.pathname
  const show = new URLSearchParams(loc.search).get('show')
  const folder = Number(new URLSearchParams(loc.search).get('folder')) || 0
  const l = rows || []
  const nav: { to: string; label: string; icon: string; active: boolean; count?: number }[] = [
    { to: '/', label: 'All boards', icon: 'grid', active: path === '/' && !show && !folder },
    { to: '/?show=starred', label: 'Starred', icon: 'star', active: path === '/' && show === 'starred', count: l.filter(b => b.starred).length || undefined },
    { to: '/?show=mine', label: 'Made by me', icon: 'user', active: path === '/' && show === 'mine' },
    { to: '/?show=shared', label: 'Shared with me', icon: 'users', active: path === '/' && show === 'shared', count: l.filter(b => b.role !== 'owner').length || undefined },
  ]
  const recent = [...l].sort((a, b) => (b.opened_at || b.updated_at).localeCompare(a.opened_at || a.updated_at)).slice(0, 7)
  return (
    <aside className="sidebar" aria-label="Pocket Whiteboard">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title">{title !== 'Pocket Whiteboard' ? title : <>Pocket <b>Whiteboard</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^v?\d+\.\d+/.test(status.version) ? 'v' + status.version.replace(/^v/, '').replace(/[-+].*$/, '') : status.version}</span>}
      </div>
      <button type="button" className="btn primary create-btn" onClick={onCreate} title="New board (N)">
        <Icon name="plus" size={15} strokeWidth={2.2} />New board
      </button>

      <nav className="sb-nav" aria-label="Lists">
        {nav.map(n => (
          <NavLink key={n.to} to={n.to} onClick={onNavigate} className={() => `sb-nav-item ${n.active ? 'active' : ''}`}>
            <Icon name={n.icon} size={16} />{n.label}
            {!!n.count && <span className="sb-count">{n.count}</span>}
          </NavLink>
        ))}
      </nav>

      <section className="sb-sec">
        <h4 className="sb-h sb-h-row"><span>Folders</span><button type="button" className="sb-h-btn" title="New folder" aria-label="New folder" onClick={async () => { const name = window.prompt('Folder name')?.trim(); if (!name) return; try { const r = await api.post<{ folder: Folder }>(`${V1}/folders`, { name }); load(); onNavigate(); navigate(`/?folder=${r.folder.id}`) } catch { /* shown on the page */ } }}><Icon name="plus" size={14} /></button></h4>
        {folders.map(f => (
          <NavLink key={f.id} to={`/?folder=${f.id}`} onClick={onNavigate} className={() => `sb-row sb-list ${path === '/' && folder === f.id ? 'active' : ''}`}>
            <Icon name="archive" size={15} className="sb-list-ico" /><span className="grow ellipsis">{f.name}</span>{f.boards > 0 && <span className="muted small">{f.boards}</span>}
          </NavLink>
        ))}
        {folders.length === 0 && <p className="muted small sb-empty-line">Keep boards together: + makes a folder.</p>}
      </section>

      <section className="sb-sec">
        <h4 className="sb-h"><Link to="/" onClick={onNavigate} className="sb-h-link">Recent</Link></h4>
        {recent.map(b => (
          <button key={b.id} type="button" className="sb-row sb-list" onClick={() => { onNavigate(); navigate(`/b/${b.id}`) }} title={b.title}>
            <Icon name={b.starred ? 'star' : 'sticky'} size={15} className="sb-list-ico" />
            <span className="grow ellipsis">{b.title}</span>
            {b.live > 0 && <span className="sb-live" title={`${b.live} open now`} />}
          </button>
        ))}
        {rows && recent.length === 0 && <button type="button" className="sb-row sb-list" onClick={() => { onNavigate(); onCreate() }}><Icon name="plus" size={15} className="sb-list-ico" /><span className="grow">Make your first board</span></button>}
      </section>

      <div className="sb-foot">
        <Link to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</Link>
        <button type="button" className="sb-foot-link" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('pwb:shortcuts')) }}><Icon name="keyboard" size={15} />Shortcuts</button>
      </div>
    </aside>
  )
}
