<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { siteSettings } from '@/lib/siteSettings'
useHead({ title: () => `${siteSettings.about.title} · ${siteSettings.brand.name}` })
</script>

<template>
  <div class="page-shell about-page">
    <header class="about-header">
      <p class="eyebrow">{{ siteSettings.about.eyebrow }}</p>
      <h1>{{ siteSettings.about.title }}</h1>
      <p class="about-lead">{{ siteSettings.about.lead }}</p>
    </header>
    <main class="about-content">
      <section v-for="(section, index) in siteSettings.about.sections" :key="index">
        <span class="about-section-number">{{ String(index + 1).padStart(2, '0') }}</span>
        <div><h2>{{ section.title }}</h2><p>{{ section.body }}</p></div>
      </section>
    </main>
    <footer class="about-colophon"><span>{{ siteSettings.brand.subtitle }}</span><p>{{ siteSettings.about.colophon }}</p></footer>
  </div>
</template>
