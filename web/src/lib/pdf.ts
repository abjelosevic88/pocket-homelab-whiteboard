// A small PDF writer: each page one JPEG picture (a frame of the board, or
// the whole board), sized to fit A4 landscape or portrait.
async function jpegBytes(c: HTMLCanvasElement): Promise<Uint8Array> {
  const blob = await new Promise<Blob>((res, rej) => c.toBlob(b => (b ? res(b) : rej(new Error('could not draw the page'))), 'image/jpeg', 0.9))
  return new Uint8Array(await blob.arrayBuffer())
}

export async function pagesToPDF(pages: HTMLCanvasElement[], title: string): Promise<Blob> {
  const enc = new TextEncoder()
  const parts: Uint8Array[] = []
  const offsets: number[] = []
  let size = 0
  const add = (b: Uint8Array | string) => { const u = typeof b === 'string' ? enc.encode(b) : b; parts.push(u); size += u.length }
  const obj = (n: number, body: (Uint8Array | string)[]) => { offsets[n] = size; add(`${n} 0 obj\n`); body.forEach(add); add('\nendobj\n') }
  add('%PDF-1.4\n%âãÏÓ\n')
  // 1 catalog, 2 pages, 3 info; then per page: page, content, image
  const kids: string[] = []
  const pageObjs: { page: number; content: number; image: number; c: HTMLCanvasElement; jpg: Uint8Array }[] = []
  let n = 4
  for (const c of pages) { const p = { page: n, content: n + 1, image: n + 2, c, jpg: await jpegBytes(c) }; n += 3; pageObjs.push(p); kids.push(`${p.page} 0 R`) }
  obj(1, ['<< /Type /Catalog /Pages 2 0 R >>'])
  obj(2, [`<< /Type /Pages /Kids [${kids.join(' ')}] /Count ${pageObjs.length} >>`])
  const esc = (t: string) => t.replace(/[\\()]/g, m => '\\' + m).replace(/[^\x20-\x7e]/g, '?')
  obj(3, [`<< /Title (${esc(title)}) /Producer (Pocket Whiteboard) >>`])
  for (const p of pageObjs) {
    // fit the picture on an A4 page (842 x 595 points), the long side along the picture's
    const land = p.c.width >= p.c.height
    const pw = land ? 842 : 595, ph = land ? 595 : 842, m = 24
    const s = Math.min((pw - 2 * m) / p.c.width, (ph - 2 * m) / p.c.height)
    const w = p.c.width * s, h = p.c.height * s, x = (pw - w) / 2, y = (ph - h) / 2
    const content = `q ${w.toFixed(2)} 0 0 ${h.toFixed(2)} ${x.toFixed(2)} ${y.toFixed(2)} cm /Im0 Do Q`
    obj(p.page, [`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 ${pw} ${ph}] /Resources << /XObject << /Im0 ${p.image} 0 R >> >> /Contents ${p.content} 0 R >>`])
    obj(p.content, [`<< /Length ${content.length} >>\nstream\n${content}\nendstream`])
    obj(p.image, [`<< /Type /XObject /Subtype /Image /Width ${p.c.width} /Height ${p.c.height} /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length ${p.jpg.length} >>\nstream\n`, p.jpg, '\nendstream'])
  }
  const xref = size
  let x = `xref\n0 ${n}\n0000000000 65535 f \n`
  for (let i = 1; i < n; i++) x += `${String(offsets[i] || 0).padStart(10, '0')} 00000 n \n`
  add(x)
  add(`trailer\n<< /Size ${n} /Root 1 0 R /Info 3 0 R >>\nstartxref\n${xref}\n%%EOF\n`)
  return new Blob(parts as BlobPart[], { type: 'application/pdf' })
}
