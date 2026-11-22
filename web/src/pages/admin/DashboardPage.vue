<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useHead } from '@unhead/vue'
import AppIcon from '@/components/AppIcon.vue'
import { api } from '@/lib/api'
import { buildStateLabel, useAdminPublish } from '@/lib/adminPublish'
import { adminEditorPath, type AdminNote } from '@/lib/adminNotes'
import type { PopularNote } from '@/lib/analytics'
import { notePath } from '@/lib/content'

interface CourseViews { category: string; categoryTitle: string; course: string; courseSlug: string; views: number }
interface DailyViews { date: string; views: number }
interface ReadingOverview { totalViews: number; recentViews: number; activeNotes: number; popularNotes: PopularNote[]; courses: CourseViews[]; daily: DailyViews[] }
interface Overview {
  notes: number
  courses: number
  published: number
  drafts: number
  users: number
  recentNotes: AdminNote[]
  reading: ReadingOverview
}

const overview = ref<Overview>()
const { status } = useAdminPublish()
const requests = new AbortController()
const loading = ref(true)
const error = ref('')
useHead({ title: '工作台 · 碧山秋云' })

const recentNotes = computed(() => overview.value?.recentNotes ?? [])

const popularReadingNotes = computed(() => overview.value?.reading.popularNotes.filter(note => note.views > 0) || [])
const dailySeries = computed(() => {
  const values = new Map((overview.value?.reading.daily || []).map(day => [day.date.slice(0, 10), day.views]))
  const today = new Date()
  return Array.from({ length: 14 }, (_, index) => {
    const date = new Date(Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), today.getUTCDate() - 13 + index))
    const key = date.toISOString().slice(0, 10)
    return { key, views: values.get(key) || 0 }
  })
})
const maxDaily = computed(() => Math.max(1, ...dailySeries.value.map(day => day.views)))
const maxCourseViews = computed(() => Math.max(1, ...(overview.value?.reading.courses || []).map(course => course.views)))

async function load() {
  loading.value = true
  error.value = ''
  try {
    overview.value = await api<Overview>('/api/admin/overview', { signal: requests.signal })
  } catch (reason) {
    if (!requests.signal.aborted) error.value = reason instanceof Error ? reason.message : '读取工作台失败'
  } finally {
    loading.value = false
  }
}
function formatDate(value: string) { return new Date(value).toLocaleString('zh-CN', { hour12: false }) }
function formatNumber(value: number) { return new Intl.NumberFormat('zh-CN').format(value) }
function shortDay(key: string) { const date = new Date(`${key}T00:00:00Z`); return `${date.getUTCMonth() + 1}/${date.getUTCDate()}` }
onMounted(load)
onBeforeUnmount(() => requests.abort())
</script>

