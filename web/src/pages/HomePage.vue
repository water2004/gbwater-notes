<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useHead } from '@unhead/vue'
import AppIcon from '@/components/AppIcon.vue'
import { getPopularNotes, type PopularNote } from '@/lib/analytics'
import { categoryName, notePath, publishedNotes } from '@/lib/content'
import { useRecentNotes } from '@/lib/recent'
import { formatMinutes, siteSettings } from '@/lib/siteSettings'

useHead({ title: () => `${siteSettings.brand.name} · ${siteSettings.home.subtitle}`, meta: [{ name: 'description', content: () => siteSettings.home.description }] })
const recent = useRecentNotes()
const popular = ref<PopularNote[]>([])
const notesByID = new Map(publishedNotes.map(note => [note.id, note]))
const featuredNote = ref(publishedNotes[0])
const shuffling = ref(false)
const recentNotes = computed(recent.notes)
const popularNotes = computed(() => {
  const recentIds = new Set(recentNotes.value.map(note => note.id))
  return popular.value.flatMap(item => {
    const note = notesByID.get(item.id)
    return note && item.views > 0 && !recentIds.has(item.id) ? [{ ...note, views: item.views }] : []
  }).slice(0, 6)
})

function drawNote() {
  if (shuffling.value || publishedNotes.length < 2) return
  const candidates = publishedNotes.filter(note => note.id !== featuredNote.value.id)
  featuredNote.value = candidates[Math.floor(Math.random() * candidates.length)]
  shuffling.value = true
}
function formatNumber(value: number) { return new Intl.NumberFormat('zh-CN').format(value) }

onMounted(() => {
  recent.loadRecent()
  drawNote()
  getPopularNotes(10).then(notes => { popular.value = notes }).catch(() => { /* Static reading remains available without analytics. */ })
})
</script>

<template>
  <div class="home-page">
    <section class="hero">
      <div class="hero-ornament" aria-hidden="true">∑</div>
      <div class="hero-content">
        <p class="eyebrow">{{ siteSettings.home.eyebrow }}</p>
        <h1>{{ siteSettings.home.title }}<span>{{ siteSettings.home.subtitle }}</span></h1>
        <p class="hero-description">{{ siteSettings.home.description }}</p>
        <div class="hero-actions">
          <RouterLink to="/library" class="button button-primary">{{ siteSettings.home.primaryAction }}<AppIcon name="arrowRight" :size="17" /></RouterLink>
        </div>
      </div>
      <div v-if="featuredNote" class="hero-card-stage" :class="{ shuffling }">
      <article class="hero-note-card" :class="{ shuffling }" :aria-busy="shuffling">
        <div class="hero-note-top"><span class="hero-note-label"><AppIcon name="book" :size="17" />{{ siteSettings.home.randomLabel }}</span><button class="shuffle-button" :disabled="shuffling" aria-label="随机换一篇笔记" @click="drawNote"><AppIcon name="shuffle" :size="16" /><span>{{ siteSettings.home.shuffleLabel }}</span></button></div>
        <Transition name="note-shuffle" mode="out-in" @after-enter="shuffling = false">
          <div :key="featuredNote.id" class="hero-note-content" aria-live="polite">
            <span class="hero-note-category">{{ categoryName(featuredNote.category, featuredNote.categoryTitle) }} · {{ featuredNote.course }}</span>
            <RouterLink :to="notePath(featuredNote)" class="hero-note-title"><strong>{{ featuredNote.title }}</strong></RouterLink>
            <span v-if="featuredNote.excerpt" class="hero-note-excerpt">{{ featuredNote.excerpt }}</span>
            <div class="hero-note-meta"><span>{{ formatMinutes(siteSettings.home.minutesTemplate, featuredNote.readingMinutes) }}</span><RouterLink :to="notePath(featuredNote)">{{ siteSettings.home.readLabel }}<AppIcon name="arrowRight" :size="17" /></RouterLink></div>
          </div>
        </Transition>
        <span class="hero-formula" aria-hidden="true">{{ categoryName(featuredNote.category, featuredNote.categoryTitle).slice(0, 1) }}</span>
      </article>
      </div>
    </section>

    <section v-if="recentNotes.length" class="home-section recent-section">
      <div class="section-heading"><div><p class="eyebrow">{{ siteSettings.home.recentEyebrow }}</p><h2>{{ siteSettings.home.recentTitle }}</h2></div><button class="text-link" @click="recent.clearRecent">{{ siteSettings.home.clearRecent }}</button></div>
      <div class="recent-note-list"><RouterLink v-for="note in recentNotes" :key="note.id" :to="notePath(note)"><span>{{ note.course }}</span><strong>{{ note.title }}</strong><small>{{ formatMinutes(siteSettings.home.minutesTemplate, note.readingMinutes) }}<AppIcon name="arrowRight" :size="16" /></small></RouterLink></div>
    </section>

    <section v-if="popularNotes.length" class="home-section popular-section">
      <div class="section-heading"><div><p class="eyebrow">{{ siteSettings.home.popularEyebrow }}</p><h2>{{ siteSettings.home.popularTitle }}</h2></div><p>{{ siteSettings.home.popularDescription }}</p></div>
      <div class="popular-note-list">
        <RouterLink v-for="(item, index) in popularNotes" :key="item.id" :to="notePath(item)" class="popular-note">
          <span class="popular-rank">{{ String(index + 1).padStart(2, '0') }}</span>
          <span class="popular-note-title"><small>{{ categoryName(item.category, item.categoryTitle) }} · {{ item.course }}</small><strong>{{ item.title }}</strong></span>
          <span class="popular-note-meta">{{ siteSettings.home.viewsTemplate.replace('{views}', formatNumber(item.views)) }}<AppIcon name="arrowRight" :size="16" /></span>
        </RouterLink>
      </div>
    </section>
  </div>
</template>
