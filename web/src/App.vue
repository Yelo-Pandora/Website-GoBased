<script setup>
import {onMounted, ref} from 'vue';
import SwaggerUI from 'swagger-ui-dist/swagger-ui-es-bundle.js';
import 'swagger-ui-dist/swagger-ui.css';

const swaggerRoot = ref(null);

onMounted(() => {
  SwaggerUI({
    deepLinking: true,
    displayRequestDuration: true,
    docExpansion: 'list',
    domNode: swaggerRoot.value,
    filter: true,
    persistAuthorization: true,
    tryItOutEnabled: true,
    url: '/platform-api.openapi.yaml',
    withCredentials: true,
  });
});
</script>

<template>
  <div class="api-console">
    <header class="api-console__header">
      <div>
        <p class="api-console__product">互联网后端架构学习平台</p>
        <h1>Platform API Swagger UI</h1>
      </div>
      <code>/platform-api.openapi.yaml</code>
    </header>

    <main class="api-console__content">
      <div ref="swaggerRoot"></div>
    </main>
  </div>
</template>
