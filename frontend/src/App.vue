<script setup lang="ts">
import { RouterView } from 'vue-router'
import Sidebar from './components/Sidebar.vue'

// 主题初始化与系统主题跟随由 main.ts 的 initTheme()（ThemeManager）统一负责，勿在组件层重复实现

</script>

<template>
  <div class="app-layout">
    <Sidebar />
    <main class="main-content">
      <RouterView v-slot="{ Component }">
        <keep-alive>
          <component :is="Component" />
        </keep-alive>
      </RouterView>
    </main>
  </div>
</template>

<style scoped>
.app-layout {
  display: flex;
  height: 100vh;
  width: 100vw;
  overflow: hidden;
}

.main-content {
  flex: 1;
  min-width: 0;
  min-height: 0;
  overflow-y: auto;
  background: var(--mac-bg);
}

@media (max-width: 700px) {
  .app-layout {
    flex-direction: column-reverse;
  }

  .main-content {
    width: 100%;
  }
}
</style>
