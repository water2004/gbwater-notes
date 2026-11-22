import { onBeforeUnmount, ref } from 'vue'
import { sendEmailCode } from '@/lib/account'
import { ApiError } from '@/lib/api'

// useEmailCode 统一处理"发送验证码 → 60 秒倒计时"的交互。
export function useEmailCode(purpose: 'register' | 'reset') {
  const sending = ref(false)
  const cooldown = ref(0)
  const error = ref('')
  const message = ref('')
  let timer: ReturnType<typeof setInterval> | undefined

  function startCountdown(seconds: number) {
    const deadline = Date.now() + seconds * 1000
    cooldown.value = seconds
    clearInterval(timer)
    timer = setInterval(() => {
      cooldown.value = Math.max(0, Math.ceil((deadline - Date.now()) / 1000))
      if (cooldown.value <= 0) clearInterval(timer)
    }, 1000)
  }

  async function send(email: string) {
    error.value = ''
    message.value = ''
    if (!email.trim()) {
      error.value = '请先填写邮箱'
      return
    }
    sending.value = true
    try {
      const data = await sendEmailCode(email.trim(), purpose)
      message.value = purpose === 'reset' ? '如果该邮箱已注册，你将收到验证码。' : '验证码已发送，请检查邮箱（含垃圾邮件）'
      startCountdown(data.cooldownSeconds)
    } catch (reason) {
      error.value = reason instanceof Error ? reason.message : '发送失败'
      if (reason instanceof ApiError && reason.status === 429 && reason.retryAfter > 0) {
        startCountdown(reason.retryAfter)
      }
    } finally {
      sending.value = false
    }
  }

  onBeforeUnmount(() => clearInterval(timer))
  return { sending, cooldown, error, message, send }
}
