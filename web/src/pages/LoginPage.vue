<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useHead } from '@unhead/vue'
import AppIcon from '@/components/AppIcon.vue'
import { login } from '@/lib/account'

const route = useRoute()
const router = useRouter()
const identifier = ref(typeof route.query.identifier === 'string' ? route.query.identifier : '')
const password = ref('')
const loading = ref(false)
const error = ref('')
useHead({ title: '登录 · 碧山秋云' })

async function submit() {
  loading.value = true
  error.value = ''
  try {
    const user = await login(identifier.value, password.value)
    const next = typeof route.query.next === 'string' && route.query.next.startsWith('/') ? route.query.next : user.isAdmin ? '/admin' : '/account'
    await router.replace(next)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '登录失败'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <main class="account-page">
    <form class="account-card" @submit.prevent="submit">
      <RouterLink to="/" class="brand"><span class="brand-mark">秋</span><span><strong>碧山秋云</strong><small>GBWATER NOTES</small></span></RouterLink>
      <h1>登录</h1>
      <p class="account-lead">登录后可在不同设备同步阅读进度与收藏。</p>
      <label class="form-field"><span>邮箱或用户名</span><input v-model="identifier" autocomplete="username" required autofocus></label>
      <label class="form-field"><span>密码</span><input v-model="password" type="password" autocomplete="current-password" required></label>
      <p v-if="error" class="error-message">{{ error }}</p>
      <button class="button button-primary" type="submit" :disabled="loading">{{ loading ? '正在登录…' : '登录' }}<AppIcon v-if="!loading" name="arrowRight" /></button>
      <div class="account-links">
        <RouterLink to="/forgot" class="text-link">忘记密码</RouterLink>
        <RouterLink :to="{ path: '/register', query: route.query }" class="text-link">注册新账号</RouterLink>
      </div>
      <RouterLink to="/" class="text-link account-back">返回公开网站</RouterLink>
    </form>
  </main>
</template>
