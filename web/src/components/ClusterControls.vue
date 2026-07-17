<script setup>
import {Gauge, Plus, Scale, Trash2} from '@lucide/vue';
import {computed, reactive, watch} from 'vue';

import {buildBalancerView} from '../features/lab/balancer-view.js';

const props = defineProps({
  instances: {type: Array, default: () => []},
  busy: Boolean,
  enabled: Boolean,
  mode: {type: String, default: 'fixed'},
  balancer: {type: Object, default: null},
});

const emit = defineEmits(['action']);
const performance = reactive({});
const weights = reactive({});
const balancerView = computed(() => buildBalancerView(props.instances, props.balancer));

watch(() => props.instances, (instances) => {
  for (const instance of instances) {
    performance[instance.instanceId] = instance.performancePercent;
    weights[instance.instanceId] = instance.currentWeight;
  }
}, {immediate: true, deep: true});

function submit(actionType, targetInstanceId = null, parameters = {}) {
  emit('action', {actionType, targetInstanceId, parameters});
}

function applyWeights() {
  submit('SET_INSTANCE_WEIGHTS', null, {
    weights: props.instances.map((instance) => ({
      instanceId: instance.instanceId,
      weight: Number(weights[instance.instanceId]),
    })),
  });
}

function setMode(mode) {
  if (mode === props.mode) return;
  submit('SET_BALANCING_MODE', null, {balancingMode: mode});
}
</script>

<template>
  <section class="workspace-section cluster-controls" aria-labelledby="cluster-controls-title">
    <div class="section-heading section-heading--split">
      <span><Gauge :size="18" /><h2 id="cluster-controls-title">集群控制</h2></span>
      <button
        class="button button--primary"
        type="button"
        :disabled="busy || !enabled || instances.length >= 4"
        @click="submit('ADD_INSTANCE')"
      >
        <Plus :size="17" />增加实例
      </button>
    </div>

    <div class="cluster-instance-list">
      <article v-for="instance in instances" :key="instance.instanceId" class="cluster-instance-row">
        <div>
          <strong>{{ instance.instanceName }}</strong>
          <small>
            容量 {{ instance.effectiveCapacity }} · CPU {{ instance.cpuLimitCores }}
            <template v-if="mode === 'adaptive'">
              · 目标权重 {{ balancerView.targetFor(instance.instanceId) }}
            </template>
          </small>
        </div>
        <label>
          <span>性能</span>
          <select v-model.number="performance[instance.instanceId]" :disabled="busy || !enabled">
            <option v-for="value in [20, 30, 40, 50, 60, 70, 80, 90, 100]" :key="value" :value="value">
              {{ value }}%
            </option>
          </select>
        </label>
        <button
          class="button"
          type="button"
          :disabled="busy || !enabled || performance[instance.instanceId] === instance.performancePercent"
          @click="submit('SET_INSTANCE_PERFORMANCE', instance.instanceId, {
            performancePercent: performance[instance.instanceId],
          })"
        >应用</button>
        <button
          class="icon-button"
          type="button"
          title="删除实例"
          :disabled="busy || !enabled || instances.length <= 1"
          @click="submit('REMOVE_INSTANCE', instance.instanceId)"
        ><Trash2 :size="17" /></button>
      </article>
    </div>

    <div class="balancing-mode-control">
      <span><Scale :size="17" />负载均衡模式</span>
      <div class="mode-toggle" role="group" aria-label="负载均衡模式">
        <button
          type="button"
          :class="{ 'is-active': mode === 'fixed' }"
          :aria-pressed="mode === 'fixed'"
          :disabled="busy || !enabled"
          @click="setMode('fixed')"
        >固定</button>
        <button
          type="button"
          :class="{ 'is-active': mode === 'adaptive' }"
          :aria-pressed="mode === 'adaptive'"
          :disabled="busy || !enabled"
          @click="setMode('adaptive')"
        >自适应</button>
      </div>
    </div>

    <div v-if="mode === 'fixed'" class="weight-editor">
      <span><Scale :size="17" />固定权重</span>
      <label v-for="instance in instances" :key="instance.instanceId">
        <span>{{ instance.instanceName }}</span>
        <input
          v-model.number="weights[instance.instanceId]"
          type="number"
          min="1"
          max="100"
          :disabled="busy || !enabled"
        />
      </label>
      <button class="button" type="button" :disabled="busy || !enabled" @click="applyWeights">
        应用权重
      </button>
    </div>

    <div v-else class="adaptive-balancer" :data-status="balancerView.status">
      <div>
        <Scale :size="18" />
        <span><strong>{{ balancerView.statusText }}</strong><small>后端每 2 秒采样一次</small></span>
      </div>
      <code>容量 {{ balancerView.capacityRatio }} → 权重 {{ balancerView.weightRatio }}</code>
      <p v-if="balancerView.notice">{{ balancerView.notice }}</p>
      <p v-if="balancerView.error" class="adaptive-balancer__error">
        {{ balancerView.error.code }}：{{ balancerView.error.message }}
      </p>
    </div>
  </section>
</template>
