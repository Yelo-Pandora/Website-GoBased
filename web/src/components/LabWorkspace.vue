<script setup>
import {
  AlertTriangle,
  FlaskConical,
  Plus,
  RefreshCw,
  RotateCcw,
  Trash2,
} from '@lucide/vue';
import {computed} from 'vue';

import LifecyclePanel from './LifecyclePanel.vue';
import OperationPanel from './OperationPanel.vue';
import TopologyPanel from './TopologyPanel.vue';

const props = defineProps({
  course: Object,
  snapshot: Object,
  now: {type: Number, required: true},
  busy: Boolean,
  loading: Boolean,
  error: Object,
});

defineEmits(['create', 'reset', 'terminate', 'refresh']);

const activeStatuses = new Set(['Preparing', 'Running', 'Expiring', 'Terminating']);
const active = computed(() => activeStatuses.has(props.snapshot?.lab?.status));
const canReset = computed(() => ['Running', 'Expiring'].includes(props.snapshot?.lab?.status));
const canTerminate = computed(() => ['Preparing', 'Running', 'Expiring', 'Failed'].includes(props.snapshot?.lab?.status));

const statusNames = {
  Preparing: '准备中',
  Running: '运行中',
  Expiring: '即将过期',
  Failed: '失败',
  Terminating: '正在结束',
  Terminated: '已结束',
};

const reasonNames = {
  user_requested: '用户主动结束',
  idle_timeout: '空闲超时',
  maximum_duration: '达到最长时限',
  destroy_failed: '资源清理失败',
};
</script>

<template>
  <main class="lab-workspace">
    <header class="workspace-header">
      <div>
        <p class="eyebrow">{{ course?.category || 'experiment' }}</p>
        <h1>{{ course?.title || '实验工作台' }}</h1>
        <p>{{ course?.summary }}</p>
      </div>
      <div v-if="snapshot" class="workspace-header__state">
        <span class="lab-status" :data-status="snapshot.lab.status">
          {{ statusNames[snapshot.lab.status] || snapshot.lab.status }}
        </span>
        <small v-if="snapshot.lab.terminationReason">
          {{ reasonNames[snapshot.lab.terminationReason] || snapshot.lab.terminationReason }}
        </small>
      </div>
    </header>

    <div v-if="error" class="error-banner" role="alert">
      <AlertTriangle :size="19" />
      <div><strong>{{ error.code }}</strong><span>{{ error.message }}</span></div>
      <code v-if="error.requestId">{{ error.requestId }}</code>
    </div>

    <section v-if="!snapshot" class="empty-workspace">
      <FlaskConical :size="34" />
      <h2>{{ course?.labAvailable ? '尚未创建实验' : '该课程暂无实验' }}</h2>
      <button
        v-if="course?.labAvailable"
        class="button button--primary"
        type="button"
        :disabled="busy"
        @click="$emit('create')"
      >
        <Plus :size="18" />{{ busy ? '正在创建' : '创建实验' }}
      </button>
    </section>

    <template v-else>
      <div class="workspace-toolbar">
        <button class="icon-button" type="button" title="刷新快照" :disabled="loading" @click="$emit('refresh')">
          <RefreshCw :class="{spin: loading}" :size="18" />
        </button>
        <button class="button" type="button" :disabled="busy || !canReset" @click="$emit('reset')">
          <RotateCcw :size="18" />重置
        </button>
        <button class="button button--danger" type="button" :disabled="busy || !canTerminate" @click="$emit('terminate')">
          <Trash2 :size="18" />结束
        </button>
        <button
          v-if="!active"
          class="button button--primary"
          type="button"
          :disabled="busy || !course?.labAvailable"
          @click="$emit('create')"
        >
          <Plus :size="18" />新建实验
        </button>
      </div>

      <TopologyPanel :lab="snapshot.lab" :topology="snapshot.topology" />
      <LifecyclePanel
        v-if="!['Terminated', 'Failed'].includes(snapshot.lab.status)"
        :lab="snapshot.lab"
        :now="now"
      />
      <OperationPanel :operation="snapshot.latestOperation" />
    </template>
  </main>
</template>
