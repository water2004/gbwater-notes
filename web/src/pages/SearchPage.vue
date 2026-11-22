<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useHead } from '@unhead/vue'
import AppIcon from '@/components/AppIcon.vue'
import { api, ApiError } from '@/lib/api'
import { notePath } from '@/lib/content'
import { session } from '@/lib/session'
import type { SearchResult } from '@/types/content'

const route = useRoute()
const router = useRouter()
const query = ref(String(route.query.q || ''))
const results = ref<SearchResult[]>([])
const recent = ref<string[]>([])
const loading = ref(false)
const searched = ref(false)
const error = ref('')
const retryAfter = ref(0)
const input = ref<HTMLInputElement>()
const cache = new Map<string, SearchResult[]>()
let timer: ReturnType<typeof setTimeout> | undefined
let controller: AbortController | undefined
const loggedIn = computed(() => Boolean(session.user))
useHead({ title: '全文搜索 · 碧山秋云' })

function requireLogin() {
  router.push({ path: '/login', query: { next: route.fullPath } })
}

async function runSearch() {
  const term = query.value.trim()
  controller?.abort()
  error.value = ''
  retryAfter.value = 0
  if (!term || !loggedIn.value) {
    results.value = []
    loading.value = false
    searched.value = false
    return
  }
  const cached = cache.get(term)
  if (cached) {
    results.value = cached
    searched.value = true
    loading.value = false
    return
  }
  loading.value = true
  controller = new AbortController()
  const { signal } = controller
  try {
    const data = await api<{ results: SearchResult[] }>(`/api/search?q=${encodeURIComponent(term)}`, { signal })
    if (signal.aborted) return
    results.value = data.results
    cache.set(term, data.results)
    if (cache.size > 30) cache.delete(cache.keys().next().value || '')
    searched.value = true
    recent.value = [term, ...recent.value.filter(item => item !== term)].slice(0, 6)
    localStorage.setItem('gbwater-recent-searches', JSON.stringify(recent.value))
  } catch (reason) {
    if (signal.aborted) return
    if (reason instanceof ApiError && reason.status === 429) {
      retryAfter.value = reason.retryAfter || 0
      error.value = reason.message
    } else if (reason instanceof ApiError && reason.status === 401) {
      error.value = '登录状态已失效，请重新登录后搜索'
    } else {
      error.value = reason instanceof Error ? reason.message : '搜索暂时不可用'
    }
  } finally {
    if (!signal.aborted) loading.value = false
  }
}

function focusSearch() {
  nextTick(() => { input.value?.focus(); input.value?.select() })
}
function choose(term: string) {
  if (!loggedIn.value) { requireLogin(); return }
  query.value = term
  clearTimeout(timer)
  runSearch()
  router.replace({ query: { q: term } })
}
watch(query, () => {
  if (!loggedIn.value) return
  controller?.abort()
  clearTimeout(timer)
  timer = setTimeout(() => {
    runSearch()
    router.replace({ query: query.value.trim() ? { q: query.value.trim() } : {} })
  }, 320)
}, { flush: 'sync' })
watch(() => session.user?.id, () => {
  clearTimeout(timer)
  cache.clear()
  runSearch()
})
onMounted(() => {
  try { recent.value = JSON.parse(localStorage.getItem('gbwater-recent-searches') || '[]') } catch (_) { recent.value = [] }
  window.addEventListener('gbwater:search', focusSearch)
  if (query.value && loggedIn.value) runSearch()
})
onBeforeUnmount(() => {
  clearTimeout(timer)
  controller?.abort()
  window.removeEventListener('gbwater:search', focusSearch)
})
</script>

<template>
  <div class="page-shell search-page">
    <header class="page-hero compact-hero"><p class="eyebrow">SEARCH</p><h1>搜索笔记</h1><p>检索标题、课程和完整正文，支持常见拼写误差。</p></header>

    <section v-if="!loggedIn" class="search-gate">
      <AppIcon name="lock" :size="34" />
      <h2>搜索仅对登录用户开放</h2>
      <p>登录后可以全文检索，并按账号限制请求频率，避免接口被滥用。注册只需邮箱验证。</p>
      <div class="search-gate-actions">
        <RouterLink :to="{ path: '/login', query: { next: route.fullPath } }" class="button button-primary">登录<AppIcon name="arrowRight" :size="17" /></RouterLink>
        <RouterLink :to="{ path: '/register', query: { next: route.fullPath } }" class="button button-quiet">注册账号</RouterLink>
      </div>
    </section>

    <template v-else>
      <div class="search-box"><AppIcon name="search" :size="25" /><input ref="input" v-model="query" autofocus type="search" placeholder="输入概念、课程、公式或正文关键词…" aria-label="搜索全部公开笔记" @keydown.esc="query ? query = '' : input?.blur()"><button v-if="query" class="icon-button search-clear" aria-label="清空搜索" @click="query = ''; input?.focus()"><AppIcon name="close" /></button><kbd>Ctrl K</kbd></div>
      <p class="search-hint">每个账号每分钟最多 30 次搜索。</p>
      <div v-if="!query" class="search-suggestions"><span>{{ recent.length ? '最近搜索' : '可以试试' }}</span><button v-for="term in (recent.length ? recent : ['傅里叶', '拉格朗日', '量子比特', '中心极限定理', '晶格振动'])" :key="term" @click="choose(term)">{{ term }}</button></div>
      <p v-if="loading" class="search-status"><span class="loading-spinner" />正在检索…</p>
      <p v-else-if="error" class="error-message">{{ error }}<button v-if="!retryAfter" class="text-link" @click="runSearch">重试</button><span v-else class="search-hint-inline">请稍后再试</span></p>
      <template v-else-if="query && searched">
        <p class="result-count">“{{ query }}” 找到 {{ results.length }} 条相关笔记</p>
        <div v-if="results.length" class="search-results">
          <RouterLink v-for="result in results" :key="result.id" :to="notePath(result.slug)" class="search-result"><span>{{ result.course }}</span><h2>{{ result.title }}</h2><p>{{ result.excerpt }}</p><small>打开笔记<AppIcon name="arrowRight" :size="16" /></small></RouterLink>
        </div>
        <div v-else class="empty-state"><AppIcon name="search" :size="34" /><h2>没有找到相关内容</h2><p>试试更短的关键词、英文缩写或相近术语。</p></div>
      </template>
    </template>
  </div>
</template>
