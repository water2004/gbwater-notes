import { reactive } from 'vue'
import { readCategories, type Categories } from './categories'

export interface AboutSectionSettings { title: string; body: string }
export interface PublicSiteSettings {
  favicon: string
  brand: { mark: string; name: string; subtitle: string }
  navigation: { home: string; library: string; search: string; about: string }
  footer: { tagline: string; copyright: string; icp: string; police: string }
  home: {
    eyebrow: string; title: string; subtitle: string; description: string; primaryAction: string
    randomLabel: string; shuffleLabel: string; readLabel: string; minutesTemplate: string; viewsTemplate: string
    recentEyebrow: string; recentTitle: string; clearRecent: string
    popularEyebrow: string; popularTitle: string; popularDescription: string
  }
  library: {
    eyebrow: string; title: string; description: string; filterPlaceholder: string
  }
  categories: Categories
  about: { eyebrow: string; title: string; lead: string; sections: AboutSectionSettings[]; colophon: string }
}

const defaults: PublicSiteSettings = {
  favicon: '',
  brand: { mark: '秋', name: '碧山秋云', subtitle: 'GBWATER NOTES' },
  navigation: { home: '首页', library: '笔记库', search: '搜索', about: '关于' },
  footer: {
    tagline: '把推导写清楚，把知识留长久。', copyright: '© 2026 GBwater · 碧山秋云',
    icp: '粤ICP备2022125598号', police: '粤公网安备44060602002381号',
  },
  home: {
    eyebrow: 'GBWATER 的课程笔记', title: '乱云难锁碧山秋', subtitle: '数学、物理与计算机笔记',
    description: '按课程组织的公开笔记，保留完整公式、图形与推导上下文。', primaryAction: '浏览课程目录',
    randomLabel: '随机笔记', shuffleLabel: '换一篇', readLabel: '阅读这篇', minutesTemplate: '{minutes} 分钟阅读', viewsTemplate: '{views} 次阅读',
    recentEyebrow: 'CONTINUE READING', recentTitle: '继续上次阅读', clearRecent: '清空记录',
    popularEyebrow: 'MOST READ', popularTitle: '热门笔记', popularDescription: '按公开页面的累计访问量排序。',
  },
  library: {
    eyebrow: '', title: '笔记库', description: '',
    filterPlaceholder: '搜索本学科的课程或笔记',
  },
  categories: readCategories(undefined),
  about: {
    eyebrow: 'ABOUT', title: '关于碧山秋云',
    lead: '这里保存 GBwater 的数学、物理与计算机学习笔记。内容按课程组织，适合顺序学习，也适合在需要时回头查阅。',
    sections: [
      { title: '内容如何组织', body: '每篇笔记都保留在原来的课程脉络中。课程目录给出阅读顺序，文内大纲和前后篇导航用于快速定位；标题筛选与全文搜索则用于查找具体概念。' },
      { title: '阅读记录保存在哪里', body: '已读标记、最近阅读、字号和主题偏好只保存在当前浏览器中，不需要注册账户。公开页访问量只按笔记和日期聚合，不保存访客 IP 或逐条访问日志。' },
      { title: '公开与修订', body: '这里只公开展示的笔记，不公开草稿、实验报告或其他私人资料。内容会随学习和整理持续修订，公开页面在构建后更新。' },
    ],
    colophon: '把推导写清楚，把知识留长久。',
  },
}

function cloneDefaults() { return JSON.parse(JSON.stringify(defaults)) as PublicSiteSettings }
function mergeSettings<T>(base: T, override: unknown): T {
  if (Array.isArray(base)) return (Array.isArray(override) ? override : base) as T
  if (base && typeof base === 'object') {
    const result = { ...base } as Record<string, unknown>
    if (override && typeof override === 'object' && !Array.isArray(override)) {
      for (const [key, value] of Object.entries(override)) {
        if (Object.hasOwn(result, key)) result[key] = mergeSettings(result[key], value)
      }
    }
    return result as T
  }
  return (override === undefined || override === null ? base : override) as T
}

export function normalizeSiteSettings(value: unknown) {
  const settings = mergeSettings(cloneDefaults(), value)
  if (typeof settings.favicon !== 'string' || !settings.favicon.startsWith('data:image/png;base64,')) settings.favicon = ''
  settings.categories = readCategories(value && typeof value === 'object' && 'categories' in value ? value.categories : undefined)
  return settings
}
const generatedSettings = import.meta.glob('../generated/site-settings.json', { eager: true, import: 'default' })
export const siteSettings = reactive(normalizeSiteSettings(generatedSettings['../generated/site-settings.json']))
export function defaultSiteSettings() { return cloneDefaults() }
export function formatMinutes(template: string, minutes: number) { return template.replace('{minutes}', String(minutes)) }
