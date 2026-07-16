<script setup>
import {Gauge, Plus, Scale, Trash2} from '@lucide/vue';
import {reactive, watch} from 'vue';

const props = defineProps({
  instances: {type: Array, default: () => []},
  busy: Boolean,
  enabled: Boolean,
});

const emit = defineEmits(['action']);
const performance = reactive({});
const weights = reactive({});

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
          <small>容量 {{ instance.effectiveCapacity }} · CPU {{ instance.cpuLimitCores }}</small>
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

    <div class="weight-editor">
      <span><Scale :size="17" />固定权重</span>
      <label v-for="instance in instances" :key="instance.instanceId">
        <span>{{ instance.instanceName }}</span>
        <input v-model.number="weights[instance.instanceId]" type="number" min="1" max="100" />
      </label>
      <button class="button" type="button" :disabled="busy || !enabled" @click="applyWeights">
        应用权重
      </button>
    </div>
  </section>
</template>
