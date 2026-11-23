import { createApp } from 'vue'
import { createHead } from '@unhead/vue/client'
import { createRouter, createWebHistory } from 'vue-router'
import App from './App.vue'
import { routes } from './router'
import { refreshSession } from '@/lib/session'
import { catalogUnavailable, initializeCatalog } from '@/lib/content'
import { normalizeSiteSettings, siteSettings } from '@/lib/siteSettings'
import { siteBase } from '@/lib/paths'
import { captureInitialArticle } from './lib/initial-article'
import type { NoteMeta } from './types/content'
import './styles/controls.css'
import './styles/main.css'
import './styles/admin-navigation.css'
import './styles/admin-workspace.css'
import './styles/site-settings.css'

captureInitialArticle()

const router = createRouter({
  history: createWebHistory(siteBase),
  routes,
  scrollBehavior(to, _from, savedPosition) {
    if (savedPosition) return savedPosition
    if (to.hash) return { el: to.hash, top: 96 }
    return { top: 0 }
  },
})

async function fetchSiteFile(name: string) {
  const response = await fetch(`${siteBase}${name}`)
  if (!response.ok || !response.headers.get('Content-Type')?.includes('application/json')) {
    throw new Error(`无法加载 ${name}（${response.status}）`)
  }
  return response.json()
}

const settingsLoad = fetchSiteFile('site-settings.json')
  .then(settings => Object.assign(siteSettings, normalizeSiteSettings(settings)))
  .catch(() => {
    // 登录和后台在首次发布前使用默认设置。
  })

let catalogLoad: Promise<void> | undefined
router.beforeEach(async to => {
  await settingsLoad
  if (to.meta.admin) catalogLoad = undefined
  if (!to.meta.requiresCatalog) return
  const pending = (catalogLoad ??= fetchSiteFile('catalog.json').then(notes => initializeCatalog(notes as NoteMeta[])))
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

const app = createApp(App)
app.use(createHead())
app.use(router)
void refreshSession()
void router.isReady().then(() => {
  const container = document.getElementById('app')
  if (catalogUnavailable.value && container?.querySelector('main')) return
  app.mount('#app')
})
