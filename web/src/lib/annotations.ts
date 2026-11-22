import { api } from '@/lib/api'

export type HighlightColor = 'yellow' | 'green' | 'blue' | 'pink'

export interface Highlight {
  id: string
  noteId: string
  quote: string
  prefix: string
  suffix: string
  heading: string
  color: HighlightColor
  comment: string
  revision: string
  createdAt: string
  updatedAt: string
}

export interface Bookmark {
  id: string
  noteId: string
  label: string
  quote: string
  prefix: string
  suffix: string
  heading: string
  position: number
  revision: string
  createdAt: string
  updatedAt: string
}

export interface StickyNote {
  id: string
  noteId: string
  body: string
  color: HighlightColor
  quote: string
  prefix: string
  suffix: string
  heading: string
  revision: string
  createdAt: string
  updatedAt: string
}

export interface Memo {
  noteId: string
  body: string
  updatedAt: string
}

export interface NoteAnnotations {
  highlights: Highlight[]
  bookmarks: Bookmark[]
  memo: Memo | null
  stickyNotes: StickyNote[]
}

export interface AnchorInput {
  quote: string
  prefix: string
  suffix: string
  heading: string
}

export async function loadAnnotations(noteId: string, signal: AbortSignal): Promise<NoteAnnotations> {
  return api<NoteAnnotations>(`/api/me/annotations?noteId=${encodeURIComponent(noteId)}`, { signal })
}

export async function createHighlight(noteId: string, anchor: AnchorInput, color: HighlightColor, comment: string, revision: string) {
  const data = await api<{ highlight: Highlight }>('/api/me/highlights', {
    method: 'POST',
    json: { noteId, ...anchor, color, comment, revision },
  })
  return data.highlight
}

export async function deleteHighlight(id: string) {
  await api(`/api/me/highlights/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export async function createBookmark(noteId: string, anchor: AnchorInput, label: string, position: number, revision: string) {
  const data = await api<{ bookmark: Bookmark }>('/api/me/bookmarks', {
    method: 'POST',
    json: { noteId, ...anchor, label, position, revision },
  })
  return data.bookmark
}

export async function updateBookmark(id: string, patch: Partial<Pick<Bookmark, 'label' | 'revision' | 'quote' | 'prefix' | 'suffix' | 'heading' | 'position'>>) {
  const data = await api<{ bookmark: Bookmark }>(`/api/me/bookmarks/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    json: patch,
  })
  return data.bookmark
}

export async function deleteBookmark(id: string) {
  await api(`/api/me/bookmarks/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export async function saveMemo(noteId: string, body: string) {
  const data = await api<{ memo: Memo }>('/api/me/memo', { method: 'PUT', json: { noteId, body } })
  return data.memo
}

export interface AnnotationSummary {
  highlightCount: number
  bookmarkCount: number
  memoCount: number
  stickyCount: number
  recentHighlights: Array<Highlight & { noteTitle: string; course: string }>
  recentBookmarks: Array<Bookmark & { noteTitle: string; course: string }>
}

export async function createStickyNote(noteId: string, body: string, color: HighlightColor, anchor?: AnchorInput, revision = '') {
  const data = await api<{ stickyNote: StickyNote }>('/api/me/sticky-notes', {
    method: 'POST',
    json: { noteId, body, color, ...(anchor || {}), revision },
  })
  return data.stickyNote
}

export async function updateStickyNote(id: string, patch: { body?: string; color?: HighlightColor }) {
  const data = await api<{ stickyNote: StickyNote }>(`/api/me/sticky-notes/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    json: patch,
  })
  return data.stickyNote
}

