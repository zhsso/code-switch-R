import { createRouter, createWebHistory } from 'vue-router'

const routes = [
  { path: '/', component: () => import('../components/Main/Index.vue') },
  { path: '/availability', component: () => import('../components/Availability/Index.vue') },
  { path: '/speedtest', component: () => import('../components/SpeedTest/Index.vue') },
  { path: '/logs', component: () => import('../components/Logs/Index.vue') },
  { path: '/capture', component: () => import('../components/Capture/Index.vue') },
  { path: '/settings', component: () => import('../components/General/Index.vue') },
]

export default createRouter({
  history: createWebHistory(),
  routes
})
