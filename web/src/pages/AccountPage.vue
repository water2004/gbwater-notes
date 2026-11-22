<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useHead } from '@unhead/vue'
import AppIcon from '@/components/AppIcon.vue'
import { changePassword, logout, renameAccount } from '@/lib/account'
import { loadAnnotationSummary, type AnnotationSummary } from '@/lib/annotations'
import { getNote, notePath } from '@/lib/content'
import { useFavorites } from '@/lib/favorites'
import { useReadingProgress } from '@/lib/progress'
import { refreshSession, session } from '@/lib/session'

const router = useRouter()
const booting = ref(typeof window !== 'undefined')
const username = ref('')
const currentPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const profileMessage = ref('')
const profileError = ref('')
const passwordMessage = ref('')
const passwordError = ref('')
const savingProfile = ref(false)
const savingPassword = ref(false)
const { completed } = useReadingProgress()
const { favorites, toggleFavorite } = useFavorites()
const annotationSummary = ref<AnnotationSummary>()
const favoriteNotes = computed(() => [...favorites.value]
  .map(id => getNote(id))
  .filter((item): item is NonNullable<typeof item> => item !== undefined))
const mismatch = computed(() => confirmPassword.value !== '' && confirmPassword.value !== newPassword.value)
const joinedAt = computed(() => session.user ? new Date(session.user.createdAt).toLocaleDateString('zh-CN') : '')
useHead({ title: '我的账号 · 碧山秋云' })

onMounted(async () => {
  if (!session.ready) await refreshSession()
  booting.value = false
  if (session.user) {
    username.value = session.user.username
    annotationSummary.value = await loadAnnotationSummary().catch(() => undefined)
  } else await router.replace({ path: '/login', query: { next: '/account' } })
})

watch(() => session.user, user => {
  if (!user && !booting.value) void router.replace({ path: '/login', query: { next: '/account' } })
  else if (user) username.value = user.username
})

async function saveProfile() {
  savingProfile.value = true
  profileError.value = ''
  profileMessage.value = ''
  try {
    await renameAccount(username.value.trim())
    profileMessage.value = '用户名已更新'
  } catch (reason) {
    profileError.value = reason instanceof Error ? reason.message : '保存失败'
  } finally {
    savingProfile.value = false
  }
}

async function savePassword() {
  passwordError.value = ''
  passwordMessage.value = ''
  if (mismatch.value) {
    passwordError.value = '两次输入的新密码不一致'
    return
  }
  savingPassword.value = true
  try {
    await changePassword(currentPassword.value, newPassword.value)
    currentPassword.value = ''
    newPassword.value = ''
    confirmPassword.value = ''
    passwordMessage.value = '密码已更新，其他设备已退出登录'
  } catch (reason) {
    passwordError.value = reason instanceof Error ? reason.message : '修改失败'
  } finally {
    savingPassword.value = false
  }
}

async function signOut() {
  await logout()
  await router.replace('/')
}
</script>

