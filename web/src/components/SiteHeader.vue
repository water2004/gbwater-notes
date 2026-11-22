<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppIcon from './AppIcon.vue'
import { useTheme } from '@/lib/theme'
import { session } from '@/lib/session'

const route = useRoute()
const router = useRouter()
const menuOpen = ref(false)
const hidden = ref(false)
let lastY = 0
const { isDark, toggleTheme } = useTheme()
watch(() => route.fullPath, () => { menuOpen.value = false; hidden.value = false })

// 向下滚动收起导航，向上滚动立即出现；接近顶部时强制显示。
function onScroll() {
  const y = window.scrollY
  if (y < 90) hidden.value = false
  else if (y - lastY > 6) hidden.value = true
  else if (lastY - y > 6) hidden.value = false
  lastY = y
}
async function openSearch() {
  await router.push('/search')
  window.dispatchEvent(new Event('gbwater:search'))
}
function keyboard(event: KeyboardEvent) {
  const target = event.target as HTMLElement | null
  const editing = target?.matches('input, textarea, select, [contenteditable="true"]')
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') {
    event.preventDefault()
    openSearch()
  } else if (event.key === '/' && !editing) {
    event.preventDefault()
    openSearch()
  }
}
onMounted(() => {
  lastY = window.scrollY
  window.addEventListener('keydown', keyboard)
  window.addEventListener('scroll', onScroll, { passive: true })
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', keyboard)
  window.removeEventListener('scroll', onScroll)
})
</script>

<template>
  <header class="site-header" :class="{ 'is-hidden': hidden }">
    <div class="header-inner">
      <RouterLink to="/" class="brand" aria-label="碧山秋云首页">
        <span class="brand-mark">秋</span>
        <span><strong>碧山秋云</strong><small>GBWATER NOTES</small></span>
      </RouterLink>
      <button class="icon-button mobile-menu" :aria-expanded="menuOpen" :aria-label="menuOpen ? '关闭导航' : '打开导航'" aria-controls="main-navigation" @click="menuOpen = !menuOpen">
        <AppIcon :name="menuOpen ? 'close' : 'menu'" />
      </button>
      <nav id="main-navigation" :class="['main-nav', { open: menuOpen }]" aria-label="主导航">
        <RouterLink to="/" exact-active-class="active"><AppIcon name="home" :size="17" />首页</RouterLink>
        <RouterLink to="/library" :class="{ active: route.path.startsWith('/library') || route.path.startsWith('/courses') || route.path.startsWith('/notes') }"><AppIcon name="library" :size="17" />笔记库</RouterLink>
        <RouterLink to="/search" active-class="active"><AppIcon name="search" :size="17" />搜索</RouterLink>
        <RouterLink to="/about" active-class="active">关于</RouterLink>
      </nav>
      <div class="header-actions">
        <RouterLink v-if="session.user" to="/account" class="header-account" :title="`${session.user.username} · ${session.user.email}`">
          <AppIcon name="user" :size="17" /><span>{{ session.user.username }}</span>
        </RouterLink>
        <RouterLink v-else to="/login" class="header-account" title="登录或注册">
          <AppIcon name="user" :size="17" /><span>登录</span>
        </RouterLink>
        <button class="icon-button" :aria-label="isDark ? '切换为浅色模式' : '切换为深色模式'" @click="toggleTheme">
          <AppIcon :name="isDark ? 'sun' : 'moon'" />
        </button>
      </div>
    </div>
  </header>
</template>
