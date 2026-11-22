import MarkdownIt, { type StateBlock, type StateInline } from 'markdown-it'
import container from 'markdown-it-container'
import footnote from 'markdown-it-footnote'
import attrs from 'markdown-it-attrs'
import type { MathEntry, MermaidEntry, NativeDocument } from '@/types/content'
import { headingSlug } from './slug'

const emptyNative: NativeDocument = { math: {}, mermaid: {} }
export const calloutNames: Record<string, string> = {
  proof: '证明',
  theorem: '定理',
  definition: '定义',
  example: '示例',
  Bbox: '要点',
  note: '注记',
  warning: '注意',
}

function mathBlock(state: StateBlock, startLine: number, endLine: number, silent: boolean) {
  const start = state.bMarks[startLine] + state.tShift[startLine]
  const end = state.eMarks[startLine]
  const first = state.src.slice(start, end).trim()
  if (!first.startsWith('$$')) return false
  const lines: string[] = []
  let nextLine = startLine
  if (first === '$$') {
    let closed = false
    for (nextLine = startLine + 1; nextLine < endLine; nextLine += 1) {
      const lineStart = state.bMarks[nextLine] + state.tShift[nextLine]
      const lineEnd = state.eMarks[nextLine]
      const line = state.src.slice(lineStart, lineEnd)
      if (line.trim() === '$$') {
        closed = true
        break
      }
      lines.push(line)
    }
    if (!closed) return false
  } else if (first.length > 4 && first.endsWith('$$')) {
    lines.push(first.slice(2, -2))
  } else {
    return false
  }
  const content = lines.join('\n').trim()
  if (!content) return false
  if (silent) return true
  state.line = nextLine + 1
  const token = state.push('math_block', '', 0)
  token.content = content
  token.map = [startLine, state.line]
  token.meta = { display: true }
  return true
}

function mathInline(state: StateInline, silent: boolean) {
  const source = state.src
  if (source[state.pos] !== '$') return false
  const count = source[state.pos + 1] === '$' ? 2 : 1
  const delimiter = '$'.repeat(count)
  let cursor = state.pos + count
  while (cursor < source.length) {
    const end = source.indexOf(delimiter, cursor)
    if (end < 0) return false
    let slashes = 0
    for (let index = end - 1; index >= 0 && source[index] === '\\'; index -= 1) slashes += 1
    const content = source.slice(state.pos + count, end)
    const validNewlines = count === 2 ? !/\n\s*\n/.test(content) : !content.includes('\n')
    if (slashes % 2 === 0 && content.trim() && validNewlines) {
      if (!silent) {
        const token = state.push('math_inline', '', 0)
        token.content = content.trim()
        token.meta = { display: count === 2 }
      }
      state.pos = end + count
      return true
    }
    cursor = end + count
  }
  return false
}

function normalizedTex(tex: string): string {
  return tex.split('\n').map(line => line.trim()).join('\n')
}

