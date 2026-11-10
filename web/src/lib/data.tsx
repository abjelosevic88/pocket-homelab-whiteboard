// The person's app states, and "something changed" between pages.
import React, { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { api, V1 } from './api'
import type { AppState } from './types'

const EVENT = 'pwb:changed'

interface DataState {
  apps: AppState[] | null
  reloadApps: () => Promise<void>
}
const Ctx = createContext<DataState>({ apps: null, reloadApps: async () => {} })
export function useData() { return useContext(Ctx) }

/** Something changed (a plan, a piece of furniture): lists reload. */
export function announceChange() { window.dispatchEvent(new CustomEvent(EVENT)) }

export function useOnChange(fn: () => void) {
  useEffect(() => {
    window.addEventListener(EVENT, fn)
    return () => window.removeEventListener(EVENT, fn)
  }, [fn])
}

export function DataProvider({ children }: { children: React.ReactNode }) {
  const [apps, setApps] = useState<AppState[] | null>(null)
  const reloadApps = useCallback(async () => {
    try { setApps((await api.get<{ apps: AppState[] }>(`${V1}/apps`)).apps) } catch { setApps(a => a || []) }
  }, [])
  useEffect(() => { reloadApps() }, [reloadApps])
  useEffect(() => {
    const t = window.setInterval(() => { if (document.visibilityState === 'visible') reloadApps() }, 300000)
    return () => window.clearInterval(t)
  }, [reloadApps])
  return <Ctx.Provider value={{ apps, reloadApps }}>{children}</Ctx.Provider>
}

/** One app's state for this person; `ok` = the scope works. */
export function useAppState() {
  const { apps } = useData()
  return (slug: string, scope?: string) => {
    const a = apps?.find(x => x.slug === slug)
    const ok = !!a && a.via !== '' && (!scope || a.on.includes(scope))
    return { app: a, ok, loading: !apps }
  }
}

/** A toast action that opens a page in another Pocket app. */
export function openAction(url?: string) { return url ? { label: 'Open', run: () => { window.open(url, '_blank', 'noopener') } } : undefined }
