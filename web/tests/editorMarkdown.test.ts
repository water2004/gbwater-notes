import { describe, expect, it } from 'vitest'
import { compareMarkdown, composeMarkdown, splitMarkdown } from '../src/lib/editorMarkdown'
import { editorImageURL, imageMarkdown } from '../src/lib/adminAssets'
import { findImageReferences, imageAltText } from '../src/lib/editorImages'
import { EditorState } from '@codemirror/state'
import { CompletionContext } from '@codemirror/autocomplete'
import { completeMarkdown } from '../src/lib/editorCompletion'
import { findHeadings, noteLanguage } from '../src/lib/editorSyntax'

describe('Markdown workbench', () => {
  it('keeps indentation and unmanaged frontmatter when editing a document', () => {
    const parsed = splitMarkdown('---\r\ntitle: 原来的标题\r\nkeywords: [alpha, beta]\r\n---\r\n\r\n    indented code\r\n')
    expect(parsed.fields.title).toBe('原来的标题')
    expect(composeMarkdown({ title: '新标题' }, parsed.lines, parsed.body))
      .toBe('---\ntitle: "新标题"\nkeywords: [alpha, beta]\n---\n\n    indented code\n')
  })

  it('does not treat shifted unchanged lines as edits', () => {
    const result = compareMarkdown('A\nB\nC\nD', 'A\ninsert\nB\nC\nD')
    expect(result.added).toBe(1)
    expect(result.removed).toBe(0)
    expect(result.changes).toContainEqual({ kind: 'added', text: 'insert' })
  })

  it('inserts a publishable image link and resolves it to an authenticated editor preview', () => {
    const id = 'math/课程 (一)/标题/配图.png'
    const markdown = imageMarkdown({ id, name: '配图.png', modified: '', bytes: 0 })
    expect(markdown).toContain('/content-assets/math/%E8%AF%BE%E7%A8%8B%20%28%E4%B8%80%29/')
    const source = markdown.slice(markdown.indexOf('](') + 2, -1)
    const preview = editorImageURL(source, '/custom/')
    expect(preview).toContain(`${import.meta.env.VITE_API_BASE_PATH}admin/asset/content?`)
    expect(new URL(preview!, 'http://test').searchParams.get('id')).toBe(id)
    expect(editorImageURL(`/mypdf${source}`, '/custom/')).toBe(preview)
    expect(editorImageURL('https://external.example/image.png', '/custom/')).toBeUndefined()
  })

  it('identifies editable image label and destination without touching titles, markup, or code fences', () => {
    const source = '![A \\[diagram\\]](<./image one.png> "title"){.wide}\n\n```\n![hidden](skip.png)\n```'
    const [image, ...others] = findImageReferences(source)
    expect(others).toEqual([])
    expect(image.alt).toBe('A [diagram]')
    expect(source.slice(image.urlFrom, image.urlTo)).toBe('./image one.png')
    const updated = source.slice(0, image.urlFrom) + '/mypdf/content-assets/uploads/new.png' + source.slice(image.urlTo)
    expect(updated).toContain('<' + '/mypdf/content-assets/uploads/new.png' + '> "title"){.wide}')
    expect(source.slice(image.from, image.to)).toContain('{.wide}')
  })

  it('keeps existing HTML image attributes when adding or editing alt text', () => {
    const source = '<figure>\n<!-- <img src="ignore.png"> -->\n<img src="/mypdf/content-assets/课程/曲线.png" style="width:9cm" />\n</figure>\n\n文字 <img alt=old src="other.png">'
    const [figure, inline, ...others] = findImageReferences(source)
    expect(others).toEqual([])
    expect(source.slice(figure.urlFrom, figure.urlTo)).toBe('/mypdf/content-assets/课程/曲线.png')
    expect(figure.altFrom).toBe(figure.altTo)
    expect(imageAltText(figure, '实验 "A&B"')).toBe(' alt="实验 &quot;A&amp;B&quot;"')
    expect(inline.alt).toBe('old')
    expect(imageAltText(inline, '新说明')).toBe('"新说明"')
  })

  it('finds real headings and highlights math and site directives outside code', () => {
    const source = '# 直流电 ##\n\n对照\n---\n\n::: proof\n$R = U/I$\n:::\n\n```text\n## 示例\n$x$\n```'
    expect(findHeadings(source).map(({ title, level }) => [title, level])).toEqual([['直流电', 1], ['对照', 2]])
    const parsed = noteLanguage.language.parser.parse(source).toString()
    expect(parsed.match(/NoteMath/g)).toHaveLength(1)
    expect(parsed.match(/NoteDirective/g)).toHaveLength(2)
    expect(noteLanguage.language.parser.parse('> [!center] 居中').toString()).toContain('NoteDirective')
  })

  it('limits completions to their Markdown context', () => {
    const suggestions = (doc: string) => {
      const state = EditorState.create({ doc, extensions: [noteLanguage] })
      const result = completeMarkdown(new CompletionContext(state, doc.length, false))
      if (!result || result instanceof Promise) return []
      return result.options.map(option => option.label)
    }
    expect(suggestions('/ta')).toContain('/table')
    expect(suggestions('::: pr')).toContain('proof')
    expect(suggestions('> [!cen')).toContain('center')
    expect(suggestions('```py')).toContain('python')
    expect(suggestions('$x + \\fr')).toContain('\\frac')
    expect(suggestions('$$\n\\fr')).toContain('\\frac')
    expect(suggestions('$$\nx^2\n$$\n\\fr')).toEqual([])
    expect(suggestions('普通段落 /ta')).toEqual([])
    expect(suggestions('```text\n/ta')).toEqual([])
  })
})
