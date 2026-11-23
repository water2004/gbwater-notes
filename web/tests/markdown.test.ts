import { describe, expect, it } from 'vitest'
import { markdownBody, renderMarkdown } from '@/lib/markdown'
import { coursePath, headingSlug, initializeCatalog, notePath } from '@/lib/content'

const native = {
  math: {
    formula: { tex: 'x^2', display: false, width: 20, height: 16, baseline: 13 },
    display: { tex: '\\int_0^1 x dx', display: true, width: 100, height: 50, baseline: 30 },
    cases: { tex: '\\begin{cases}\n        x&=1\\\\\n        x&=2\n    \\end{cases}', display: true, width: 80, height: 60, baseline: 35 },
  },
  mermaid: {
    diagram: { source: 'flowchart TD\nA-->B', width: 100, height: 80 },
  },
}

describe('Markdown native rendering', () => {
  it('strips frontmatter from both LF and CRLF source files', () => {
    expect(markdownBody('---\r\ntitle: "测试"\r\n---\r\n\r\n正文')).toBe('正文')
    expect(markdownBody('---\ntitle: "测试"\n---\n\n正文')).toBe('正文')
  })

  it('renders inline and display math with intrinsic geometry', () => {
    const html = renderMarkdown('公式 $x^2$。\n\n$$\n\\int_0^1 x dx\n$$', native, '/mypdf/')
    expect(html).toContain('/mypdf/generated/math/formula.svg')
    expect(html).toContain('vertical-align:-3.000px')
    expect(html).toContain('class="math-display"')
    expect(html).toContain('/mypdf/generated/math/display.svg')
    expect(html).toContain('alt=""')
    expect(html).not.toContain('x^2')
    expect(html).not.toContain('\\int_0^1')
  })

  it('honours an arbitrary deployment base path', () => {
    const html = renderMarkdown('公式 $x^2$', native, '/notes-app/')
    expect(html).toContain('/notes-app/generated/math/formula.svg')
    expect(html).not.toContain('/mypdf/')
  })

  it('rewrites legacy /mypdf/ image prefixes to the configured base', () => {
    const html = renderMarkdown('![示意图](/mypdf/content-assets/math/demo/a.png)', native, '/notes-app/')
    expect(html).toContain('/notes-app/content-assets/math/demo/a.png')
    expect(html).not.toContain('/mypdf/')
  })

  it('matches multiline formulas despite block indentation', () => {
    const html = renderMarkdown('$$\n\\begin{cases}\nx&=1\\\\\nx&=2\n\\end{cases}\n$$', native, '/mypdf/')
    expect(html).toContain('/mypdf/generated/math/cases.svg')
    expect(html).not.toContain('math-fallback')
  })

  it('renders both Mermaid themes without a JavaScript runtime', () => {
    const html = renderMarkdown('```mermaid\nflowchart TD\nA-->B\n```', native, '/mypdf/')
    expect(html).toContain('diagram-light.svg')
    expect(html).toContain('diagram-dark.svg')
    expect(html).not.toContain('<script')
    expect(html).not.toContain('flowchart TD')
  })

  it('keeps LaTeX and Mermaid source out of preview failures', () => {
    const html = renderMarkdown('$missingTex$\n\n```mermaid\nflowchart LR\nA-->B\n```')
    expect(html).toContain('公式未渲染')
    expect(html).toContain('图表未渲染')
    expect(html).not.toContain('missingTex')
    expect(html).not.toContain('flowchart LR')
  })

  it('escapes raw HTML and preserves heading anchors', () => {
    const html = renderMarkdown('## 傅里叶 变换\n\n<script>alert(1)</script>', native)
    expect(html).toContain('id="傅里叶-变换"')
    expect(html).toContain('&lt;script&gt;')
  })
})

describe('content URLs', () => {
  it('creates stable ASCII note and course URLs with Chinese heading slugs', () => {
    initializeCatalog([{
      id: 'math/数学分析B2/Fourier分析', category: 'math', categoryTitle: '数学',
      title: 'Fourier分析', course: '数学分析B2', slug: 'fourier-fen-xi', courseSlug: 'shu-xue-fen-xi-b2',
      order: 1, draft: false, excerpt: '', characters: 0, readingMinutes: 1, headings: [],
    }])
    expect(notePath('math/数学分析B2/Fourier分析')).toBe('/notes/fourier-fen-xi/')
    expect(notePath({ slug: 'dirac-notation' })).toBe('/notes/dirac-notation/')
    expect(coursePath({ courseSlug: 'quantum-mechanics' })).toBe('/courses/quantum-mechanics/')
    expect(headingSlug(' Fourier 分析 ')).toBe('fourier-分析')
  })
})
