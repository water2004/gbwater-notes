<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useHead } from '@unhead/vue'
import AppIcon from '@/components/AppIcon.vue'
import { forgotPassword } from '@/lib/account'
import { useEmailCode } from '@/lib/emailCode'

const router = useRouter()
const email = ref('')
const code = ref('')
const password = ref('')
const confirm = ref('')
const loading = ref(false)
const error = ref('')
const { sending, cooldown, error: codeError, message: codeMessage, send } = useEmailCode('reset')
const mismatch = computed(() => confirm.value !== '' && confirm.value !== password.value)
useHead({ title: '找回密码 · 碧山秋云' })

async function submit() {
  error.value = ''
  if (mismatch.value) {
    error.value = '两次输入的密码不一致'
    return
  }
  loading.value = true
  try {
    await forgotPassword(email.value.trim(), code.value.trim(), password.value)
    await router.replace({ path: '/login', query: { identifier: email.value.trim() } })
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '重置失败'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <main class="account-page">
    <form class="account-card" @submit.prevent="submit">
      <RouterLink to="/" class="brand"><span class="brand-mark">秋</span><span><strong>碧山秋云</strong><small>GBWATER NOTES</small></span></RouterLink>
      <h1>找回密码</h1>
      <p class="account-lead">通过注册邮箱接收验证码，重置后所有设备都需要重新登录。</p>
      <label class="form-field"><span>邮箱</span><input v-model="email" type="email" autocomplete="email" required placeholder="you@example.com"></label>
      <div class="form-field">
        <span>验证码</span>
        <div class="code-row">
          <input v-model="code" inputmode="numeric" autocomplete="one-time-code" required maxlength="6" placeholder="6 位数字">
          <button class="button button-quiet" type="button" :disabled="sending || cooldown > 0" @click="send(email)">
            {{ cooldown > 0 ? `${cooldown} 秒后重发` : sending ? '发送中…' : '发送验证码' }}
          </button>
        </div>
      </div>
      <p v-if="codeError" class="error-message">{{ codeError }}</p>
      <p v-else-if="codeMessage" class="form-hint">{{ codeMessage }}</p>
      <label class="form-field"><span>新密码</span><input v-model="password" type="password" autocomplete="new-password" required minlength="8" placeholder="至少 8 位，含两类字符"></label>
      <label class="form-field"><span>确认新密码</span><input v-model="confirm" type="password" autocomplete="new-password" required><small v-if="mismatch" class="form-hint form-hint-error">两次输入的密码不一致</small></label>
      <p v-if="error" class="error-message">{{ error }}</p>
      <button class="button button-primary" type="submit" :disabled="loading">{{ loading ? '正在重置…' : '重置密码' }}<AppIcon v-if="!loading" name="arrowRight" /></button>
      <div class="account-links"><RouterLink to="/login" class="text-link">想起来了，返回登录</RouterLink></div>
    </form>
  </main>
</template>
