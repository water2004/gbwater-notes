<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useHead } from '@unhead/vue'
import AppIcon from '@/components/AppIcon.vue'
import CourseCard from '@/components/CourseCard.vue'
import CourseGrid from '@/components/CourseGrid.vue'
import { categories, getCourses } from '@/lib/content'
import { siteSettings } from '@/lib/siteSettings'
import '@/styles/library.css'

const route = useRoute()
const router = useRouter()
const courses = getCourses()
const subjects = Object.entries(categories).map(([id, config]) => {
  const groups = courses.filter(group => group.category === id)
  return { id, ...config, courses: groups, total: groups.reduce((n, group) => n + group.notes.length, 0) }
})
const subject = computed(() => route.params.category
  ? subjects.find(item => item.id === route.params.category)
  : subjects.find(item => item.courses.length) || subjects[0])
const query = ref('')
const term = computed(() => query.value.trim().toLocaleLowerCase('zh-CN'))
const expanded = ref('')
const searchInput = ref<HTMLInputElement>()
const visible = computed(() => (subject.value?.courses || []).flatMap(group => {
  const notes = group.course.toLocaleLowerCase('zh-CN').includes(term.value)
    ? group.notes : group.notes.filter(note => note.title.toLocaleLowerCase('zh-CN').includes(term.value))
  return notes.length ? [{ group, notes }] : []
}))
const tiles = computed(() => {
  const selected = visible.value.find(tile => tile.group.courseSlug === expanded.value)
  return selected ? [selected, ...visible.value.filter(tile => tile !== selected)] : visible.value
})
let returnViewportTop = 0

watch(() => subject.value?.id, () => { query.value = ''; expanded.value = '' })
watch(term, () => { expanded.value = '' })
async function toggleCourse(slug: string) {
  const card = document.getElementById('card-' + slug)
  if (!card) return
  const opening = expanded.value !== slug
  if (opening) returnViewportTop = card.getBoundingClientRect().top
  expanded.value = opening ? slug : ''
  await nextTick()
  card.querySelector<HTMLButtonElement>('.shelf-trigger')?.focus({ preventScroll: true })
  const top = card.parentElement!.getBoundingClientRect().top + card.offsetTop
  const margin = parseFloat(getComputedStyle(card).scrollMarginTop)
  const target = opening ? Math.max(top, margin) : Math.max(margin, Math.min(returnViewportTop, window.innerHeight - 80))
  if (Math.abs(top - target) > 1) window.scrollBy({ top: top - target, behavior: 'instant' })
}
async function clearQuery() {
  query.value = ''
  await nextTick()
  searchInput.value?.focus()
}
async function moveTab(event: KeyboardEvent, index: number) {
  const last = subjects.length - 1
  let next = index
  switch (event.key) {
    case 'ArrowRight': next = index === last ? 0 : index + 1; break
    case 'ArrowLeft': next = index === 0 ? last : index - 1; break
    case 'Home': next = 0; break
    case 'End': next = last; break
    default: return
  }
  event.preventDefault()
  const id = subjects[next].id
  await router.push('/library/' + id)
  document.getElementById('subject-' + id)?.focus()
}
useHead({ title: () => [subject.value?.name, siteSettings.library.title, siteSettings.brand.name].filter(Boolean).join(' · ') })
</script>

<template>
  <div class="page-shell library-shelf">
    <header class="shelf-header">
      <div>
        <p v-if="siteSettings.library.eyebrow" class="eyebrow">{{ siteSettings.library.eyebrow }}</p>
        <h1>{{ siteSettings.library.title }}</h1>
        <p v-if="siteSettings.library.description" class="shelf-description">{{ siteSettings.library.description }}</p>
      </div>
      <div class="shelf-search" role="search">
        <AppIcon name="search" :size="18" />
        <input ref="searchInput" v-model="query" type="search" :placeholder="siteSettings.library.filterPlaceholder" aria-label="在当前学科搜索课程或笔记" @keydown.esc.prevent="clearQuery">
        <button v-if="query" type="button" aria-label="清除搜索" @click="clearQuery"><AppIcon name="close" :size="16" /></button>
      </div>
    </header>
    <div class="shelf-controls">
      <nav class="shelf-tabs" role="tablist" aria-label="学科">
        <RouterLink v-for="(item, index) in subjects" :id="'subject-' + item.id" :key="item.id"
          :to="'/library/' + item.id" role="tab" :aria-selected="subject?.id === item.id"
          :tabindex="subject?.id === item.id || (!subject && !index) ? 0 : -1" aria-controls="subject-panel" @keydown="moveTab($event, index)">
          {{ item.name }}<small>{{ item.courses.length }}</small>
        </RouterLink>
      </nav>
      <p v-if="subject" class="shelf-summary" role="status">{{ term ? visible.length + ' 门匹配课程' : subject.courses.length + ' 门课程 · ' + subject.total + ' 篇笔记' }}</p>
    </div>
    <section id="subject-panel" role="tabpanel" :aria-labelledby="subject ? 'subject-' + subject.id : undefined" @keydown.esc="expanded && toggleCourse(expanded)">
      <CourseGrid v-if="visible.length" :key="subject?.id" :expanded="expanded">
        <CourseCard v-for="tile in tiles" :id="'card-' + tile.group.courseSlug" :key="tile.group.courseSlug"
          :group="tile.group" :notes="tile.notes" :expanded="expanded === tile.group.courseSlug" :searching="Boolean(term)"
          @toggle="toggleCourse(tile.group.courseSlug)" />
      </CourseGrid>
      <div v-else class="shelf-empty">
        <h2>{{ term ? '没有匹配的课程或笔记' : subject ? '这个学科还没有公开笔记' : '学科不存在' }}</h2>
        <button v-if="term" type="button" class="button button-quiet" @click="clearQuery">清除搜索</button>
        <RouterLink v-else-if="!subject" to="/library" class="text-link">返回笔记库</RouterLink>
      </div>
    </section>
  </div>
</template>
