<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { useHead } from '@unhead/vue'
import AppIcon from '@/components/AppIcon.vue'
import { api } from '@/lib/api'
import { defaultSiteSettings, normalizeSiteSettings, type PublicSiteSettings } from '@/lib/siteSettings'
import { validCategoryID } from '@/lib/categories'

const tabs = [
  { id: 'brand', label: '站点外观' },
  { id: 'home', label: '首页' },
  { id: 'library', label: '笔记库' },
  { id: 'categories', label: '学科' },
  { id: 'about', label: '关于页' },
] as const
const activeTab = ref<typeof tabs[number]['id']>('brand')
const settings = ref<PublicSiteSettings>(defaultSiteSettings())
const savedSnapshot = ref('')
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const message = ref('')
const newID = ref('')
const newName = ref('')
const categoryError = ref('')
function addCategory() {
  const id = newID.value.trim()
  const name = newName.value.trim()
  if (!validCategoryID(id)) { categoryError.value = '请使用小写字母或数字，词间可用连字符，并避开系统保留名。'; return }
  if (Object.hasOwn(settings.value.categories, id)) { categoryError.value = '这个网址标识已经存在。'; return }
  if (!name) { categoryError.value = '请输入学科名称。'; return }
  settings.value.categories[id] = { name, description: '' }
  newID.value = ''; newName.value = ''; categoryError.value = ''; message.value = ''
}
const dirty = computed(() => savedSnapshot.value !== '' && JSON.stringify(settings.value) !== savedSnapshot.value)
useHead({ title: '站点设置 · 碧山秋云' })

async function load() {
  loading.value = true
  error.value = ''
  try {
    const stored = await api<unknown>('/admin/site-settings')
    settings.value = normalizeSiteSettings(stored)
    savedSnapshot.value = JSON.stringify(settings.value)
  } catch (reason) { error.value = reason instanceof Error ? reason.message : '读取站点设置失败' } finally { loading.value = false }
}
function validate() {
  if (Object.values(settings.value.categories).some(entry => !entry.name.trim())) {
    error.value = '请填写每个学科的显示名称。'
    activeTab.value = 'categories'
    return false
  }
  const { brand, home, library, about } = settings.value
  const required = [
    { tab: 'brand', value: brand.name, label: '站点名称' },
    { tab: 'home', value: home.title, label: '首页主标题' },
    { tab: 'home', value: home.primaryAction, label: '首页主按钮文案' },
    { tab: 'library', value: library.title, label: '笔记库标题' },
    { tab: 'about', value: about.title, label: '关于页标题' },
  ] as const
  const missing = required.find(field => !field.value.trim())
  if (missing) {
    activeTab.value = missing.tab
    error.value = `请填写${missing.label}。`
    return false
  }
  if (!settings.value.about.sections.length || settings.value.about.sections.some(section => !section.title.trim() || !section.body.trim())) {
    error.value = '关于页至少需要一个章节，且每个章节的标题和正文都不能为空。'
    activeTab.value = 'about'
    return false
  }
  return true
}
async function save() {
  if (!dirty.value || saving.value) return
  error.value = ''; message.value = ''
  if (!validate()) return
  saving.value = true
  try {
    const saved = await api<unknown>('/admin/site-settings', { method: 'PUT', json: settings.value })
    settings.value = normalizeSiteSettings(saved)
    savedSnapshot.value = JSON.stringify(settings.value)
    message.value = '设置已保存。公开静态页面会在下次构建后使用新文案。'
  } catch (reason) { error.value = reason instanceof Error ? reason.message : '保存设置失败' } finally { saving.value = false }
}
function undoChanges() {
  if (!savedSnapshot.value) return
  settings.value = normalizeSiteSettings(JSON.parse(savedSnapshot.value))
  error.value = ''; message.value = ''
}
function addSection() { settings.value.about.sections.push({ title: '新章节', body: '' }); message.value = '' }
function removeSection(index: number) { if (settings.value.about.sections.length > 1) settings.value.about.sections.splice(index, 1) }
function moveSection(index: number, direction: number) {
  const target = index + direction
  if (target < 0 || target >= settings.value.about.sections.length) return
  const sections = settings.value.about.sections
  ;[sections[index], sections[target]] = [sections[target], sections[index]]
}
onMounted(load)
onBeforeRouteLeave(() => !dirty.value || window.confirm('站点设置还有未保存的修改，确定离开吗？'))
</script>

