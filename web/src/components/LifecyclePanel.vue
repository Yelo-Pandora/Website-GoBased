<script setup>
import {Clock3, Hourglass, TimerReset} from '@lucide/vue';

const props = defineProps({lab: {type: Object, required: true}, now: {type: Number, required: true}});

function remaining(expiresAt) {
  if (!expiresAt) return '--:--';
  const seconds = Math.max(0, Math.floor((new Date(expiresAt).getTime() - props.now) / 1000));
  if (seconds === 0) return '已到期';
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const rest = seconds % 60;
  if (hours > 0) {
    return `${String(hours).padStart(2, '0')}:${String(minutes).padStart(2, '0')}:${String(rest).padStart(2, '0')}`;
  }
  return `${String(minutes).padStart(2, '0')}:${String(rest).padStart(2, '0')}`;
}

function percentage(startAt, expiresAt) {
  if (!startAt || !expiresAt) return 0;
  const start = new Date(startAt).getTime();
  const end = new Date(expiresAt).getTime();
  if (end <= start) return 0;
  return Math.max(0, Math.min(100, ((end - props.now) / (end - start)) * 100));
}
</script>

<template>
  <section class="workspace-section lifecycle-panel" aria-labelledby="lifecycle-title">
    <div class="section-heading">
      <Hourglass :size="18" />
      <h2 id="lifecycle-title">生命周期</h2>
    </div>
    <div class="deadline-grid">
      <article class="deadline-item">
        <div class="deadline-item__header">
          <span><TimerReset :size="17" />空闲时限</span>
          <strong>{{ remaining(lab.idleExpiresAt) }}</strong>
        </div>
        <div class="meter" aria-hidden="true">
          <span :style="{width: `${percentage(lab.lastEffectiveActionAt, lab.idleExpiresAt)}%`}"></span>
        </div>
        <time>{{ lab.idleExpiresAt ? new Date(lab.idleExpiresAt).toLocaleString('zh-CN') : '等待实验启动' }}</time>
      </article>
      <article class="deadline-item">
        <div class="deadline-item__header">
          <span><Clock3 :size="17" />最长时限</span>
          <strong>{{ remaining(lab.maximumExpiresAt) }}</strong>
        </div>
        <div class="meter meter--maximum" aria-hidden="true">
          <span :style="{width: `${percentage(lab.startedAt, lab.maximumExpiresAt)}%`}"></span>
        </div>
        <time>{{ lab.maximumExpiresAt ? new Date(lab.maximumExpiresAt).toLocaleString('zh-CN') : '等待实验启动' }}</time>
      </article>
    </div>
  </section>
</template>
