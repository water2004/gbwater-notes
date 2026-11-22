<script setup lang="ts">
import { computed } from 'vue'
import AppIcon from './AppIcon.vue'
import { coursePath, notePath, type getCourses } from '@/lib/content'

type Course = ReturnType<typeof getCourses>[number]
const props = defineProps<{ group: Course; notes: Course['notes']; expanded: boolean; searching: boolean }>()
const emit = defineEmits<{ toggle: [] }>()
const previewCount = computed(() => {
  const count = props.notes.length
  const limit = Math.min(5, Math.ceil(Math.sqrt(count)))
  return count - limit === 1 ? count : limit
})
const visible = computed(() => props.expanded || props.searching ? props.notes : props.notes.slice(0, previewCount.value))
const remaining = computed(() => props.notes.length - visible.value.length)
</script>

<template>
  <article class="shelf-card" :class="{ 'is-expanded': expanded }" :data-wide-preview="visible.length >= 4 ? '' : undefined">
    <div class="shelf-card-inner">
      <header class="shelf-card-head">
        <h2>
          <button type="button" class="shelf-trigger" :aria-expanded="expanded"
            :aria-controls="'notes-' + group.courseSlug" :aria-label="(expanded ? '收起' : '展开') + group.course + '目录'" @click="emit('toggle')">
            <span>{{ group.course }}</span><AppIcon :name="expanded ? 'close' : 'chevronDown'" :size="15" />
          </button>
        </h2>
        <span class="shelf-count">{{ group.notes.length }} 篇笔记</span>
        <RouterLink v-if="expanded" class="shelf-course-link" :to="coursePath(group)">打开课程页 <AppIcon name="arrowRight" :size="14" /></RouterLink>
      </header>
      <div class="shelf-card-body">
        <ol :id="'notes-' + group.courseSlug" class="shelf-notes">
          <li v-for="note in visible" :key="note.id">
            <RouterLink :to="notePath(note)" :title="note.title">
              <small aria-hidden="true">{{ String(group.notes.indexOf(note) + 1).padStart(2,'0') }}</small>
              <span>{{ note.title }}</span>
            </RouterLink>
          </li>
        </ol>
        <button v-if="remaining" type="button" class="shelf-more" :aria-controls="'notes-' + group.courseSlug"
          :aria-label="'展开' + group.course + '的其余 ' + remaining + ' 篇笔记'" @click="emit('toggle')">
          <AppIcon name="plus" :size="13" /> 其余 {{ remaining }} 篇
        </button>
      </div>
    </div>
  </article>
</template>
