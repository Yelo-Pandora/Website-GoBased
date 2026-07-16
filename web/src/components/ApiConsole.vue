<script setup>
import {onBeforeUnmount, onMounted, ref} from 'vue';
import SwaggerUI from 'swagger-ui-dist/swagger-ui-es-bundle.js';
import 'swagger-ui-dist/swagger-ui.css';

const swaggerRoot = ref(null);
let swagger = null;

onMounted(() => {
  swagger = SwaggerUI({
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

onBeforeUnmount(() => swagger?.unmount?.());
</script>

<template>
  <main class="api-view">
    <div ref="swaggerRoot"></div>
  </main>
</template>
