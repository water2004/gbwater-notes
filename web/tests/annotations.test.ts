import { afterEach, describe, expect, it, vi } from 'vitest'
import { anchorForTextRange, locate, type TextIndex } from '@/lib/annotations'
import { fetchArticle } from '@/lib/initial-article'

function indexOf(text: string): TextIndex {
  return { text, nodes: [], images: [] }
}

describe('划线锚点重定位', () => {
  it('精确命中并返回引用区间', () => {
    const text = '前文，线性方程组的系数矩阵，后文'
    const start = text.indexOf('系数矩阵')
    expect(locate(indexOf(text), '系数矩阵')).toEqual({ start, end: start + '系数矩阵'.length })
  })

  it('同一段文字多处出现时用前后文选择正确位置', () => {
    const text = '第一处矩阵说明。第二处矩阵定义。'
    const result = locate(indexOf(text), '矩阵', '第二处', '定义')
    expect(result).not.toBeNull()
    expect(result?.start).toBe(text.indexOf('矩阵', 5))
  })

  it('使用引文结束位置之后的文字来区分重复引文', () => {
    const text = '矩阵定义。矩阵说明。'
    const start = text.lastIndexOf('矩阵')
    expect(locate(indexOf(text), '矩阵', '', '说明')).toEqual({ start, end: start + 2 })
  })

  it('格式化引入换行时仍能找回原文，并返回原始文本偏移', () => {
    const text = '其他内容。一个可\n    移动的引文。'
    expect(locate(indexOf(text), '一个可 移动的引文')).toEqual({
      start: text.indexOf('一个'),
      end: text.lastIndexOf('。'),
    })
  })

  it('多个位置无法消歧或只剩别处的同一个短词时不定位', () => {
    expect(locate(indexOf('甲有矩阵，乙有矩阵。'), '矩阵')).toBeNull()
    expect(locate(indexOf('乙有矩阵。'), '矩阵', '甲有', '，乙有')).toBeNull()
  })

  it('原文改写后不会仅靠原句前半段贴到新的文字上', () => {
    const text = '这一段介绍线性方程组的系数矩阵以及它的秩和行列式的性质'
    const quote = '这一段介绍线性方程组的系数矩阵以及它的秩和行列式的定义'
    expect(locate(indexOf(text), quote)).toBeNull()
  })

  it('引用被删掉时找不到，恢复后用同一锚点重新定位', () => {
    expect(locate(indexOf('完全无关的内容'), '线性代数')).toBeNull()
    expect(locate(indexOf('线性代数'), '线性代数')).toEqual({ start: 0, end: 4 })
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
})
