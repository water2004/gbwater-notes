import { siteBase } from './paths'

export interface RenderedArticle {
  html: string
  revision: string
}

let initialArticle: RenderedArticle | undefined
const revisionPattern = /^[0-9a-f]{32}$/

export async function fetchArticle(slug: string, signal: AbortSignal): Promise<RenderedArticle> {
  const response = await fetch(`${siteBase}note-content/${encodeURIComponent(slug)}.json`, { signal })
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