export async function deleteStickyNote(id: string) {
  await api(`/api/me/sticky-notes/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export async function loadAnnotationSummary() {
  return api<AnnotationSummary>('/api/me/annotations/summary')
}

interface TextNode {
  node: globalThis.Text
  start: number
  end: number
}

export interface InlineImage {
  element: HTMLImageElement
  offset: number
}

export interface TextIndex {
  text: string
  nodes: TextNode[]
  images: InlineImage[]
  normalized?: { text: string; offsets: number[] }
}

export interface TextLocation { start: number; end: number }

// indexArticle 把文章里的文本节点拼成一条长文本，并记录每个节点对应的区间；
// 行内公式是 <img>，不产生文字，但记录它所在的文本偏移，供划线背景覆盖与定位。
// 锚点（quote + 前后文）都在这条文本上计算，因此与具体标签结构无关。
export function indexArticle(article: HTMLElement): TextIndex {
  const walker = document.createTreeWalker(article, NodeFilter.SHOW_TEXT | NodeFilter.SHOW_ELEMENT)
  const nodes: TextNode[] = []
  const images: InlineImage[] = []
  let text = ''
  let current = walker.nextNode()
  while (current) {
    if (current.nodeType === Node.TEXT_NODE) {
      const value = current.nodeValue || ''
      if (value) {
        nodes.push({ node: current as globalThis.Text, start: text.length, end: text.length + value.length })
        text += value
      }
    } else if (current instanceof HTMLImageElement && current.classList.contains('math-image')) {
      images.push({ element: current, offset: text.length })
    }
    current = walker.nextNode()
  }
  return { text, nodes, images }
}

function nodeAt(index: TextIndex, offset: number, end = false) {
  for (const item of index.nodes) {
    const within = end ? item.start < offset && offset <= item.end : item.start <= offset && offset < item.end
    if (within) {
      return { node: item.node, offset: offset - item.start }
    }
  }
  const last = index.nodes[index.nodes.length - 1]
  return last ? { node: last.node, offset: last.node.nodeValue?.length || 0 } : null
}

function scoreContext(before: string, after: string, prefix: string, suffix: string) {
  const left = !prefix ? 0 : before.endsWith(prefix) ? 2 : before.endsWith(prefix.slice(-12)) ? 1 : 0
  const right = !suffix ? 0 : after.startsWith(suffix) ? 2 : after.startsWith(suffix.slice(0, 12)) ? 1 : 0
  return left + right
}

function matchQuote(text: string, quote: string, prefix: string, suffix: string): TextLocation | null {
  let best: TextLocation | null = null
  let bestScore = -1
  let tied = false
  for (let start = text.indexOf(quote); start >= 0; start = text.indexOf(quote, start + 1)) {
    const end = start + quote.length
    const before = text.slice(Math.max(0, start - prefix.length), start)
    const after = text.slice(end, end + suffix.length)
    const score = scoreContext(before, after, prefix, suffix)
    if (score > bestScore) {
      best = { start, end }
      bestScore = score
      tied = false
    } else if (score === bestScore) {
      tied = true
    }
  }
  // 短引文在前后文都对不上时，可能只剩下别处的同一个词。
  if (tied || (bestScore === 0 && quote.length < 12 && (prefix.trim() || suffix.trim()))) return null
  return best
}

function normalizeWhitespace(text: string) {
  return text.replace(/\s+/g, ' ')
}

// 用原文和前后文定位。HTML 格式化可能引入换行与缩进，第二次匹配
// 忽略连续空白，再把结果映射回原有的 DOM 文本偏移。
export function locate(index: TextIndex, quote: string, prefix = '', suffix = ''): TextLocation | null {
  if (!quote.trim()) return null
  const exact = matchQuote(index.text, quote, prefix, suffix)
  if (exact) return exact
  if (!index.normalized) {
    const offsets: number[] = []
    for (const match of index.text.matchAll(/\s+|\S/g)) offsets.push(match.index)
    index.normalized = { text: normalizeWhitespace(index.text), offsets }
  }
  const { text, offsets } = index.normalized
  const match = matchQuote(text, normalizeWhitespace(quote), normalizeWhitespace(prefix), normalizeWhitespace(suffix))
  return match && { start: offsets[match.start], end: offsets[match.end - 1] + 1 }
}

export function anchorForTextRange(index: TextIndex, start: number, end: number, heading: string): AnchorInput | null {
  const text = index.text.slice(start, end)
  const quote = [...text.trim()].slice(0, 500).join('')
  if (!quote) return null
  const quoteStart = start + text.length - text.trimStart().length
  const quoteEnd = quoteStart + quote.length
  return {
    quote,
    prefix: index.text.slice(Math.max(0, quoteStart - 40), quoteStart),
    suffix: index.text.slice(quoteEnd, quoteEnd + 40),
    heading,
  }
}

export function selectionAnchor(article: HTMLElement, range: Range): AnchorInput | null {
  const index = indexArticle(article)
  const startNode = range.startContainer
  const endNode = range.endContainer
  if (startNode.nodeType !== Node.TEXT_NODE || endNode.nodeType !== Node.TEXT_NODE) return null
  const startEntry = index.nodes.find(item => item.node === startNode)
  const endEntry = index.nodes.find(item => item.node === endNode)
  if (!startEntry || !endEntry) return null
  const start = startEntry.start + range.startOffset
  const end = endEntry.start + range.endOffset
  if (end <= start) return null
  const heading = (range.startContainer.parentElement?.closest('h1,h2,h3,h4')?.textContent || '').trim()
  return anchorForTextRange(index, start, end, heading)
}

export function clearHighlights(article: HTMLElement) {
  article.querySelectorAll('mark[data-highlight-id]').forEach(mark => {
    const parent = mark.parentNode
    if (!parent) return
    while (mark.firstChild) parent.insertBefore(mark.firstChild, mark)
    parent.removeChild(mark)
    parent.normalize()
  })
  article.querySelectorAll('.highlight-layer').forEach(layer => layer.remove())
  article.querySelectorAll<HTMLImageElement>('img[data-highlight-id]').forEach(image => {
    delete image.dataset.highlightId
  })
}

// applyMarkdownHighlights 把 ==荧光笔== 的背景抽到文字下方的独立层，
// 保证层级是：荧光笔底色 < 用户划线 < 正文。
export function applyMarkdownHighlights(article: HTMLElement) {
  article.querySelectorAll('.markdown-highlight-layer').forEach(layer => layer.remove())
  const marks = article.querySelectorAll<HTMLElement>('mark.markdown-highlight')
  if (!marks.length) return
  const layer = document.createElement('div')
  layer.className = 'markdown-highlight-layer'
  layer.setAttribute('aria-hidden', 'true')
  article.prepend(layer)
  const articleRect = article.getBoundingClientRect()
  for (const mark of marks) {
    for (const rect of mark.getClientRects()) {
      if (rect.width < 1 || rect.height < 1) continue
      const band = document.createElement('div')
      band.className = 'markdown-highlight-band'
      band.style.left = `${rect.left - articleRect.left}px`
      band.style.top = `${rect.top - articleRect.top}px`
      band.style.width = `${rect.width}px`
      band.style.height = `${rect.height}px`
      layer.appendChild(band)
    }
  }
}

// applyHighlights 用 Range.getClientRects 画连续的底层色带（含行内公式），
// 文字只保留透明的点击包裹层，因此视觉上不再按节点/公式分段。
export function applyHighlights(article: HTMLElement, highlights: Highlight[], locations: ReadonlyMap<string, TextLocation>) {
  clearHighlights(article)
  const index = indexArticle(article)
  const located: Array<{ highlight: Highlight; range: { start: number; end: number }; rects: DOMRect[] }> = []
  const used: Array<{ start: number; end: number }> = []
  for (const highlight of highlights) {
    const range = locations.get(highlight.id)
    if (!range) continue
    if (used.some(item => range.start < item.end && range.end > item.start)) continue
    const start = nodeAt(index, range.start)
    const end = nodeAt(index, range.end, true)
    if (!start || !end) continue
    const domRange = document.createRange()
    domRange.setStart(start.node, start.offset)
    domRange.setEnd(end.node, end.offset)
    used.push(range)
    located.push({ highlight, range, rects: [...domRange.getClientRects()] })
  }

  const layer = document.createElement('div')
  layer.className = 'highlight-layer'
  layer.setAttribute('aria-hidden', 'true')
  article.prepend(layer)
  const articleRect = article.getBoundingClientRect()
  for (const item of located) {
    // 同一行里被 Range 拆成多段的矩形合并成一条，避免圆角处出现接缝。
    const rects = item.rects
      .filter(rect => rect.width >= 1 && rect.height >= 1)
      .map(rect => ({ left: rect.left, top: rect.top, width: rect.width, height: rect.height }))
      .sort((a, b) => a.left - b.left)
    const merged: typeof rects = []
    for (const rect of rects) {
      const last = merged[merged.length - 1]
      if (last && Math.abs(last.top - rect.top) < 1 && Math.abs(last.height - rect.height) < 1
        && rect.left <= last.left + last.width + 1.5) {
        last.width = Math.max(last.width, rect.left - last.left + rect.width)
      } else {
        merged.push({ ...rect })
      }
    }
    for (const rect of merged) {
      const band = document.createElement('div')
      band.className = `highlight-band highlight-${item.highlight.color}`
      band.dataset.highlightId = item.highlight.id
      band.style.left = `${rect.left - articleRect.left}px`
      band.style.top = `${rect.top - articleRect.top}px`
      band.style.width = `${rect.width}px`
      band.style.height = `${rect.height}px`
      layer.appendChild(band)
    }
  }

  // 透明包裹层只负责点击命中，不参与视觉；公式图片直接标记 id。
  located.sort((a, b) => b.range.start - a.range.start)
  for (const item of located) {
    const affected = index.nodes.filter(node => node.end > item.range.start && node.start < item.range.end)
    for (let position = affected.length - 1; position >= 0; position -= 1) {
      const entry = affected[position]
      let node = entry.node
      const from = Math.max(item.range.start, entry.start) - entry.start
      const to = Math.min(item.range.end, entry.end) - entry.start
      if (to <= from) continue
      if (from > 0) node = node.splitText(from)
      if (to - from < (node.nodeValue || '').length) node.splitText(to - from)
      const mark = document.createElement('mark')
      mark.className = 'user-highlight'
      mark.dataset.highlightId = item.highlight.id
      if (item.highlight.comment) mark.title = item.highlight.comment
      node.parentNode?.insertBefore(mark, node)
      mark.appendChild(node)
    }
    for (const image of index.images) {
      if (image.offset <= item.range.start || image.offset >= item.range.end) continue
      image.element.dataset.highlightId = item.highlight.id
    }
  }
}
export function headingText(article: HTMLElement, anchor: HTMLElement | null): string {
  let current: Element | null = anchor
  while (current && current !== article) {
    if (/^H[1-4]$/.test(current.tagName)) return (current.textContent || '').trim()
    current = current.parentElement
  }
  return ''
}

// elementAtOffset 把文本偏移映射回 DOM 元素，用于滚动定位。
export function elementAtOffset(index: TextIndex, offset: number): HTMLElement | null {
  const image = index.images.find(item => Math.abs(item.offset - offset) <= 1)
  if (image) return image.element
  const item = nodeAt(index, offset)
  return item ? item.node.parentElement : null
}
// applyBookmarks 在阅读位置所在的块上加一枚可点击的书签旗标；找不到位置时
// 保留数据与列表，由用户在书签面板里删除或重新记录。
export function applyBookmarks(article: HTMLElement, bookmarks: Bookmark[], locations: ReadonlyMap<string, TextLocation>) {
  article.querySelectorAll('.bookmark-flag').forEach(flag => flag.remove())
  article.querySelectorAll('.bookmark-mark').forEach(element => element.classList.remove('bookmark-mark'))
  if (!bookmarks.length) return
  const index = indexArticle(article)
  for (const bookmark of bookmarks) {
    const range = locations.get(bookmark.id)
    if (!range) continue
    const element = elementAtOffset(index, range.start)
    const block = element?.closest('p, li, h2, h3, h4, blockquote, td') || element
    if (!block) continue
    block.classList.add('bookmark-mark')
    const flag = document.createElement('button')
    flag.type = 'button'
    flag.className = 'bookmark-flag'
    flag.dataset.bookmarkId = bookmark.id
    flag.title = bookmark.label || '阅读书签'
    flag.setAttribute('aria-label', '阅读书签')
    block.prepend(flag)
  }
}
// textRange 用文本偏移构造 DOM Range，供便条精确取到文字行首的矩形。
export function textRange(index: TextIndex, start: number, end: number): Range | null {
  const from = nodeAt(index, start)
  const to = nodeAt(index, end, true)
  if (!from || !to) return null
  const range = document.createRange()
  range.setStart(from.node, from.offset)
  range.setEnd(to.node, to.offset)
  return range
}
