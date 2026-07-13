<script setup>
import {onMounted, ref} from 'vue';

const platform = ref({
  service: 'platform-api',
  status: 'checking',
  version: 'unknown',
});
const requestError = ref('');

async function loadPlatformInfo() {
  try {
    const response = await fetch('/api/v1/system/info', {
      headers: {'Accept': 'application/json'},
    });
    if (!response.ok) {
      throw new Error(`HTTP ${response.status}`);
    }
    platform.value = await response.json();
  } catch (error) {
    requestError.value = error instanceof Error ? error.message : 'unknown error';
    platform.value.status = 'unavailable';
  }
}

onMounted(loadPlatformInfo);
</script>

<template>
  <div class="app-shell">
    <header class="topbar">
      <div class="brand-mark" aria-hidden="true">BA</div>
      <div>
        <p class="product-name">互联网后端架构学习平台</p>
        <p class="environment-name">Foundation scaffold</p>
      </div>
    </header>

    <main class="workspace">
      <section class="workspace-heading" aria-labelledby="page-title">
        <div>
          <p class="eyebrow">运行状态</p>
          <h1 id="page-title">平台底层服务</h1>
        </div>
        <span class="status-summary" :data-status="platform.status">
          {{ platform.status }}
        </span>
      </section>

      <section class="service-grid" aria-label="基础服务状态">
        <article class="service-item">
          <div class="service-item__header">
            <h2>Edge Nginx</h2>
            <span class="status-dot status-dot--healthy" aria-label="运行正常"></span>
          </div>
          <p>Vue 静态资源与平台 API 入口</p>
          <code>:8080</code>
        </article>

        <article class="service-item">
          <div class="service-item__header">
            <h2>Platform API</h2>
            <span
              class="status-dot"
              :class="platform.status === 'scaffold' ? 'status-dot--healthy' : 'status-dot--waiting'"
              :aria-label="platform.status"
            ></span>
          </div>
          <p>控制面 API 与数据库就绪检查</p>
          <code>{{ platform.version }}</code>
        </article>

        <article class="service-item">
          <div class="service-item__header">
            <h2>Orchestrator</h2>
            <span class="status-dot status-dot--internal" aria-label="内部服务"></span>
          </div>
          <p>Unix Domain Socket 内部编排边界</p>
          <code>/run/platform/orchestrator.sock</code>
        </article>

        <article class="service-item">
          <div class="service-item__header">
            <h2>Lab Runtime</h2>
            <span class="status-dot status-dot--idle" aria-label="按需创建"></span>
          </div>
          <p>实验应用与 Redis 按会话动态创建</p>
          <code>profile: images</code>
        </article>
      </section>

      <p v-if="requestError" class="error-message" role="status">
        Platform API unavailable: {{ requestError }}
      </p>
    </main>
  </div>
</template>
