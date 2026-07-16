<script setup>
import {Activity, CircleCheck, CircleX, LoaderCircle} from '@lucide/vue';

defineProps({operation: Object});

const actionNames = {
  CREATE_LAB: '创建实验',
  RESET_LAB: '重置实验',
  DESTROY_LAB: '结束实验',
};

function isBusy(status) {
  return ['pending', 'claimed', 'running', 'compensating'].includes(status);
}
</script>

<template>
  <section class="workspace-section operation-panel" aria-labelledby="operation-title">
    <div class="section-heading">
      <Activity :size="18" />
      <h2 id="operation-title">最新操作</h2>
    </div>
    <div v-if="operation" class="operation-row">
      <LoaderCircle v-if="isBusy(operation.status)" class="spin" :size="20" />
      <CircleCheck v-else-if="operation.status === 'succeeded'" class="icon-success" :size="20" />
      <CircleX v-else class="icon-danger" :size="20" />
      <div class="operation-row__copy">
        <strong>{{ actionNames[operation.action] || operation.action }}</strong>
        <code>{{ operation.operationId }}</code>
      </div>
      <span class="status-badge" :data-status="operation.status">{{ operation.status }}</span>
      <time>{{ new Date(operation.completedAt || operation.submittedAt).toLocaleString('zh-CN') }}</time>
    </div>
    <p v-else class="empty-copy">暂无实验操作</p>
    <p v-if="operation?.error" class="operation-error">
      <strong>{{ operation.error.code }}</strong>
      {{ operation.error.message }}
    </p>
  </section>
</template>
