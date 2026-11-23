// 构建期渲染出来的笔记正文只存在于首次 HTML 中。vite-ssg 的客户端会在异步路由
// 组件解析完成后重新挂载并清空容器，所以必须在入口挂载前把正文取走。
export interface RenderedArticle {
  html: string
  revision: string
}

let initialArticle: RenderedArticle | undefined
const revisionPattern = /^[0-9a-f]{32}$/

// 同一标识供 Vite SSG 构建钩子与 Vue 的预渲染页面使用。
export const prerenderedArticle = Symbol.for('gbwater.prerenderedArticle')

export async function fetchArticle(slug: string, signal: AbortSignal): Promise<RenderedArticle> {
  const response = await fetch(`${import.meta.env.BASE_URL}note-content/${encodeURIComponent(slug)}.json`, { signal })
  if (!response.ok || !response.headers.get('Content-Type')?.includes('application/json')) {
    throw new Error(`无法加载笔记正文（${response.status}）`)
  }
  const article = await response.json() as Partial<RenderedArticle> | null
  if (typeof article?.html !== 'string' || typeof article.revision !== 'string' || !revisionPattern.test(article.revision)) {
    throw new Error('笔记正文版本无效')
  }
  return { html: article.html, revision: article.revision }
}

export function captureInitialArticle() {
  if (typeof document === 'undefined') return
  const element = document.querySelector<HTMLElement>('.note-article')
  const revision = element?.dataset.annotationRevision
  if (element && revision && revisionPattern.test(revision)) {
    initialArticle = { html: element.innerHTML, revision }
  }
}

// takeInitialArticle 取一次即清空，避免站内切换时误用上一篇的正文。
export function takeInitialArticle(): RenderedArticle | undefined {
  const value = initialArticle
  initialArticle = undefined
  return value
}
