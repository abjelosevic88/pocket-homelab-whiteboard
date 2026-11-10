// Formatted text in notes: a few safe tags only (bold, italic, underline,
// strike, lists, links), because anyone with an edit link can write here.
const KEEP: Record<string, string> = { B: 'b', STRONG: 'b', I: 'i', EM: 'i', U: 'u', S: 's', STRIKE: 's', DEL: 's', UL: 'ul', OL: 'ol', LI: 'li', A: 'a', BR: 'br', DIV: 'div', P: 'div' }
const FORMATTING = /<(b|i|u|s|ul|ol|li|a)[\s>]/

function esc(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
}

function safeHref(h: string | null): string {
  if (!h) return ''
  const v = h.trim()
  return /^(https?:|mailto:)/i.test(v) ? v : /^www\./i.test(v) ? 'https://' + v : ''
}

function walk(n: Node): string {
  if (n.nodeType === Node.TEXT_NODE) return esc(n.textContent || '')
  if (n.nodeType !== Node.ELEMENT_NODE) return ''
  const el = n as HTMLElement
  const inner = [...el.childNodes].map(walk).join('')
  const tag = KEEP[el.tagName]
  // bold / italic that came as styles (browsers do that)
  const st = el.getAttribute('style') || ''
  let out = inner
  if (/font-weight:\s*(bold|[6-9]00)/.test(st)) out = `<b>${out}</b>`
  if (/font-style:\s*italic/.test(st)) out = `<i>${out}</i>`
  if (!tag) return out
  if (tag === 'br') return '<br>'
  if (tag === 'a') {
    const href = safeHref(el.getAttribute('href'))
    return href ? `<a href="${esc(href)}" target="_blank" rel="noopener noreferrer">${out}</a>` : out
  }
  return `<${tag}>${out}</${tag}>`
}

/** Clean HTML from the editor or the server: only the allowed tags. */
export function sanitize(html: string): string {
  const doc = new DOMParser().parseFromString(`<div>${html}</div>`, 'text/html')
  return [...doc.body.firstChild!.childNodes].map(walk).join('')
}

/** Whether there is any formatting at all (else keep plain text only). */
export function hasFormatting(html: string): boolean {
  return FORMATTING.test(html)
}

/** Plain text as safe HTML (for the editor's first content). */
export function textToHTML(t: string): string {
  return esc(t).replace(/\n/g, '<br>')
}

/** Text of HTML, line breaks kept (lists become lines). */
export function htmlToText(html: string): string {
  const doc = new DOMParser().parseFromString(`<div>${html}</div>`, 'text/html')
  const out: string[] = []
  const go = (n: Node) => {
    if (n.nodeType === Node.TEXT_NODE) { out.push(n.textContent || ''); return }
    if (n.nodeType !== Node.ELEMENT_NODE) return
    const el = n as HTMLElement
    const block = ['DIV', 'P', 'LI', 'UL', 'OL'].includes(el.tagName)
    if (el.tagName === 'BR') { out.push('\n'); return }
    if (block && out.length && !out[out.length - 1].endsWith('\n')) out.push('\n')
    if (el.tagName === 'LI') out.push(el.parentElement?.tagName === 'OL' ? `${[...el.parentElement.children].indexOf(el) + 1}. ` : '• ')
    el.childNodes.forEach(go)
    if (block && !out[out.length - 1]?.endsWith('\n')) out.push('\n')
  }
  doc.body.firstChild!.childNodes.forEach(go)
  return out.join('').replace(/\n{3,}/g, '\n\n').replace(/\n$/, '')
}
