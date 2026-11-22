<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useHead } from '@unhead/vue'
import AppIcon from '@/components/AppIcon.vue'
import AdminDialog from '@/components/AdminDialog.vue'
import { api } from '@/lib/api'
import { session, type AccountUser as AdminUser } from '@/lib/session'

const users = ref<AdminUser[]>([])
const total = ref(0)
const query = ref('')
const loading = ref(true)
const error = ref('')
const notice = ref('')
const page = ref(1)
const pageSize = 20
const busy = ref('')
const managed = ref<AdminUser>()
const detailError = ref('')
const detailNotice = ref('')
let request: AbortController | undefined
let searchTimer: ReturnType<typeof setTimeout> | undefined
const temporaryPassword = ref('')
useHead({ title: '用户管理 · 碧山秋云' })

const pageCount = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
const rangeStart = computed(() => total.value ? (page.value - 1) * pageSize + 1 : 0)
const rangeEnd = computed(() => Math.min(page.value * pageSize, total.value))

async function load() {
  clearTimeout(searchTimer)
  request?.abort()
  const current = new AbortController()
  request = current
  loading.value = true
  error.value = ''
  try {
    const params = new URLSearchParams({ limit: String(pageSize), offset: String((page.value - 1) * pageSize) })
    if (query.value.trim()) params.set('query', query.value.trim())
    const data = await api<{ users: AdminUser[]; total: number }>(`/api/admin/users?${params}`, { signal: current.signal })
    if (current.signal.aborted) return
    total.value = data.total
    if (page.value > pageCount.value) {
      page.value = pageCount.value
      return
    }
    users.value = data.users
  } catch (reason) {
    if (!current.signal.aborted) error.value = reason instanceof Error ? reason.message : '读取用户失败'
  } finally {
    if (!current.signal.aborted) loading.value = false
  }
}

watch(page, load)
watch(query, () => {
  clearTimeout(searchTimer)
  request?.abort()
  searchTimer = setTimeout(() => {
    if (page.value === 1) void load()
    else page.value = 1
  }, 250)
})

onMounted(load)
onBeforeUnmount(() => {
  clearTimeout(searchTimer)
  request?.abort()
})

function manage(user?: AdminUser) {
  if (busy.value) return
  managed.value = user
  temporaryPassword.value = ''
  detailError.value = ''
  detailNotice.value = ''
}

async function patch(user: AdminUser, payload: { role?: string; status?: string }) {
  busy.value = user.id
  detailError.value = ''
  detailNotice.value = ''
  try {
    const data = await api<{ user: AdminUser }>(`/api/admin/users/${user.id}`, { method: 'PATCH', json: payload })
    users.value = users.value.map(item => item.id === data.user.id ? data.user : item)
    managed.value = data.user
    detailNotice.value = `已更新 ${data.user.username}`
  } catch (reason) {
    detailError.value = reason instanceof Error ? reason.message : '更新失败'
  } finally {
    busy.value = ''
  }
}

function changeRole(user: AdminUser, event: Event) {
  const select = event.target as HTMLSelectElement
  const role = select.value
  select.value = user.role
  void patch(user, { role })
}

async function resetPassword(user: AdminUser) {
  if (!window.confirm(`确定为 ${user.username} 重置密码？该用户的所有登录会话会立即失效。`)) return
  busy.value = user.id
  detailError.value = ''
  detailNotice.value = ''
  temporaryPassword.value = ''
  try {
    const data = await api<{ password: string }>(`/api/admin/users/${user.id}/password`, { method: 'POST', json: {} })
    temporaryPassword.value = data.password
    detailNotice.value = `已为 ${user.username} 生成临时密码，请通过安全渠道转交`
  } catch (reason) {
    detailError.value = reason instanceof Error ? reason.message : '重置失败'
  } finally {
    busy.value = ''
  }
}

async function remove(user: AdminUser) {
  if (!window.confirm(`确定删除用户 ${user.username}？其阅读进度与收藏会一并删除，且无法恢复。`)) return
  busy.value = user.id
  detailError.value = ''
  detailNotice.value = ''
  try {
    await api(`/api/admin/users/${user.id}`, { method: 'DELETE' })
    managed.value = undefined
    temporaryPassword.value = ''
    notice.value = `已删除 ${user.username}`
    await load()
  } catch (reason) {
    detailError.value = reason instanceof Error ? reason.message : '删除失败'
  } finally {
    busy.value = ''
  }
}

