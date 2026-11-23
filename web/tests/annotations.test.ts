import { afterEach, describe, expect, it, vi } from 'vitest'
import { anchorForTextRange, loadAnnotations, mapArticleOffsets, type TextIndex } from '@/lib/annotations'
import { fetchArticle } from '@/lib/initial-article'

function indexOf(text: string): TextIndex {
  return { text, nodes: [], images: [] }
}

describe('标注位置映射', () => {
  it('将服务端 Unicode 字符位置转换成 DOM UTF-16 区间', () => {
    const domRange = mapArticleOffsets('🧠 前\n 置矩阵')
    expect(domRange({ start: 1, end: 5 })).toEqual({ start: 3, end: 9 })
    expect(domRange({ start: 5, end: 6 })).toBeUndefined()
    expect(domRange(undefined)).toBeUndefined()
  })

  it('按去掉选区首尾空白后的边界保存上下文', () => {
    const text = '开头  原文  末尾'
    expect(anchorForTextRange(indexOf(text), 2, 8, '')).toEqual({
      quote: '原文', prefix: '开头  ', suffix: '  末尾', heading: '',
    })
  })
})

describe('正文版本', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('通过静态正文地址同时读到 HTML 和版本', async () => {
    const payload = { html: '<p>原文</p>', revision: '0123456789abcdef'.repeat(2) }
    const request = vi.fn().mockResolvedValue(Response.json(payload))
    vi.stubGlobal('fetch', request)
    const signal = new AbortController().signal
    expect(await fetchArticle('example', signal)).toEqual(payload)
    expect(request).toHaveBeenCalledWith(`${import.meta.env.BASE_URL}note-content/example.json`, { signal })
  })

  it('静态正文缺失时拒绝 Vite 的应用壳回退', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('<div id="app"></div>', {
      headers: { 'Content-Type': 'text/html' },
    })))
    await expect(fetchArticle('missing', new AbortController().signal)).rejects.toThrow('无法加载笔记正文')
  })

  it('拒绝把其他版本的标注画到当前正文', async () => {
    const revision = 'a'.repeat(32)
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(Response.json({
      revision: 'b'.repeat(32), highlights: [], bookmarks: [], stickyNotes: [],
    })))
    await expect(loadAnnotations('math/course/note', revision, new AbortController().signal))
      .rejects.toThrow('标注版本与当前正文不一致')
  })
})
