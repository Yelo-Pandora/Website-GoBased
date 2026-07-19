<script setup>
import {AlertTriangle, BookOpen, RefreshCw} from '@lucide/vue';
import {computed} from 'vue';

import {renderCourseMarkdown} from '../features/course/render-course-markdown.js';
import StandaloneArchitectureDiagram from './StandaloneArchitectureDiagram.vue';

const props = defineProps({
  course: Object,
  detail: Object,
  loading: Boolean,
  error: Object,
});

defineEmits(['retry']);

const renderedContent = computed(() => renderCourseMarkdown(props.detail?.content));
</script>

<template>
  <main class="course-theory">
    <header class="workspace-header">
      <div>
        <p class="eyebrow">{{ detail?.category || course?.category || 'course' }}</p>
        <h1>{{ detail?.title || course?.title || '课程内容' }}</h1>
        <p>{{ detail?.summary || course?.summary }}</p>
      </div>
    </header>

    <section v-if="loading" class="course-message" aria-live="polite">
      <RefreshCw class="spin" :size="24" />
      <p>正在加载课程正文</p>
    </section>

    <section v-else-if="error" class="course-message course-message--error" role="alert">
      <AlertTriangle :size="24" />
      <div>
        <strong>{{ error.code }}</strong>
        <p>{{ error.message }}</p>
      </div>
      <button class="button" type="button" @click="$emit('retry')">
        <RefreshCw :size="17" />重新加载
      </button>
    </section>

    <article v-else-if="detail" class="markdown-body" v-html="renderedContent"></article>
    <StandaloneArchitectureDiagram
      v-if="detail?.slug === 'standalone-architecture'"
    />

    <section v-else class="course-message">
      <BookOpen :size="24" />
      <p>请选择一门课程开始阅读。</p>
    </section>
  </main>
</template>