async function copyPassword() {
  try {
    await navigator.clipboard.writeText(temporaryPassword.value)
    detailNotice.value = '临时密码已复制到剪贴板'
  } catch (_) {
    detailNotice.value = '复制失败，请手动选择文本'
  }
}

function formatDate(value?: string) {
  return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—'
}
function isSelf(user: AdminUser) { return session.user?.id === user.id }
</script>

<template>
  <div>
    <header class="admin-page-header">
      <div><h1>用户管理</h1><p>查找账号，管理角色、状态和登录凭据。</p></div>
    </header>

    <div class="admin-toolbar">
      <label class="admin-filter-search"><AppIcon name="search" :size="17" /><input v-model="query" type="search" placeholder="搜索用户名或邮箱" aria-label="搜索用户"></label>
      <span class="notes-list-meta-inline">共 {{ total }} 位用户</span>
    </div>

    <p v-if="error" class="error-message">{{ error }}</p>
    <p v-else-if="notice" class="form-hint form-hint-success">{{ notice }}</p>
    <p v-if="loading" class="search-status"><span class="loading-spinner" />正在读取用户…</p>
    <div v-else-if="users.length" class="admin-table-wrap">
      <table class="admin-table">
        <thead><tr><th>用户</th><th>角色</th><th>状态</th><th>注册时间</th><th>最近登录</th><th>操作</th></tr></thead>
        <tbody>
          <tr v-for="user in users" :key="user.id">
            <td data-label="用户"><strong>{{ user.username }}</strong><small class="table-secondary">{{ user.email }}</small></td>
            <td data-label="角色">{{ user.role === 'admin' ? '管理员' : '读者' }}</td>
            <td data-label="状态"><span :class="['admin-status', { failed: user.status === 'disabled' }]">{{ user.status === 'active' ? '正常' : '已停用' }}</span></td>
            <td data-label="注册时间"><time :datetime="user.createdAt">{{ formatDate(user.createdAt) }}</time></td>
            <td data-label="最近登录">{{ formatDate(user.lastLoginAt) }}</td>
            <td data-label="操作"><button type="button" class="text-link" :aria-label="`管理 ${user.username}`" :disabled="!!busy" @click="manage(user)">管理</button></td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-else-if="!loading" class="empty-state"><AppIcon name="user" :size="30" /><h2>没有匹配的用户</h2><p>换个关键词再试。</p></div>

    <nav v-if="pageCount > 1" class="admin-pagination" aria-label="用户列表分页">
      <button class="button button-quiet" :disabled="page <= 1" @click="page--"><AppIcon name="arrowLeft" :size="15" />上一页</button>
      <span>第 {{ page }} / {{ pageCount }} 页（显示 {{ rangeStart }}–{{ rangeEnd }}）</span>
      <button class="button button-quiet" :disabled="page >= pageCount" @click="page++">下一页<AppIcon name="arrowRight" :size="15" /></button>
    </nav>
    <AdminDialog v-if="managed" title="账号管理" class="user-management-dialog" @close="manage()">
      <div class="user-management">
        <div class="user-management-identity"><strong>{{ managed.username }}</strong><small>{{ managed.email }}</small></div>
        <p v-if="detailError" class="error-message" role="alert">{{ detailError }}</p>
        <p v-if="detailNotice" class="form-hint" role="status">{{ detailNotice }}</p>
        <section>
          <h3>角色与状态</h3>
          <label class="form-field"><span>角色</span>
            <select :value="managed.role" :disabled="!!busy || isSelf(managed)" @change="changeRole(managed, $event)">
              <option value="user">读者</option><option value="admin">管理员</option>
            </select>
          </label>
          <button class="button button-quiet" type="button" :disabled="!!busy || isSelf(managed)"
            @click="patch(managed, { status: managed.status === 'active' ? 'disabled' : 'active' })">{{ managed.status === 'active' ? '停用账号' : '启用账号' }}</button>
        </section>
        <section>
          <h3>登录凭据</h3>
          <button class="button button-quiet" type="button" :disabled="!!busy" @click="resetPassword(managed)">重置密码</button>
          <div v-if="temporaryPassword" class="admin-secret">
            <span>临时密码</span><code>{{ temporaryPassword }}</code>
            <button type="button" class="button button-quiet" @click="copyPassword">复制</button>
          </div>
        </section>
        <section><button type="button" class="text-link danger" :disabled="!!busy || isSelf(managed)" @click="remove(managed)">删除账号</button></section>
      </div>
    </AdminDialog>
  </div>
</template>
