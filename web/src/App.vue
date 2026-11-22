<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import SiteHeader from '@/components/SiteHeader.vue'
import SiteFooter from '@/components/SiteFooter.vue'
import AdminShell from '@/components/AdminShell.vue'
import { catalogUnavailable } from '@/lib/content'
import { readingSettings, updateReadingSettings } from '@/lib/readingSettings'

const route = useRoute()
const admin = computed(() => Boolean(route.meta.admin))

// 阅读页拦截 Ctrl+滚轮：不缩放整个页面，只调整正文字号。
function onWheel(event: WheelEvent) {
  if (!event.ctrlKey || !document.querySelector('.note-page')) return
  event.preventDefault()
  const step = event.deltaY > 0 ? -0.05 : 0.05
  const next = Math.round((readingSettings.fontScale + step) * 100) / 100
  updateReadingSettings({ fontScale: Math.min(1.35, Math.max(0.85, next)) })
}

onMounted(() => window.addEventListener('wheel', onWheel, { passive: false }))
onBeforeUnmount(() => window.removeEventListener('wheel', onWheel))

function reloadPage() {
  window.location.reload()
}
</script>

<template>
  <a class="skip-link" href="#main-content">跳到正文</a>
  <AdminShell v-if="admin"><RouterView /></AdminShell>
  <template v-else>
    <SiteHeader />
    <main id="main-content">
      <div v-if="route.meta.requiresCatalog && catalogUnavailable" class="empty-state page-shell">
        <h1>笔记目录暂时无法加载</h1>
        <button class="button button-quiet" @click="reloadPage">重新加载</button>
      </div>
      <RouterView v-else />
    </main>
    <SiteFooter />
  </template>
</template>