<template>
  <div class="site-settings-page">
    <header class="admin-page-header">
      <div><h1>站点设置</h1><p>编辑公开站点的文案、导航和学科。</p></div>
    </header>
    <p v-if="loading" class="search-status" role="status"><span class="loading-spinner" />正在读取站点设置…</p>
    <p v-else-if="!savedSnapshot" class="error-message" role="alert">{{ error }} <button class="text-link" type="button" @click="load">重试</button></p>

    <div v-else class="site-settings-layout">
      <nav class="site-settings-nav" aria-label="设置分组">
        <button v-for="tab in tabs" :key="tab.id" type="button" :aria-current="activeTab === tab.id ? 'true' : undefined"
          @click="activeTab = tab.id">{{ tab.label }}</button>
      </nav>
      <form class="site-settings-editor" novalidate @submit.prevent="save">
        <fieldset :disabled="saving">
          <section v-if="activeTab === 'brand'">
            <header class="site-settings-heading"><h2>站点外观</h2><p>设置页眉的站名、主导航和页脚信息。</p></header>
            <section class="site-settings-section">
              <h3>站点标识</h3>
              <div class="settings-fields">
                <label class="form-field"><span>站点名称</span><input v-model="settings.brand.name" required></label>
                <label class="form-field"><span>站点标记</span><input v-model="settings.brand.mark" maxlength="2"></label>
                <label class="form-field field-wide"><span>英文副标题</span><input v-model="settings.brand.subtitle"></label>
              </div>
            </section>
            <section class="site-settings-section">
              <h3>主导航</h3>
              <div class="settings-fields">
                <label class="form-field"><span>首页</span><input v-model="settings.navigation.home"></label>
                <label class="form-field"><span>笔记库</span><input v-model="settings.navigation.library"></label>
                <label class="form-field"><span>搜索</span><input v-model="settings.navigation.search"></label>
                <label class="form-field"><span>关于页</span><input v-model="settings.navigation.about"></label>
              </div>
            </section>
            <section class="site-settings-section">
              <h3>页脚</h3>
              <div class="settings-fields">
                <label class="form-field field-wide"><span>页脚短句</span><input v-model="settings.footer.tagline"></label>
                <label class="form-field field-wide"><span>版权信息</span><input v-model="settings.footer.copyright"></label>
                <label class="form-field"><span>ICP 备案文字</span><input v-model="settings.footer.icp"></label>
                <label class="form-field"><span>公安备案文字</span><input v-model="settings.footer.police"></label>
              </div>
            </section>
          </section>

          <section v-else-if="activeTab === 'home'">
            <header class="site-settings-heading"><h2>首页</h2><p>编辑访客打开首页时看到的内容。</p></header>
            <section class="site-settings-section">
              <h3>首屏</h3>
              <div class="settings-fields">
                <label class="form-field field-wide"><span>首页主标题</span><input v-model="settings.home.title" required></label>
                <label class="form-field"><span>副标题</span><input v-model="settings.home.subtitle"></label>
                <label class="form-field"><span>眉题</span><input v-model="settings.home.eyebrow"></label>
                <label class="form-field field-wide"><span>首页说明</span><textarea v-model="settings.home.description" rows="3" /></label>
                <label class="form-field"><span>主按钮文案</span><input v-model="settings.home.primaryAction" required></label>
              </div>
            </section>
            <div class="site-settings-details">
            <details class="site-settings-disclosure">
              <summary>随机笔记卡<AppIcon name="chevronDown" :size="16" /></summary>
              <div class="settings-fields">
                <label class="form-field"><span>卡片标签</span><input v-model="settings.home.randomLabel"></label>
                <label class="form-field"><span>换一篇按钮</span><input v-model="settings.home.shuffleLabel"></label>
                <label class="form-field"><span>阅读按钮</span><input v-model="settings.home.readLabel"></label>
                <label class="form-field"><span>阅读时长格式</span><input v-model="settings.home.minutesTemplate"><small>使用 {minutes} 表示分钟数</small></label>
                <label class="form-field"><span>访问量格式</span><input v-model="settings.home.viewsTemplate"><small>使用 {views} 表示访问量</small></label>
              </div>
            </details>
            <details class="site-settings-disclosure">
              <summary>继续阅读<AppIcon name="chevronDown" :size="16" /></summary>
              <div class="settings-fields">
                <label class="form-field"><span>眉题</span><input v-model="settings.home.recentEyebrow"></label>
                <label class="form-field"><span>区块标题</span><input v-model="settings.home.recentTitle"></label>
                <label class="form-field"><span>清空最近阅读按钮</span><input v-model="settings.home.clearRecent"></label>
              </div>
            </details>
            <details class="site-settings-disclosure">
              <summary>热门笔记<AppIcon name="chevronDown" :size="16" /></summary>
              <div class="settings-fields">
                <label class="form-field"><span>眉题</span><input v-model="settings.home.popularEyebrow"></label>
                <label class="form-field"><span>区块标题</span><input v-model="settings.home.popularTitle"></label>
                <label class="form-field field-wide"><span>区块说明</span><input v-model="settings.home.popularDescription"></label>
              </div>
            </details>
            </div>
          </section>

          <section v-else-if="activeTab === 'library'">
            <header class="site-settings-heading"><h2>笔记库</h2><p>设置课程目录页面的标题、简介和搜索框文案。</p></header>
            <div class="settings-fields">
              <label class="form-field"><span>页面标题</span><input v-model="settings.library.title" required></label>
              <label class="form-field"><span>眉题</span><input v-model="settings.library.eyebrow"></label>
              <label class="form-field field-wide"><span>页面简介</span><textarea v-model="settings.library.description" rows="3" /></label>
              <label class="form-field field-wide"><span>标题筛选占位文字</span><input v-model="settings.library.filterPlaceholder"></label>
            </div>
          </section>

          <section v-else-if="activeTab === 'categories'">
            <header class="site-settings-heading"><h2>学科</h2><p>保存后可在编辑器选择新学科。构建后更新公开笔记库。</p></header>
            <section class="site-settings-section">
              <h3>添加学科</h3>
              <div class="settings-fields">
                <label class="form-field"><span>学科名称</span><input v-model="newName" placeholder="例如：天文学" @keydown.enter.prevent="addCategory"></label>
                <label class="form-field"><span>网址标识</span><input v-model="newID" placeholder="astronomy" @keydown.enter.prevent="addCategory"></label>
              </div>
              <p v-if="categoryError" class="error-message" role="alert">{{ categoryError }}</p>
              <button type="button" class="button button-quiet" @click="addCategory"><AppIcon name="plus" :size="16" />添加学科</button>
            </section>
            <section class="site-settings-section">
              <h3>已有学科</h3>
              <article v-for="(category, key) in settings.categories" :key="key" class="settings-record">
                <header><h4>{{ category.name || '未命名学科' }}</h4><code>{{ key }}</code></header>
                <div class="settings-fields">
                  <label class="form-field"><span>显示名称</span><input v-model="category.name" required></label>
                  <label class="form-field"><span>学科说明</span><textarea v-model="category.description" rows="2" /></label>
                </div>
              </article>
            </section>
          </section>

          <section v-else>
            <header class="site-settings-heading"><h2>关于页</h2><p>编辑简介、正文各章节和页尾短句。</p></header>
            <div class="settings-fields">
              <label class="form-field"><span>页面标题</span><input v-model="settings.about.title" required></label>
              <label class="form-field"><span>眉题</span><input v-model="settings.about.eyebrow"></label>
              <label class="form-field field-wide"><span>页首简介</span><textarea v-model="settings.about.lead" rows="3" /></label>
            </div>
            <section class="site-settings-section">
              <h3>正文</h3>
              <article v-for="(section, index) in settings.about.sections" :key="index" class="settings-record">
                <header>
                  <h4>章节 {{ index + 1 }}</h4>
                  <div class="settings-record-actions">
                    <button type="button" class="icon-button icon-button-compact" title="上移章节" aria-label="上移章节" :disabled="index === 0" @click="moveSection(index, -1)"><AppIcon name="arrowUp" :size="16" /></button>
                    <button type="button" class="icon-button icon-button-compact" title="下移章节" aria-label="下移章节" :disabled="index === settings.about.sections.length - 1" @click="moveSection(index, 1)"><AppIcon name="arrowUp" :size="16" class="rotated" /></button>
                    <button type="button" class="button button-danger button-compact" :disabled="settings.about.sections.length === 1" @click="removeSection(index)">删除</button>
                  </div>
                </header>
                <div class="settings-fields">
                  <label class="form-field field-wide"><span>章节标题</span><input v-model="section.title" required></label>
                  <label class="form-field field-wide"><span>章节正文</span><textarea v-model="section.body" rows="4" required /></label>
                </div>
              </article>
              <button type="button" class="button button-quiet" @click="addSection"><AppIcon name="plus" :size="16" />添加章节</button>
            </section>
            <section class="site-settings-section">
              <h3>页尾</h3>
              <div class="settings-fields">
                <label class="form-field field-wide"><span>页尾短句</span><input v-model="settings.about.colophon"></label>
              </div>
            </section>
          </section>
        </fieldset>

        <footer class="site-settings-footer">
          <p v-if="error" class="settings-save-error" role="alert">{{ error }}</p>
          <p v-else-if="message" role="status">{{ message }} <RouterLink to="/admin/publish" class="text-link">前往发布<AppIcon name="arrowRight" :size="15" /></RouterLink></p>
          <p v-else aria-live="polite">{{ dirty ? '有未保存的修改' : '保存后，在发布页构建更新。' }}</p>
          <div>
            <button class="button button-quiet" type="button" :disabled="!dirty || saving" @click="undoChanges">撤销修改</button>
            <button class="button button-primary" type="submit" :disabled="!dirty || saving"><AppIcon name="check" :size="16" />{{ saving ? '保存中…' : '保存设置' }}</button>
          </div>
        </footer>
      </form>
    </div>
  </div>
</template>
