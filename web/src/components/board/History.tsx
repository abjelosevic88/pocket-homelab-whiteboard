// The board's history: versions kept while people work (go back to one),
// and what was deleted (put it back).
import { useCallback, useEffect, useState } from 'react'
import type { PreviewShape, Trashed, Version } from '../../lib/types'
import { ago, fmtDateTime } from '../../lib/format'
import { Confirm, Spinner, errMsg, useToast } from '../ui'
import { Icon } from '../icons'
import { Panel } from './Panels'
import { Miniature } from '../../pages/Home'
import type { Req } from './Comments'

const KIND_WORDS: Record<string, string> = { sticky: 'Sticky note', text: 'Text', shape: 'Shape', line: 'Line', frame: 'Frame', pen: 'Drawing', image: 'Picture', sticker: 'Sticker' }

export function HistoryPanel({ req, canEdit, background, onClose, reloadKey }: { req: Req; canEdit: boolean; background: string; onClose: () => void; reloadKey: number }) {
  const { toast } = useToast()
  const [data, setData] = useState<{ versions: Version[]; trash: Trashed[] } | null>(null)
  const [peek, setPeek] = useState<{ id: number; preview: PreviewShape[] } | null>(null)
  const [back, setBack] = useState<Version | null>(null)
  const [name, setName] = useState('')
  const load = useCallback(() => req<{ versions: Version[]; trash: Trashed[] }>('GET', '/history').then(setData).catch(e => toast(errMsg(e), 'err')), [req, toast])
  useEffect(() => { load() }, [load, reloadKey])
  const show = async (v: Version) => {
    if (peek?.id === v.id) { setPeek(null); return }
    try { const r = await req<{ preview: PreviewShape[] }>('GET', `/versions/${v.id}`); setPeek({ id: v.id, preview: r.preview }) } catch (e) { toast(errMsg(e), 'err') }
  }
  return (
    <Panel title="History" icon="history" onClose={onClose}>
      {!data ? <Spinner /> : <>
        {canEdit && (
          <section className="wb-sec">
            <h4>Keep this moment</h4>
            <div className="row">
              <input value={name} onChange={e => setName(e.target.value)} placeholder="Before the workshop" aria-label="Name" onKeyDown={e => e.stopPropagation()} />
              <button type="button" className="btn sm" onClick={async () => { try { await req('POST', '/versions', { name }); setName(''); toast('Kept'); load() } catch (e) { toast(errMsg(e), 'err') } }}>Keep</button>
            </div>
          </section>
        )}
        <section className="wb-sec">
          <h4>The board as it was</h4>
          <p className="muted small">Kept on its own before the first change after a quiet ten minutes, and when someone keeps a moment.</p>
          {data.versions.length === 0 && <p className="muted small">Nothing kept yet.</p>}
          <ol className="wb-frames">
            {data.versions.map(v => (
              <li key={v.id} className="wb-ver">
                <button type="button" className="wb-rank-row" onClick={() => show(v)} title={fmtDateTime(v.created_at)}>
                  <Icon name={v.name ? 'bookmark' : 'clock'} size={14} />
                  <span className="grow"><b>{v.name || ago(v.created_at)}</b><br /><span className="muted small">{v.name ? `${ago(v.created_at)} · ` : ''}{v.n} {v.n === 1 ? 'thing' : 'things'}{v.who ? ` · ${v.who}` : ''}</span></span>
                  <Icon name={peek?.id === v.id ? 'chevron-up' : 'chevron-down'} size={14} />
                </button>
                {peek?.id === v.id && (
                  <div className="wb-ver-peek">
                    <Miniature shapes={peek.preview} bg={background} />
                    {canEdit && <button type="button" className="btn sm" onClick={() => setBack(v)}><Icon name="undo" size={14} />Go back to this</button>}
                  </div>
                )}
              </li>
            ))}
          </ol>
        </section>
        <section className="wb-sec">
          <h4>Deleted</h4>
          {data.trash.length === 0 && <p className="muted small">Nothing deleted lately.</p>}
          <ol className="wb-frames">
            {data.trash.map(t => (
              <li key={t.id}>
                <div className="wb-rank-row">
                  <span className="grow ellipsis"><b>{t.text || KIND_WORDS[t.kind] || t.kind}</b><br /><span className="muted small">{t.text ? `${KIND_WORDS[t.kind] || t.kind} · ` : ''}{t.deleted_by ? `${t.deleted_by}, ` : ''}{ago(t.deleted_at)}</span></span>
                  {canEdit && <button type="button" className="btn ghost sm" onClick={async () => { try { await req('POST', `/trash/${t.id}/restore`); load() } catch (e) { toast(errMsg(e), 'err') } }}>Put back</button>}
                </div>
              </li>
            ))}
          </ol>
        </section>
      </>}
      {back && <Confirm danger={false} title="Go back to this version?" confirmLabel="Go back" message={<>The board becomes what it was {back.name ? `at “${back.name}”` : ago(back.created_at)}, for everyone on it. How it is now is kept first, so you can come back to it.</>}
        onCancel={() => setBack(null)} onConfirm={async () => { const v = back; setBack(null); try { await req('POST', `/versions/${v.id}/restore`); setPeek(null); load() } catch (e) { toast(errMsg(e), 'err') } }} />}
    </Panel>
  )
}
