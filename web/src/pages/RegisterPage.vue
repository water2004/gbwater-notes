<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useHead } from '@unhead/vue'
import AppIcon from '@/components/AppIcon.vue'
import { register } from '@/lib/account'
import { useEmailCode } from '@/lib/emailCode'
import { api } from '@/lib/api'

const route = useRoute()
const router = useRouter()
const username = ref('')
const email = ref('')
const emailInput = ref<HTMLInputElement>()
const localPart = ref('')
const domain = ref('')
const options = ref<{ enabled: boolean, emailDomains: string[] }>()
const optionsLoading = ref(true)
const optionsError = ref('')
const request = new AbortController()
const allowedDomains = computed(() => options.value?.emailDomains ?? [])
const address = computed(() => allowedDomains.value.length
  ? localPart.value.trim() && domain.value ? `${localPart.value.trim()}@${domain.value}` : ''
  : email.value.trim())
const code = ref('')
const password = ref('')
const confirm = ref('')
const loading = ref(false)
const error = ref('')
const { sending, cooldown, error: codeError, message: codeMessage, send } = useEmailCode('register')
const mismatch = computed(() => confirm.value !== '' && confirm.value !== password.value)
useHead({ title: '注册 · 碧山秋云' })

async function loadOptions() {
  optionsLoading.value = true
  optionsError.value = ''
  try {
    options.value = await api('/auth/registration', { signal: request.signal })
    domain.value = options.value?.emailDomains[0] ?? ''
  } catch (reason) {
    if (!request.signal.aborted) optionsError.value = reason instanceof Error ? reason.message : '无法读取注册设置'
  } finally {
    optionsLoading.value = false
  }
}

function updateLocalPart(event: Event) {
  const input = event.currentTarget as HTMLInputElement
  const value = input.value.trim()
  const at = value.indexOf('@')
  const suffix = at > 0 ? value.slice(at + 1).toLowerCase() : ''
  if (allowedDomains.value.includes(suffix)) {
    domain.value = suffix
    input.value = value.slice(0, at)
  }
  localPart.value = input.value
}

function sendCode() {
  if (options.value?.enabled && emailInput.value?.reportValidity()) void send(address.value)
}

watch(address, () => { code.value = ''; codeError.value = ''; codeMessage.value = '' })
onMounted(loadOptions)
onBeforeUnmount(() => request.abort())

async function submit() {
  if (!options.value?.enabled || !address.value) return
  error.value = ''
  if (mismatch.value) {
    error.value = '两次输入的密码不一致'
    return
  }
  loading.value = true
  try {
    await register({ username: username.value, email: address.value, password: password.value, code: code.value.trim() })
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
      <p v-if="optionsLoading" class="account-lead" role="status">正在读取注册设置…</p>
      <p v-else-if="optionsError" class="error-message" role="alert">{{ optionsError }} <button type="button" class="text-link" @click="loadOptions">重试</button></p>
      <p v-else-if="!options?.enabled" class="account-lead">站点暂未开放注册。</p>
      <template v-else>
      <p class="account-lead">注册需要验证邮箱。</p>
      <label class="form-field"><span>用户名</span><input v-model="username" autocomplete="nickname" required minlength="2" maxlength="24" placeholder="2–24 个字符，中英文均可"></label>
      <div v-if="allowedDomains.length" class="form-field">
        <label for="register-email">邮箱</label>
        <div class="email-suffix-row">
          <input id="register-email" ref="emailInput" :value="localPart" type="text" inputmode="email" autocapitalize="none" autocomplete="off" spellcheck="false" required maxlength="64" pattern="[^@\s]+" title="填写 @ 前的部分" placeholder="@ 前的部分" :disabled="sending || loading" @input="updateLocalPart">
          <select v-model="domain" aria-label="邮箱后缀" :disabled="sending || loading">
            <option v-for="suffix in allowedDomains" :key="suffix" :value="suffix">@{{ suffix }}</option>
          </select>
        </div>
      </div>
      <label v-else class="form-field"><span>邮箱</span><input ref="emailInput" v-model="email" type="email" autocomplete="email" required placeholder="you@example.com" :disabled="sending || loading"></label>
      <div class="form-field">
        <span>邮箱验证码</span>
        <div class="code-row">
          <input v-model="code" inputmode="numeric" autocomplete="one-time-code" required maxlength="6" placeholder="6 位数字">
          <button class="button button-quiet" type="button" :disabled="sending || loading || cooldown > 0" @click="sendCode">
            {{ cooldown > 0 ? `${cooldown} 秒后重发` : sending ? '发送中…' : '发送验证码' }}
          </button>
        </div>
      </div>
      <p v-if="codeError" class="error-message">{{ codeError }}</p>
      <p v-else-if="codeMessage" class="form-hint">{{ codeMessage }}</p>
      <label class="form-field"><span>密码</span><input v-model="password" type="password" autocomplete="new-password" required minlength="8" placeholder="至少 8 位，含两类字符"></label>
      <label class="form-field"><span>确认密码</span><input v-model="confirm" type="password" autocomplete="new-password" required><small v-if="mismatch" class="form-hint form-hint-error">两次输入的密码不一致</small></label>
      <p v-if="error" class="error-message">{{ error }}</p>
      <button class="button button-primary" type="submit" :disabled="loading || sending">{{ loading ? '正在创建账号…' : '创建账号' }}<AppIcon v-if="!loading" name="arrowRight" /></button>
      </template>
      <div class="account-links">
        <RouterLink :to="{ path: '/login', query: route.query }" class="text-link">已有账号，去登录</RouterLink>
      </div>
    </form>
  </main>
</template>
