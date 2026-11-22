<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useHead } from '@unhead/vue'
import AppIcon from '@/components/AppIcon.vue'
import { categoryName, coursePath, getCourses, notePath } from '@/lib/content'
import { siteSettings } from '@/lib/siteSettings'
import '@/styles/library.css'

const route = useRoute()
const router = useRouter()
const courses = getCourses()
const group = computed(() => courses.find(item => item.courseSlug === route.params.courseSlug))
const siblings = computed(() => courses.filter(item => item.category === group.value?.category))

function switchCourse(event: Event) {
  const slug = (event.target as HTMLSelectElement).value
  if (slug) void router.push(coursePath({ courseSlug: slug }))
}

useHead({ title: () => `${group.value?.course || '课程不存在'} · ${siteSettings.brand.name}` })
</script>

<template>
  <div v-if="group" class="page-shell course-page">
    <nav class="breadcrumbs" aria-label="面包屑">
      <RouterLink to="/library">{{ siteSettings.library.title }}</RouterLink>
      <AppIcon name="chevronRight" :size="13" />
      <RouterLink :to="`/library/${group.category}`">{{ categoryName(group.category) }}</RouterLink>
      <AppIcon name="chevronRight" :size="13" />
      <strong aria-current="page">{{ group.course }}</strong>
    </nav>

    <header class="course-cover">
      <div>
        <p v-if="siteSettings.library.eyebrow" class="eyebrow">{{ siteSettings.library.eyebrow }}</p>
        <h1>{{ group.course }}</h1>
        <p>{{ group.notes.length }} 篇笔记</p>
      </div>
    </header>

    <div class="course-layout">
      <nav class="course-switcher" aria-label="同学科课程">
        <h2>同学科课程</h2>
        <RouterLink v-for="sibling in siblings" :key="sibling.courseSlug" :to="coursePath(sibling)">
          <span>{{ sibling.course }}</span><small>{{ sibling.notes.length }}</small>
        </RouterLink>
      </nav>
      <label class="mobile-course-select">
        <span>同学科课程</span>
        <select :value="group.courseSlug" @change="switchCourse">
          <option v-for="sibling in siblings" :key="sibling.courseSlug" :value="sibling.courseSlug">{{ sibling.course }}（{{ sibling.notes.length }} 篇）</option>
        </select>
      </label>

      <section class="course-content" aria-labelledby="course-index">
        <h2 id="course-index">课程目录</h2>
        <ol class="course-full-list">
          <li v-for="(note, index) in group.notes" :key="note.id">
            <RouterLink :to="notePath(note)">
              <span class="note-order" aria-hidden="true">{{ String(index + 1).padStart(2, '0') }}</span>
              <div>
                <strong>{{ note.title }}</strong>
                <p v-if="note.excerpt">{{ note.excerpt }}</p>
                <small><AppIcon name="clock" :size="14" />约 {{ note.readingMinutes }} 分钟</small>
              </div>
              <AppIcon name="arrowRight" :size="19" />
            </RouterLink>
          </li>
        </ol>
      </section>
    </div>
  </div>
  <div v-else class="empty-state page-shell">
    <h1>课程不存在</h1>
    <RouterLink to="/library" class="button button-primary">返回笔记库</RouterLink>
  </div>
</template>
