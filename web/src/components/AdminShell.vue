<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppIcon from './AppIcon.vue'
import { logout as accountLogout } from '@/lib/account'
import { refreshSession, session } from '@/lib/session'
import { useTheme } from '@/lib/theme'
import { buildStateLabel, provideAdminPublish } from '@/lib/adminPublish'

const router = useRouter()
const route = useRoute()
const editorMode = computed(() => route.meta.adminEditor)
const authorized = computed(() => session.ready && session.user?.isAdmin)
const menuOpen = ref(false)
const signingOut = ref(false)
const { isDark, toggleTheme } = useTheme()
const publish = provideAdminPublish()
const groups = [
  { label: '内容', links: [
    { id: 'home', to: '/admin', icon: 'dashboard', label: '工作台' },
    { id: 'notes', to: '/admin/notes', icon: 'file', label: '笔记' },
    { id: 'assets', to: '/admin/assets', icon: 'image', label: '图床' },
  ] },
  { label: '站点', links: [
    { id: 'publish', to: '/admin/publish', icon: 'build', label: '发布' },
    { id: 'settings', to: '/admin/settings', icon: 'settings', label: '站点设置' },
  ] },
  { label: '账号', links: [
    { id: 'users', to: '/admin/users', icon: 'users', label: '用户管理' },
    { id: 'security', to: '/admin/security', icon: 'lock', label: '注册与安全' },
  ] },
]
const activeLabel = computed(() => groups.flatMap(group => group.links).find(item => item.id === route.meta.adminSection)?.label)
const publishLabel = computed(() => {
  const status = publish.status.value
  return status?.state === 'running' || status?.state === 'failed' ? buildStateLabel[status.state] : undefined
})

onMounted(async () => {
  if (!session.ready) await refreshSession()
  if (!session.user || !session.user.isAdmin) {
    await router.replace({ path: '/login', query: { next: route.fullPath } })
    return
  }
  void publish.refresh()
})
watch(() => route.fullPath, () => { menuOpen.value = false })

async function logout() {
  if (signingOut.value) return
  signingOut.value = true
  try {
    const failure = await router.replace('/login')
    if (failure) return
    await accountLogout()
  } catch {
    // 当前浏览器的会话状态已由 accountLogout 清理。
  } finally {
    signingOut.value = false
  }
}
</script>

<template>
  <div v-if="authorized" :class="['admin-shell', 'workspace-shell', { 'editor-route': editorMode }]">
    <aside v-if="!editorMode" class="admin-sidebar workspace-nav">
      <div class="workspace-nav-header">
        <RouterLink to="/admin" class="brand admin-brand" aria-label="后台工作台"><span class="brand-mark">秋</span><span><strong>站点后台</strong></span></RouterLink>
        <button class="workspace-mobile-menu button button-quiet" type="button" aria-label="后台菜单" aria-controls="workspace-navigation" :aria-expanded="menuOpen" @click="menuOpen = !menuOpen">
          <span>{{ activeLabel }}</span><AppIcon :name="menuOpen ? 'close' : 'menu'" :size="16" />
        </button>
      </div>
      <nav id="workspace-navigation" :class="['workspace-nav-links', { open: menuOpen }]" aria-label="后台导航">
        <section v-for="group in groups" :key="group.label">
          <h2>{{ group.label }}</h2>
          <RouterLink v-for="item in group.links" :key="item.id" :to="item.to"
            :class="{ active: route.meta.adminSection === item.id }"
            :aria-current="route.meta.adminSection === item.id ? 'page' : undefined" :title="item.label">
            <AppIcon :name="item.icon" :size="17" /><span>{{ item.label }}</span>
            <small v-if="item.id === 'publish' && publishLabel" class="workspace-nav-status" :title="publishLabel"><i />{{ publishLabel }}</small>
          </RouterLink>
        </section>
      </nav>
      <div :class="['workspace-nav-footer', { open: menuOpen }]">
        <RouterLink to="/" target="_blank" rel="noopener" class="workspace-site-link" title="查看网站"><AppIcon name="external" :size="16" /><span>查看网站</span></RouterLink>
        <div class="workspace-account-row">
          <RouterLink to="/account" class="admin-account" :title="session.user?.username" aria-label="我的账号"><AppIcon name="user" :size="16" /><span>{{ session.user?.username }}</span></RouterLink>
          <button class="icon-button" type="button" :aria-label="isDark ? '切换浅色' : '切换深色'" @click="toggleTheme"><AppIcon :name="isDark ? 'sun' : 'moon'" :size="16" /></button>
          <button class="icon-button" type="button" aria-label="退出登录" title="退出登录" :disabled="signingOut" @click="logout"><AppIcon name="logout" :size="16" /></button>
        </div>
      </div>
    </aside>
    <main id="main-content" class="admin-main"><slot /></main>
  </div>
  <div v-else class="admin-loading"><span class="loading-spinner" />正在验证后台会话…</div>
</template>
