import { onMounted, ref } from 'vue'

const isDark = ref(false)
let initialized = false

export function useTheme() {
  onMounted(() => {
    if (initialized) return
    initialized = true
    isDark.value = document.documentElement.classList.contains('dark')
  })

  function toggleTheme() {
    isDark.value = !isDark.value
    document.documentElement.classList.toggle('dark', isDark.value)
    localStorage.setItem('gbwater-theme', isDark.value ? 'dark' : 'light')
    document.querySelector('meta[name="theme-color"]')?.setAttribute('content', isDark.value ? '#17231f' : '#f5f3ed')
  }

  return { isDark, toggleTheme }
}
