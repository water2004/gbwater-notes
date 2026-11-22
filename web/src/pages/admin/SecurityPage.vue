<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { useHead } from '@unhead/vue'
import { api } from '@/lib/api'

interface AuthPolicy {
  rateEnabled: boolean
  rateAttempts: number
  rateWindowMinutes: number
  emailMode: 'disabled' | 'allowlist' | 'denylist'
  emailDomains: string[]
}

interface SecuritySettings {
  policy: AuthPolicy
  mailMode: 'smtp' | 'log' | 'off'
  mailTls?: 'tls' | 'starttls' | 'none'
}

const settings = ref<SecuritySettings>()
const domainText = ref('')
const saved = ref('')
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const message = ref('')
const mailModes = { smtp: 'SMTP', log: '本地日志', off: '已关闭' }
const mailTransports = { tls: '隐式 TLS', starttls: 'STARTTLS', none: '本机中继' }
const payload = computed(() => settings.value && {
  ...settings.value.policy,
  emailDomains: domainText.value.split(/[\n,，]+/).map(domain => domain.trim()).filter(Boolean),
})
const dirty = computed(() => !!settings.value && JSON.stringify(payload.value) !== saved.value)

useHead({ title: '注册与安全 · 碧山秋云' })

function apply(data: SecuritySettings) {
  settings.value = data
  domainText.value = data.policy.emailDomains.join('\n')
  saved.value = JSON.stringify(payload.value)
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    apply(await api<SecuritySettings>('/admin/auth-policy'))
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '读取认证设置失败'
  } finally {
    loading.value = false
  }
}

async function save() {
  if (!dirty.value || saving.value) return
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    apply(await api<SecuritySettings>('/admin/auth-policy', { method: 'PUT', json: payload.value }))
    message.value = '已保存，新规则已生效。'
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '保存认证设置失败'
  } finally {
    saving.value = false
  }
}

onMounted(load)
onBeforeRouteLeave(() => !dirty.value || window.confirm('认证设置还有未保存的修改，确定离开吗？'))
</script>

<template>
  <div>
    <form class="settings-form" @submit.prevent="save">
      <header class="admin-page-header">
        <div><h1>注册与安全</h1><p>管理认证限流、注册邮箱和验证码投递。</p></div>
        <button v-if="settings" class="button button-primary" type="submit" :disabled="!dirty || saving">
          {{ saving ? '保存中…' : '保存设置' }}
        </button>
      </header>
      <p v-if="error" class="error-message" role="alert">{{ error }}</p>
      <p v-if="message" class="editor-notice" role="status">{{ message }}</p>
      <p v-if="loading" class="search-status"><span class="loading-spinner" />正在读取认证设置…</p>
      <button v-else-if="!settings" type="button" class="button button-quiet" @click="load">重试</button>
      <template v-else>
        <section class="settings-group">
          <header><h2>认证限流</h2><p>登录按 IP 和账号分别计数。注册、发送验证码和重置密码按 IP 各自计数。</p></header>
          <label class="publish-toggle"><input v-model="settings.policy.rateEnabled" type="checkbox"><span>启用认证限流</span></label>
          <div v-if="settings.policy.rateEnabled" class="settings-two-columns">
            <label class="form-field"><span>每个窗口允许的请求次数</span><input v-model.number="settings.policy.rateAttempts" type="number" min="1" max="100" required></label>
            <label class="form-field"><span>时间窗口（分钟）</span><input v-model.number="settings.policy.rateWindowMinutes" type="number" min="1" max="1440" required></label>
          </div>
        </section>
        <section class="settings-group">
          <header><h2>注册邮箱</h2><p>新用户申请注册码和提交注册时都会校验。已有账号可用原邮箱找回密码。</p></header>
          <label class="form-field">
            <span>邮箱域名规则</span>
            <select v-model="settings.policy.emailMode">
              <option value="disabled">允许所有邮箱域名</option>
              <option value="allowlist">只允许列表中的域名</option>
              <option value="denylist">禁止列表中的域名</option>
            </select>
          </label>
          <div v-if="settings.policy.emailMode !== 'disabled'" class="form-field">
            <label for="registration-domains">邮箱域名</label>
            <textarea id="registration-domains" v-model="domainText" rows="5" :required="settings.policy.emailMode === 'allowlist'" placeholder="example.com" aria-describedby="registration-domains-help" />
            <small id="registration-domains-help">每行一个域名。子域名需要单独填写。</small>
          </div>
        </section>
        <section class="settings-group">
          <header><h2>验证码投递</h2><p>发件人、服务器和凭据由 Go 服务的环境变量配置。</p></header>
          <div class="admin-panel-row"><span>发送方式</span><strong>{{ mailModes[settings.mailMode] }}</strong></div>
          <div v-if="settings.mailTls" class="admin-panel-row"><span>连接方式</span><strong>{{ mailTransports[settings.mailTls] }}</strong></div>
          <div class="admin-panel-row"><span>每邮箱发送限制</span><strong>同一用途间隔 60 秒，每小时共 5 次</strong></div>
          <div class="admin-panel-row"><span>验证码有效期</span><strong>10 分钟</strong></div>
        </section>
      </template>
    </form>
  </div>
</template>