<template>
  <div class="page-shell account-dashboard">
    <p v-if="booting" class="search-status"><span class="loading-spinner" />正在验证登录状态…</p>
    <template v-else-if="session.user">
      <header class="account-header">
        <div>
          <p class="eyebrow">ACCOUNT</p>
          <h1>{{ session.user.username }}</h1>
          <p class="account-meta">{{ session.user.email }} · 注册于 {{ joinedAt }}<span v-if="session.user.isAdmin" class="admin-badge">管理员</span></p>
        </div>
        <div class="account-header-actions">
          <RouterLink v-if="session.user.isAdmin" to="/admin" class="button button-quiet"><AppIcon name="dashboard" :size="16" />进入后台</RouterLink>
          <button class="button button-quiet" @click="signOut"><AppIcon name="logout" :size="16" />退出登录</button>
        </div>
      </header>

      <section class="account-stats">
        <div><strong>{{ completed.size }}</strong><small>已读笔记</small></div>
        <div><strong>{{ favorites.size }}</strong><small>收藏笔记</small></div>
        <div><strong>{{ session.user.role === 'admin' ? '管理员' : '读者' }}</strong><small>账号角色</small></div>
      </section>

      <div class="account-grid">
        <section class="account-panel">
          <h2>个人资料</h2>
          <form @submit.prevent="saveProfile">
            <label class="form-field"><span>用户名</span><input v-model="username" required minlength="2" maxlength="24"></label>
            <p class="form-hint">用户名用于登录和展示，修改后旧用户名立即失效。</p>
            <p v-if="profileError" class="error-message">{{ profileError }}</p>
            <p v-else-if="profileMessage" class="form-hint form-hint-success">{{ profileMessage }}</p>
            <button class="button button-primary" type="submit" :disabled="savingProfile">{{ savingProfile ? '保存中…' : '保存资料' }}</button>
          </form>
        </section>

        <section class="account-panel">
          <h2>修改密码</h2>
          <form @submit.prevent="savePassword">
            <label class="form-field"><span>当前密码</span><input v-model="currentPassword" type="password" autocomplete="current-password" required></label>
            <label class="form-field"><span>新密码</span><input v-model="newPassword" type="password" autocomplete="new-password" required minlength="8"></label>
            <label class="form-field"><span>确认新密码</span><input v-model="confirmPassword" type="password" autocomplete="new-password" required><small v-if="mismatch" class="form-hint form-hint-error">两次输入的密码不一致</small></label>
            <p v-if="passwordError" class="error-message">{{ passwordError }}</p>
            <p v-else-if="passwordMessage" class="form-hint form-hint-success">{{ passwordMessage }}</p>
            <button class="button button-primary" type="submit" :disabled="savingPassword">{{ savingPassword ? '修改中…' : '修改密码' }}</button>
          </form>
        </section>
      </div>

      <section class="account-panel annotation-summary">
        <div class="panel-heading"><h2>我的标注</h2><small>划线、书签与便条只有你自己可见</small></div>
        <div class="annotation-stats">
          <div><strong>{{ annotationSummary?.highlightCount ?? 0 }}</strong><small>划线</small></div>
          <div><strong>{{ annotationSummary?.bookmarkCount ?? 0 }}</strong><small>书签</small></div>
          <div><strong>{{ annotationSummary?.memoCount ?? 0 }}</strong><small>笔记</small></div>`n          <div><strong>{{ annotationSummary?.stickyCount ?? 0 }}</strong><small>便条</small></div>
        </div>
        <div v-if="annotationSummary?.recentHighlights.length" class="annotation-recent">
          <p class="sidebar-label">最近划线</p>
          <ul>
            <li v-for="item in annotationSummary.recentHighlights" :key="item.id">
              <RouterLink :to="notePath(item.noteId)">
                <span :class="['color-dot', item.color]" aria-hidden="true" />
                <span><strong>{{ item.noteTitle }}</strong><small>“{{ item.quote }}”</small></span>
                <AppIcon name="arrowRight" :size="15" />
              </RouterLink>
            </li>
          </ul>
        </div>
        <div v-if="annotationSummary?.recentBookmarks.length" class="annotation-recent">
          <p class="sidebar-label">最近书签</p>
          <ul>
            <li v-for="item in annotationSummary.recentBookmarks" :key="item.id">
              <RouterLink :to="notePath(item.noteId)">
                <AppIcon name="bookmark" :size="15" />
                <span><strong>{{ item.noteTitle }}</strong><small>{{ item.label || item.heading || '阅读位置' }}</small></span>
                <AppIcon name="arrowRight" :size="15" />
              </RouterLink>
            </li>
          </ul>
        </div>
      </section>

      <section class="account-panel account-favorites">
        <h2>我的收藏</h2>
        <p v-if="!favoriteNotes.length" class="empty-state-inline">还没有收藏。阅读笔记时点击右上角的心形按钮即可收藏。</p>
        <ul v-else>
          <li v-for="item in favoriteNotes" :key="item.id">
            <RouterLink :to="notePath(item)"><strong>{{ item.title }}</strong><small>{{ item.course }} · {{ item.readingMinutes }} 分钟</small></RouterLink>
            <button class="icon-button" aria-label="取消收藏" @click="toggleFavorite(item.id)"><AppIcon name="heart" :size="16" /></button>
          </li>
        </ul>
      </section>
    </template>
  </div>
</template>
