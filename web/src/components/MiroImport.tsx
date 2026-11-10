// Import from Miro: a board link and a Miro access token in, a new board
// out. The token goes to the server for this one import and is not kept.
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { announceChange } from '../lib/data'
import type { Board } from '../lib/types'
import { Icon } from './icons'
import { Field, Modal, Spinner, errMsg, useToast } from './ui'
import '../styles/import.css'

interface Result { board: Board; imported: number; skipped: Record<string, number>; capped?: boolean }

const NAMES: Record<string, [string, string]> = {
  app_card: ['app card', 'app cards'], document: ['document', 'documents'], connector: ['line', 'lines'], image: ['picture', 'pictures'],
  mindmap_node: ['mind map bubble', 'mind map bubbles'], preview: ['link preview', 'link previews'], embed: ['embed', 'embeds'],
  sticky: ['note', 'notes'], shape: ['shape', 'shapes'], text: ['text', 'texts'], frame: ['frame', 'frames'], card: ['card', 'cards'], line: ['line', 'lines'],
}

function count(n: number, kind: string): string {
  const [one, many] = NAMES[kind] || [kind.replace(/_/g, ' '), kind.replace(/_/g, ' ')]
  return `${n} ${n === 1 ? one : many}`
}

/** What came over, in a sentence. */
export function importSummary(r: Pick<Result, 'imported' | 'skipped' | 'capped'>): string {
  let s = `${r.imported} ${r.imported === 1 ? 'thing' : 'things'} came over from Miro`
  const left = Object.entries(r.skipped || {}).filter(([, n]) => n > 0).map(([k, n]) => count(n, k))
  if (left.length) s += `; left out: ${left.join(', ')}`
  if (r.capped) s += '. The board was very big: only the first 5,000 things were read'
  return s + '.'
}

export function MiroImport({ onClose }: { onClose: () => void }) {
  const navigate = useNavigate()
  const { toast } = useToast()
  const [board, setBoard] = useState('')
  const [token, setToken] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const ready = board.trim() !== '' && token.trim() !== ''

  const go = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!ready || busy) return
    setBusy(true); setErr('')
    try {
      const r = await api.post<Result>(`${V1}/import/miro`, { board: board.trim(), token: token.trim() })
      setToken('')
      announceChange()
      toast(importSummary(r))
      onClose()
      navigate(`/b/${r.board.id}`)
    } catch (e) { setErr(errMsg(e)) } finally { setBusy(false) }
  }

  return (
    <Modal title="Import from Miro" onClose={() => { if (!busy) onClose() }} size="sm">
      <form onSubmit={go} className="stack miro-import">
        <p className="muted small">Makes a new board here with the notes, shapes, text, frames, cards, pictures and lines of a Miro board. The Miro board stays as it is.</p>
        <Field label="Miro board link" help="Open the board in Miro and copy the address from the browser.">
          <input value={board} onChange={e => setBoard(e.target.value)} placeholder="https://miro.com/app/board/…" autoFocus disabled={busy} spellCheck={false} autoComplete="off" />
        </Field>
        <Field label="Miro access token" help="Used only for this import. It is not saved.">
          <input type="password" value={token} onChange={e => setToken(e.target.value)} disabled={busy} autoComplete="off" spellCheck={false} />
        </Field>
        <div className="callout info">
          <Icon name="info" size={15} />
          <div>
            <b>Getting a token</b>
            <ol className="miro-steps">
              <li>In Miro, open Settings → Your apps → Create new app (any name).</li>
              <li>Give it <b>boards:read</b>.</li>
              <li>Install it on your team, then copy the access token.</li>
            </ol>
          </div>
        </div>
        {err && <div className="callout danger" role="alert"><Icon name="alert" size={15} /><span>{err}</span></div>}
        <div className="form-actions">
          <button type="button" className="btn" onClick={onClose} disabled={busy}>Cancel</button>
          <button className="btn primary" disabled={!ready || busy}>{busy ? <><Spinner />Importing…</> : 'Import'}</button>
        </div>
        {busy && <p className="muted small miro-wait">Reading the board and its pictures. A big board takes a minute.</p>}
      </form>
    </Modal>
  )
}
