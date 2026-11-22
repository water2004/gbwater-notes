import type { RouteRecordRaw } from 'vue-router'

declare module 'vue-router' {
  interface RouteMeta {
    requiresCatalog?: boolean
    adminSection?: 'home' | 'notes' | 'assets' | 'publish' | 'settings' | 'users' | 'security'
    adminEditor?: boolean
  }
}

export const routes: RouteRecordRaw[] = [
  { path: '/', component: () => import('@/pages/HomePage.vue'), meta: { title: '首页', requiresCatalog: true } },
  { path: '/library/:category?', component: () => import('@/pages/LibraryPage.vue'), meta: { title: '笔记库', requiresCatalog: true } },
  { path: '/courses/:courseSlug', component: () => import('@/pages/CoursePage.vue'), meta: { title: '课程', requiresCatalog: true } },
  { path: '/notes/:slug', component: () => import('@/pages/NotePage.vue'), meta: { title: '阅读笔记', requiresCatalog: true } },
  { path: '/search', component: () => import('@/pages/SearchPage.vue'), meta: { title: '全文搜索' } },
  { path: '/about', component: () => import('@/pages/AboutPage.vue'), meta: { title: '关于' } },
  { path: '/login', component: () => import('@/pages/LoginPage.vue'), meta: { title: '登录' } },
  { path: '/register', component: () => import('@/pages/RegisterPage.vue'), meta: { title: '注册' } },
  { path: '/forgot', component: () => import('@/pages/ForgotPage.vue'), meta: { title: '找回密码' } },
  { path: '/account', component: () => import('@/pages/AccountPage.vue'), meta: { title: '我的账号', requiresCatalog: true } },
  { path: '/admin', component: () => import('@/pages/admin/DashboardPage.vue'), meta: { title: '工作台', admin: true, adminSection: 'home' } },
  { path: '/admin/notes', component: () => import('@/pages/admin/NotesPage.vue'), meta: { title: '笔记', admin: true, adminSection: 'notes' } },
  { path: '/admin/assets', component: () => import('@/pages/admin/AssetsPage.vue'), meta: { title: '图床', admin: true, adminSection: 'assets' } },
  { path: '/admin/publish', component: () => import('@/pages/admin/PublishPage.vue'), meta: { title: '发布', admin: true, adminSection: 'publish' } },
  { path: '/admin/users', component: () => import('@/pages/admin/UsersPage.vue'), meta: { title: '用户管理', admin: true, adminSection: 'users' } },
  { path: '/admin/security', component: () => import('@/pages/admin/SecurityPage.vue'), meta: { title: '注册与安全', admin: true, adminSection: 'security' } },
  { path: '/admin/settings', component: () => import('@/pages/admin/SettingsPage.vue'), meta: { title: '站点设置', admin: true, adminSection: 'settings' } },
  { path: '/admin/editor/:category?/:course?/:title?', component: () => import('@/pages/admin/EditorPage.vue'), meta: { title: '编辑笔记', admin: true, adminSection: 'notes', adminEditor: true } },
  { path: '/:pathMatch(.*)*', component: () => import('@/pages/NotFoundPage.vue'), meta: { title: '页面未找到' } },
]
