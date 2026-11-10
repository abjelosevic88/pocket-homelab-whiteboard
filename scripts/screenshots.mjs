// README screenshots and a browser smoke test against a demo instance:
//
//   make build && DATA_DIR=/tmp/wb-demo DEMO_DATA=true AUTH_MODE=none HOST=127.0.0.1 PORT=18270 ./pocket-whiteboard &
//   CDP=http://127.0.0.1:9340 BASE=http://127.0.0.1:18270 node scripts/screenshots.mjs   # any Chrome with remote debugging
//   SHOTS=0 … node scripts/screenshots.mjs                                                # checks only
// playwright-core comes from web/node_modules, or set PW_FROM to a package.json next to an install of it.
import { createRequire } from 'node:module'
import assert from 'node:assert/strict'

const require = createRequire(process.env.PW_FROM || new URL('../web/package.json', import.meta.url))
let pw
try { pw = require('playwright') } catch { pw = require('playwright-core') }
const { chromium } = pw
const base = process.env.BASE || 'http://127.0.0.1:18270'
const shots = process.env.SHOTS !== '0'
const dir = process.env.OUT || new URL('../docs/screenshots/', import.meta.url).pathname
const browser = process.env.CDP ? await chromium.connectOverCDP(process.env.CDP) : await chromium.launch()
const errors = []

async function session({ phone = false, dark = false } = {}) {
  const ctx = await browser.newContext({ viewport: phone ? { width: 390, height: 844 } : { width: 1440, height: 900 }, deviceScaleFactor: 2, isMobile: phone, hasTouch: phone, colorScheme: dark ? 'dark' : 'light' })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e}`))
  page.on('console', m => { if (m.type() === 'error' && !/Failed to load resource/.test(m.text())) errors.push(m.text()) })
  return { ctx, page }
}
const go = async (page, path, wait = 1400) => { await page.goto(base + path, { waitUntil: 'domcontentloaded' }); await page.waitForTimeout(wait) }
const shot = async (page, name) => { if (shots) await page.screenshot({ path: dir + name + '.png' }) }
const boardId = async (page, title) => (await page.evaluate(() => fetch('/api/v1/boards?plain=1').then(r => r.json()))).boards.find(b => b.title.startsWith(title)).id

{
  const { ctx, page } = await session()
  await go(page, '/')
  assert.ok((await page.content()).includes('Sprint 42 retrospective'), 'the demo boards are listed')
  await shot(page, 'desktop-home')
  for (const [t, n] of [['Sprint 42', 'desktop-retro'], ['Garden', 'desktop-brainstorm'], ['Booking', 'desktop-flow'], ['Website', 'desktop-kanban']]) {
    await go(page, `/b/${await boardId(page, t)}`)
    await shot(page, n)
  }
  // a new note by double-click, written on, seen on the server
  const id = await boardId(page, 'Website')
  await go(page, `/b/${id}`)
  // find empty board (nothing within 60 px), then double-click it
  const spot = await page.evaluate(() => {
    const clear = (x, y) => [[0, 0], [60, 0], [-60, 0], [0, 60], [0, -60]].every(([dx, dy]) => { const e = document.elementFromPoint(x + dx, y + dy); return e && e.classList.contains('wb-vp') })
    for (let y = 820; y > 200; y -= 40) for (let x = 1300; x > 200; x -= 40) if (clear(x, y)) return { x, y }
    return { x: 1200, y: 820 }
  })
  await page.mouse.dblclick(spot.x, spot.y)
  await page.waitForTimeout(250)
  await page.keyboard.type('Smoke test note')
  await page.keyboard.press('Escape')
  let saved = false
  for (let i = 0; i < 20 && !saved; i++) {
    await page.waitForTimeout(250)
    const doc = await page.evaluate(b => fetch(`/api/v1/boards/${b}`).then(r => r.json()), id)
    saved = doc.objects.some(o => (o.data.text || '').includes('Smoke test note'))
  }
  assert.ok(saved, 'a note written on the board is saved')
  await ctx.close()
}
{
  const { ctx, page } = await session({ phone: true })
  await go(page, '/')
  await shot(page, 'phone-home')
  await go(page, `/b/${await boardId(page, 'Sprint 42')}`)
  await shot(page, 'phone-board')
  await ctx.close()
}
{
  const { ctx, page } = await session({ dark: true })
  await go(page, '/', 600)
  await go(page, `/b/${await boardId(page, 'Booking')}`)
  await shot(page, 'desktop-dark')
  await ctx.close()
}
assert.deepEqual(errors, [], 'no page errors')
console.log('ok')
await browser.close()