export function createMarkdownRenderer(native: NativeDocument = emptyNative, base = '/', imageSource?: (source: string) => string | undefined) {
  // 资源前缀由构建期 base 决定，不能写死部署路径。
  const assetBase = base.endsWith('/') ? base : `${base}/`
  // 迁移自旧站的图片仍带 /mypdf/ 前缀，渲染时统一改写成当前 base。
  const legacyPrefix = '/mypdf/'
  const assetUrl = (url: string) => {
    if (url.startsWith('/content-assets/')) return assetBase + url.slice(1)
    return url.startsWith(legacyPrefix) ? assetBase + url.slice(legacyPrefix.length) : url
  }
  const md = new MarkdownIt({ html: false, linkify: true, typographer: false, breaks: false })
  md.use(footnote)
  md.use(attrs)
  md.block.ruler.before('fence', 'math_block', mathBlock, { alt: ['paragraph', 'reference', 'blockquote', 'list'] })
  md.inline.ruler.before('escape', 'math_inline', mathInline)

  const math = new Map<string, { key: string; entry: MathEntry }>()
  for (const [key, entry] of Object.entries(native.math || {})) {
    math.set(`${entry.display ? '1' : '0'}\0${normalizedTex(entry.tex)}`, { key, entry })
  }
  const mermaid = new Map<string, { key: string; entry: MermaidEntry }>()
  for (const [key, entry] of Object.entries(native.mermaid || {})) {
    mermaid.set(entry.source, { key, entry })
  }

  function renderMath(tex: string, display: boolean): string {
    const found = math.get(`${display ? '1' : '0'}\0${normalizedTex(tex)}`)
    const escaped = md.utils.escapeHtml(tex)
    if (!found || found.entry.error || !found.entry.width || !found.entry.height) {
      return `<code class="math-fallback" title="${md.utils.escapeHtml(found?.entry.error || '公式尚未预渲染')}">${escaped}</code>`
    }
    const { width, height, baseline = height } = found.entry
    const image = `<img class="math-image" src="${assetBase}generated/math/${found.key}.svg" alt="${escaped}" title="点击放大公式" width="${width}" height="${height}" style="vertical-align:${(baseline - height).toFixed(3)}px" loading="lazy" decoding="async">`
    return display
      ? `<div class="math-display" role="math" aria-label="${escaped}">${image}</div>`
      : `<span class="math-inline" role="math" aria-label="${escaped}">${image}</span>`
  }

  md.renderer.rules.math_inline = (tokens, index) => renderMath(tokens[index].content, Boolean(tokens[index].meta?.display))
  md.renderer.rules.math_block = (tokens, index) => renderMath(tokens[index].content, true)

  const defaultFence = md.renderer.rules.fence
  md.renderer.rules.fence = (tokens, index, options, env, self) => {
    const token = tokens[index]
    if (token.info.trim().toLowerCase() !== 'mermaid') {
      return defaultFence
        ? defaultFence(tokens, index, options, env, self)
        : self.renderToken(tokens, index, options)
    }
    const found = mermaid.get(token.content.trim())
    if (!found || found.entry.error) {
      return `<pre class="mermaid-fallback"><code>${md.utils.escapeHtml(token.content)}</code></pre>`
    }
    const width = found.entry.width || 800
    const height = found.entry.height || 450
    return `<figure class="mermaid-figure"><img class="mermaid-light" src="${assetBase}generated/mermaid/${found.key}-light.svg" width="${width}" height="${height}" alt="Mermaid 图表" loading="lazy"><img class="mermaid-dark" src="${assetBase}generated/mermaid/${found.key}-dark.svg" width="${width}" height="${height}" alt="Mermaid 图表" loading="lazy"></figure>`
  }

  const slugs = new Map<string, number>()
  md.renderer.rules.heading_open = (tokens, index, options, env, self) => {
    const base = headingSlug(tokens[index + 1]?.content || 'section') || 'section'
    const count = (slugs.get(base) || 0) + 1
    slugs.set(base, count)
    tokens[index].attrSet('id', count === 1 ? base : `${base}-${count}`)
    return self.renderToken(tokens, index, options)
  }

  const defaultImage = md.renderer.rules.image
  md.renderer.rules.image = (tokens, index, options, env, self) => {
    const source = tokens[index].attrGet('src')
    if (typeof source === 'string') tokens[index].attrSet('src', imageSource?.(source) ?? assetUrl(source))
    tokens[index].attrSet('loading', 'lazy')
    tokens[index].attrSet('decoding', 'async')
    return defaultImage
      ? defaultImage(tokens, index, options, env, self)
      : self.renderToken(tokens, index, options)
  }

  md.renderer.rules.link_open = (tokens, index, options, env, self) => {
    const href = String(tokens[index].attrGet('href') ?? '')
    if (href.startsWith(legacyPrefix)) tokens[index].attrSet('href', assetUrl(href))
    if (/^https?:\/\//.test(href)) {
      tokens[index].attrSet('target', '_blank')
      tokens[index].attrSet('rel', 'noopener noreferrer')
    }
    return self.renderToken(tokens, index, options)
  }

  for (const [name, label] of Object.entries(calloutNames)) {
    md.use(container, name, {
      render(tokens, index) {
        return tokens[index].nesting === 1
          ? `<section class="callout callout-${name.toLowerCase()}"><div class="callout-label">${label}</div>\n`
          : '</section>\n'
      },
    })
  }
  md.use(container, 'center', {
    render(tokens, index) {
      return tokens[index].nesting === 1 ? '<div class="content-center">\n' : '</div>\n'
    },
  })
  md.use(container, 'minipage', {
    render(tokens, index) {
      return tokens[index].nesting === 1 ? '<section class="minipage">\n' : '</section>\n'
    },
  })
  return md
}

export function markdownBody(source: string): string {
  const normalized = source.replace(/\r\n?/g, '\n')
  return normalized.replace(/^---\n[\s\S]*?\n---(?:\s*\n|$)/, '')
}

export function renderMarkdown(source: string, native: NativeDocument = emptyNative, base = '/'): string {
  return createMarkdownRenderer(native, base).render(source)
}
