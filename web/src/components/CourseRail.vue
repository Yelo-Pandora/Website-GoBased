<script setup>
import {BookOpen, CheckCircle2, Clock3, Info, LockKeyhole} from '@lucide/vue';

defineProps({
  courses: {type: Array, default: () => []},
  selectedKey: {type: [Number, String], default: 'introduction'},
  activeCourseId: Number,
});

defineEmits(['select', 'select-introduction']);
</script>

<template>
  <aside class="course-rail" aria-label="课程列表">
    <div class="section-heading">
      <BookOpen :size="18" />
      <h2>课程目录</h2>
    </div>
    <nav class="course-list">
      <button
        class="course-item course-item--introduction"
        :class="{'course-item--selected': selectedKey === 'introduction'}"
        type="button"
        @click="$emit('select-introduction')"
      >
        <span class="course-item__icon"><Info :size="17" /></span>
        <span class="course-item__copy">
          <strong>网站介绍</strong>
          <small>平台用途与学习路线</small>
        </span>
      </button>
      <button
        v-for="course in courses"
        :key="course.id"
        class="course-item"
        :class="{'course-item--selected': course.id === selectedKey}"
        :disabled="Boolean(activeCourseId && activeCourseId !== course.id)"
        type="button"
        @click="$emit('select', course)"
      >
        <span class="course-item__icon">
          <CheckCircle2 v-if="course.id === activeCourseId" :size="17" />
          <Clock3 v-else-if="course.status === 'active'" :size="17" />
          <LockKeyhole v-else :size="17" />
        </span>
        <span class="course-item__copy">
          <strong>{{ course.title }}</strong>
          <small>{{ course.labAvailable ? '实验可用' : '理论课程' }}</small>
        </span>
      </button>
    </nav>
  </aside>
</template>
