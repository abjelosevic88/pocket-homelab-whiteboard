// Pointer for the adding tools: a small cross where the thing lands, and beside it a badge
// showing what a click adds (a note in the picked colour, the picked shape, the sticker…).

import { iconPath } from '../components/icons'

const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')

export type ToolLook =
  | { kind: 'icon'; icon: string; color?: string }
  | { kind: 'note'; bg: string; ink: string }
  | { kind: 'emoji'; emoji: string }

/** A CSS cursor value; the hot spot is the middle of the cross. */
export function toolCursor(look: ToolLook): string {
  const cross = `<path d="M9 2v14M2 9h14" stroke="#fff" stroke-width="4" stroke-linecap="round"/><path d="M9 2v14M2 9h14" stroke="#1d2024" stroke-width="1.6" stroke-linecap="round"/>`
  let badge: string
  if (look.kind === 'note') {
    badge = `<rect x="16" y="16" width="22" height="22" rx="2" fill="${look.bg}" stroke="rgba(0,0,0,.28)"/>` +
      `<path d="M21 24h12M21 29h8" stroke="${look.ink}" stroke-opacity=".55" stroke-width="1.8" stroke-linecap="round"/>`
  } else {
    const inner = look.kind === 'emoji'
      ? `<text x="27" y="32.5" font-size="15" text-anchor="middle" font-family="Apple Color Emoji,Segoe UI Emoji,Noto Color Emoji,sans-serif">${esc(look.emoji)}</text>`
      : `<g transform="translate(19 19) scale(.6667)"><path d="${iconPath(look.icon)}" fill="none" stroke="${look.color || '#1d2024'}" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"/></g>`
    badge = `<rect x="15.5" y="15.5" width="23" height="23" rx="6" fill="#fff" stroke="rgba(0,0,0,.22)"/>${inner}`
  }
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40" viewBox="0 0 40 40">${cross}${badge}</svg>`
  return `url("data:image/svg+xml,${encodeURIComponent(svg)}") 9 9, crosshair`
}
