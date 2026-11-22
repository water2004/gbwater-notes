<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useHead } from '@unhead/vue'
import AppIcon from '@/components/AppIcon.vue'
import { register } from '@/lib/account'
import { useEmailCode } from '@/lib/emailCode'

const route = useRoute()
const router = useRouter()
const username = ref('')
const email = ref('')
const code = ref('')
const password = ref('')
const confirm = ref('')
const loading = ref(false)
const error = ref('')
const { sending, cooldown, error: codeError, message: codeMessage, send } = useEmailCode('register')
const mismatch = computed(() => confirm.value !== '' && confirm.value !== password.value)
useHead({ title: '注册 · 碧山秋云' })

async function submit() {
  error.value = ''
  if (mismatch.value) {
    error.value = '两次输入的密码不一致'
    return
  }
  loading.value = true
  try {
    await register({ username: username.value, email: email.value.trim(), password: password.value, code: code.value.trim() })
    const next = typeof route.query.next === 'string' && route.query.next.startsWith('/') ? route.query.next : '/account'
    await router.replace(next)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '注册失败'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <main class="account-page">
    <form class="account-card" @submit.prevent="submit">
      <RouterLink to="/" class="brand"><span class="brand-mark">秋</span><span><strong>碧山秋云</strong><small>GBWATER NOTES</small></span></RouterLink>
      <h1>注册账号</h1>
      <p class="account-lead">注册后可在不同设备同步阅读进度与收藏。需要邮箱验证。</p>
      <label class="form-field"><span>用户名</span><input v-model="username" autocomplete="nickname" required minlength="2" maxlength="24" placeholder="2–24 个字符，中英文均可"></label>
      <label class="form-field"><span>邮箱</span><input v-model="email" type="email" autocomplete="email" required placeholder="you@example.com"></label>
      <div class="form-field">
        <span>邮箱验证码</span>
        <div class="code-row">
          <input v-model="code" inputmode="numeric" autocomplete="one-time-code" required maxlength="6" placeholder="6 位数字">
          <button class="button button-quiet" type="button" :disabled="sending || cooldown > 0" @click="send(email)">
            {{ cooldown > 0 ? `${cooldown} 秒后重发` : sending ? '发送中…' : '发送验证码' }}
          </button>
        </div>
      </div>
      <p v-if="codeError" class="error-message">{{ codeError }}</p>
      <p v-else-if="codeMessage" class="form-hint">{{ codeMessage }}</p>
      <label class="form-field"><span>密码</span><input v-model="password" type="password" autocomplete="new-password" required minlength="8" placeholder="至少 8 位，含两类字符"></label>
      <label class="form-field"><span>确认密码</span><input v-model="confirm" type="password" autocomplete="new-password" required><small v-if="mismatch" class="form-hint form-hint-error">两次输入的密码不一致</small></label>
      <p v-if="error" class="error-message">{{ error }}</p>
      <button class="button button-primary" type="submit" :disabled="loading">{{ loading ? '正在创建账号…' : '创建账号' }}<AppIcon v-if="!loading" name="arrowRight" /></button>
      <div class="account-links">
        <RouterLink :to="{ path: '/login', query: route.query }" class="text-link">已有账号，去登录</RouterLink>
      </div>
    </form>
  </main>
</template>
