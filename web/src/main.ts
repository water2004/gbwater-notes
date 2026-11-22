import { ViteSSG } from 'vite-ssg'
import App from './App.vue'
import { routes } from './router'
import { refreshSession } from '@/lib/session'
import { catalogUnavailable, initializeCatalog } from '@/lib/content'
import { captureInitialArticle } from './lib/initial-article'
import type { NoteMeta } from './types/content'
import './styles/controls.css'
import './styles/main.css'
import './styles/admin-navigation.css'
import './styles/admin-workspace.css'
import './styles/site-settings.css'

// 必须在 Vue 挂载前执行：此时服务端渲染的正文还在 DOM 里。
if (typeof window !== 'undefined') captureInitialArticle()

async function loadCatalog() {
  const response = await fetch(`${import.meta.env.BASE_URL}catalog.json`)
  if (!response.ok || !response.headers.get('Content-Type')?.includes('application/json')) {
    throw new Error(`笔记目录暂时无法加载（${response.status}）`)
  }
  initializeCatalog(await response.json() as NoteMeta[])
}

export const createApp = ViteSSG(
  App,
  {
    routes,
    base: import.meta.env.BASE_URL,
    scrollBehavior(to, _from, savedPosition) {
      if (savedPosition) return savedPosition
      if (to.hash) return { el: to.hash, top: 96 }
      return { top: 0 }
    },
  },
  async ({ isClient, head, router }) => {
    head?.push({ htmlAttrs: { lang: 'zh-CN' } })
    if (!isClient) return
    void refreshSession()
    let catalogLoad: Promise<void> | undefined
    const initialRoute = router.resolve(router.options.history.location)
    if (initialRoute.meta.requiresCatalog && document.querySelector('#app[data-server-rendered]')) {
      // 目录请求失败时，保留已经预渲染的正文。
      catalogLoad = loadCatalog()
      try {
        await catalogLoad
      } catch (error) {
        const notice = document.createElement('p')
        notice.className = 'error-message'
        notice.setAttribute('role', 'alert')
        notice.textContent = '笔记目录暂时无法加载，请刷新页面重试。'
        document.querySelector('#app')?.prepend(notice)
        throw error
      }
    }
    router.beforeEach(async to => {
      if (to.meta.admin) catalogLoad = undefined
      if (!to.meta.requiresCatalog) return
      const pending = (catalogLoad ??= loadCatalog())
      try {
        await pending
        if (catalogLoad === pending) catalogUnavailable.value = false
      } catch {
        if (catalogLoad === pending) {
          catalogLoad = undefined
          catalogUnavailable.value = true
        }
      }
    })
  },
)
