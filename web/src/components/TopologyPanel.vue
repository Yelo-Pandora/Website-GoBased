<script setup>
import {Database, Network, Server, Waypoints} from '@lucide/vue';

defineProps({lab: {type: Object, required: true}, topology: {type: Object, required: true}});

function stateClass(status) {
  const value = String(status || '').toLowerCase();
  if (['running', 'ready', 'succeeded'].includes(value)) return 'resource-state--ready';
  if (['preparing', 'pending', 'claimed', 'unknown'].includes(value)) return 'resource-state--pending';
  if (['failed', 'deleted', 'terminated'].includes(value)) return 'resource-state--failed';
  return '';
}

function databaseState(lab) {
  if (lab.status === 'Terminated') return '已清理';
  if (lab.status === 'Failed') return '状态未知';
  return lab.startedAt ? '已分配' : '等待创建';
}
</script>

<template>
  <section class="workspace-section topology-panel" aria-labelledby="topology-title">
    <div class="section-heading section-heading--split">
      <span><Waypoints :size="18" /><h2 id="topology-title">实验拓扑</h2></span>
      <code>{{ lab.id }}</code>
    </div>
    <div class="topology-flow">
      <article class="resource-node">
        <Network :size="21" />
        <div>
          <strong>实验网关</strong>
          <span class="resource-state" :class="stateClass(topology.gateway?.status)">
            {{ topology.gateway?.status || 'unknown' }}
          </span>
        </div>
      </article>
      <div class="topology-link" aria-hidden="true"></div>
      <div class="instance-stack">
        <article v-for="instance in topology.instances" :key="instance.instanceId" class="resource-node">
          <Server :size="21" />
          <div>
            <strong>{{ instance.instanceName }}</strong>
            <span class="resource-state" :class="stateClass(instance.status)">{{ instance.status }}</span>
          </div>
          <dl>
            <div><dt>CPU</dt><dd>{{ instance.cpuLimitCores }}</dd></div>
            <div><dt>处理速度</dt><dd>{{ instance.processingSpeed }}/秒</dd></div>
            <div><dt>最大负载</dt><dd>{{ instance.maxLoad }}</dd></div>
            <div><dt>权重</dt><dd>{{ instance.currentWeight }}</dd></div>
          </dl>
        </article>
        <article v-if="topology.instances.length === 0" class="resource-node resource-node--empty">
          <Server :size="21" />
          <div><strong>应用实例</strong><span>等待创建</span></div>
        </article>
      </div>
      <div class="topology-link" aria-hidden="true"></div>
      <div class="data-stack">
        <article class="resource-node">
          <Database :size="21" />
          <div>
            <strong>实验数据库</strong>
            <span>{{ databaseState(lab) }}</span>
          </div>
        </article>
        <article v-if="topology.redis" class="resource-node">
          <Database :size="21" />
          <div>
            <strong>会话 Redis</strong>
            <span class="resource-state" :class="stateClass(topology.redis.status)">{{ topology.redis.status }}</span>
          </div>
        </article>
      </div>
    </div>
  </section>
</template>