<template>
  <div class="workbench-home">
    <header class="admin-page-header">
      <div><h1>工作台</h1><p>接着写笔记，或检查发布状态。</p></div>
      <RouterLink to="/admin/editor" class="button button-primary"><AppIcon name="plus" :size="17" />新建笔记</RouterLink>
    </header>
    <p v-if="error" class="error-message" role="alert">{{ error }} <button type="button" class="text-link" @click="load">重新读取</button></p>
    <div class="workbench-start">
      <section class="admin-panel workbench-recent">
        <div class="panel-heading"><h2>继续编辑</h2><RouterLink to="/admin/notes" class="text-link">全部笔记 <AppIcon name="arrowRight" :size="14" /></RouterLink></div>
        <p v-if="loading" class="form-hint">正在读取最近笔记…</p>
        <p v-else-if="!recentNotes.length && !error" class="form-hint">保存一篇笔记后，就可以在这里继续编辑。</p>
        <RouterLink v-for="note in recentNotes" :key="note.id" :to="adminEditorPath(note.id)" class="revision-row">
          <span><strong>{{ note.title }}</strong><small>{{ note.categoryTitle || note.category }} / {{ note.course }}{{ note.draft ? ' · 草稿' : '' }}</small></span>
          <span class="revision-action">{{ formatDate(note.modified) }} <AppIcon name="chevronRight" :size="15" /></span>
        </RouterLink>
      </section>
      <div class="workbench-shortcuts">
        <section class="admin-panel workbench-counts">
          <h2>笔记</h2>
          <RouterLink to="/admin/notes"><span>全部</span><strong>{{ overview?.notes ?? '—' }}</strong></RouterLink>
          <RouterLink :to="{ path: '/admin/notes', query: { status: 'draft' } }"><span>草稿</span><strong>{{ overview?.drafts ?? '—' }}</strong></RouterLink>
          <RouterLink :to="{ path: '/admin/notes', query: { status: 'published' } }"><span>设为公开</span><strong>{{ overview?.published ?? '—' }}</strong></RouterLink>
        </section>
        <RouterLink to="/admin/publish" class="workbench-publish-link">
          <AppIcon name="build" :size="18" />
          <span><strong>发布</strong><small>{{ status ? buildStateLabel[status.state] : '查看构建状态' }}</small></span>
          <AppIcon name="arrowRight" :size="16" />
        </RouterLink>
      </div>
    </div>
    <details class="workbench-reading">
      <summary><AppIcon name="chart" :size="16" />阅读统计</summary>
    <section class="admin-panel reading-summary">
      <div class="panel-heading"><h2>阅读概览</h2></div>
      <div class="reading-metrics">
        <div><span>累计阅读</span><strong>{{ formatNumber(overview?.reading.totalViews || 0) }}</strong></div>
        <div><span>近 7 天阅读</span><strong>{{ formatNumber(overview?.reading.recentViews || 0) }}</strong></div>
        <div><span>产生阅读的笔记</span><strong>{{ formatNumber(overview?.reading.activeNotes || 0) }}</strong></div>
      </div>
    </section>

    <div class="reading-grid">
      <section class="admin-panel">
        <div class="panel-heading"><h2>近 14 天阅读趋势</h2><small>次</small></div>
        <div v-if="overview?.reading.totalViews" class="reading-chart" aria-label="近 14 天每日阅读量柱状图">
          <div v-for="day in dailySeries" :key="day.key" class="reading-bar-column" :title="`${shortDay(day.key)}：${day.views} 次阅读`">
            <span class="reading-bar-value">{{ day.views || '' }}</span>
            <div class="reading-bar-track"><span :style="{ height: `${day.views / maxDaily * 100}%` }" /></div>
            <small>{{ shortDay(day.key) }}</small>
          </div>
        </div>
        <p v-else class="editor-message">公开笔记产生访问后，这里会显示每日阅读趋势。</p>
      </section>
      <section class="admin-panel">
        <div class="panel-heading"><h2>课程阅读分布</h2><small>累计阅读</small></div>
        <div v-if="overview?.reading.courses.length" class="course-view-list">
          <div v-for="course in overview.reading.courses" :key="course.courseSlug" class="course-view-row">
            <div><strong>{{ course.course }}</strong><span>{{ formatNumber(course.views) }}</span></div>
            <div class="course-view-track"><span :style="{ width: `${course.views / maxCourseViews * 100}%` }" /></div>
          </div>
        </div>
        <p v-else class="editor-message">还没有课程阅读数据。</p>
      </section>
    </div>

    <section class="admin-panel popular-admin-panel">
      <div class="panel-heading"><h2>热门笔记</h2><small>累计阅读 / 近 30 天</small></div>
      <div v-if="popularReadingNotes.length" class="popular-admin-list">
        <div class="popular-admin-header"><span>笔记</span><span>课程</span><span>阅读量</span></div>
        <RouterLink v-for="(note, index) in popularReadingNotes" :key="note.id" :to="notePath(note)" target="_blank" class="popular-admin-row">
          <span><small>{{ String(index + 1).padStart(2, '0') }}</small><strong>{{ note.title }}</strong></span>
          <span>{{ note.course }}</span>
          <span><strong>{{ formatNumber(note.views) }}</strong><small>近 30 天 {{ formatNumber(note.recentViews) }}</small><AppIcon name="external" :size="13" /></span>
        </RouterLink>
      </div>
      <p v-else class="editor-message">还没有热门笔记。公开页访问量会按日期汇总后显示在这里。</p>
    </section>

    </details>
  </div>
</template>
