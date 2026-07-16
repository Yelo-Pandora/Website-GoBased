<script setup>
import {CircleStop, Play, Send, ShoppingCart, Waypoints} from '@lucide/vue';
import {computed, onBeforeUnmount, ref, watch} from 'vue';

import {
  arriveAtGateway,
  createBall,
  failBall,
  receiveResult,
} from '../features/lab/traffic-state.js';

const props = defineProps({
  instances: {type: Array, default: () => []},
  policy: Object,
  running: Boolean,
  submitBatch: {type: Function, required: true},
});

const generating = ref(false);
const requestPending = ref(false);
const requestUnits = ref(60);
const generationInterval = ref(1000);
const balls = ref([]);
const instanceStates = ref({});
const processedTotal = ref(0);
const droppedTotal = ref(0);
const lastError = ref('');
const timers = new Set();

const unitsPolicy = computed(() => props.policy?.requestUnits || {
  minimum: 1, maximum: 100, step: 1, default: 60,
});
const intervalPolicy = computed(() => props.policy?.generationIntervalMs || {
  minimum: 250, maximum: 5000, step: 250, default: 1000,
});

watch(unitsPolicy, (policy) => {
  requestUnits.value = policy.default;
}, {immediate: true});

watch(intervalPolicy, (policy) => {
  generationInterval.value = policy.default;
}, {immediate: true});

watch(() => props.running, (running) => {
  if (!running) generating.value = false;
});

function schedule(callback, delay) {
  const timer = window.setTimeout(() => {
    timers.delete(timer);
    callback();
  }, delay);
  timers.add(timer);
  return timer;
}

function replaceBall(ballId, replacements) {
  const index = balls.value.findIndex((ball) => ball.id === ballId);
  if (index === -1) return;
  balls.value.splice(index, 1, ...replacements);
  for (const ball of replacements) {
    if (['moving_to_instance', 'dropped', 'error'].includes(ball.phase)) {
      schedule(() => removeBall(ball.id), 900);
    }
  }
}

function removeBall(ballId) {
  balls.value = balls.value.filter((ball) => ball.id !== ballId);
}

function markGatewayArrival(ballId) {
  const ball = balls.value.find((candidate) => candidate.id === ballId);
  if (ball) replaceBall(ballId, arriveAtGateway(ball));
}

function updateInstanceState(result) {
  instanceStates.value = {
    ...instanceStates.value,
    [result.targetInstanceId]: result.instanceState,
  };
}

function displayState(instance) {
  return instanceStates.value[instance.instanceId] || instance;
}

function targetStyle(ball) {
  const index = props.instances.findIndex((instance) => instance.instanceId === ball.targetInstanceId);
  const count = Math.max(1, props.instances.length);
  return {'--target-y': `${((Math.max(0, index) + 0.5) / count) * 100}%`};
}

async function sendOnce() {
  if (!props.running || requestPending.value) return;
  requestPending.value = true;
  lastError.value = '';
  const batchId = `batch-${crypto.randomUUID?.() || Date.now()}`.slice(0, 64);
  balls.value.push(createBall(batchId, Number(requestUnits.value)));
  schedule(() => markGatewayArrival(batchId), 650);
  try {
    const result = await props.submitBatch({
      batchId,
      productId: 1,
      requestUnits: Number(requestUnits.value),
    });
    updateInstanceState(result);
    processedTotal.value += result.processedUnits;
    droppedTotal.value += result.droppedUnits;
    const ball = balls.value.find((candidate) => candidate.id === batchId);
    if (ball) replaceBall(batchId, receiveResult(ball, result));
  } catch (error) {
    lastError.value = error?.code || 'NETWORK_ERROR';
    const ball = balls.value.find((candidate) => candidate.id === batchId);
    if (ball) replaceBall(batchId, failBall(ball));
  } finally {
    requestPending.value = false;
    if (generating.value && props.running) {
      schedule(sendOnce, Number(generationInterval.value));
    }
  }
}

function start() {
  if (!props.running || generating.value) return;
  generating.value = true;
  sendOnce();
}

function stop() {
  generating.value = false;
}

onBeforeUnmount(() => {
  for (const timer of timers) window.clearTimeout(timer);
  timers.clear();
});
</script>

<template>
  <section class="workspace-section traffic-panel" aria-labelledby="traffic-title">
    <div class="section-heading section-heading--split">
      <span><Waypoints :size="18" /><h2 id="traffic-title">真实请求流</h2></span>
      <span class="traffic-mode" :data-running="generating">{{ generating ? '生成中' : '已停止' }}</span>
    </div>

    <div class="traffic-toolbar">
      <label>
        <span>每批等效订单</span>
        <input
          v-model.number="requestUnits"
          type="number"
          :min="unitsPolicy.minimum"
          :max="unitsPolicy.maximum"
          :step="unitsPolicy.step"
          :disabled="generating"
        />
      </label>
      <label>
        <span>批次间隔</span>
        <select v-model.number="generationInterval" :disabled="generating">
          <option v-for="value in [250, 500, 1000, 2000, 5000]" :key="value" :value="value">
            {{ value }} ms
          </option>
        </select>
      </label>
      <button v-if="!generating" class="button button--primary" type="button" :disabled="!running" @click="start">
        <Play :size="17" />开始
      </button>
      <button v-else class="button" type="button" @click="stop">
        <CircleStop :size="17" />停止
      </button>
      <button class="button" type="button" :disabled="!running || requestPending" @click="sendOnce">
        <Send :size="17" />发送一批
      </button>
    </div>

    <div class="traffic-stage" aria-label="请求批次动画">
      <article class="traffic-node traffic-node--source">
        <ShoppingCart :size="21" /><strong>用户请求池</strong><small>浏览器串行生成</small>
      </article>
      <div class="traffic-route traffic-route--ingress"></div>
      <article class="traffic-node traffic-node--gateway">
        <Waypoints :size="21" /><strong>实验网关</strong><small>Nginx 实际路由</small>
      </article>
      <div class="traffic-route traffic-route--egress"></div>
      <div class="traffic-instances">
        <article
          v-for="instance in instances"
          :key="instance.instanceId"
          class="traffic-node traffic-node--instance"
          :data-load="displayState(instance).loadState || 'idle'"
        >
          <strong>{{ instance.instanceName }}</strong>
          <small>{{ displayState(instance).loadState || 'idle' }}</small>
          <span>{{ Math.round((displayState(instance).loadRatio || 0) * 100) }}%</span>
        </article>
      </div>
      <span
        v-for="ball in balls"
        :key="ball.id"
        class="traffic-ball"
        :class="`traffic-ball--${ball.phase}`"
        :data-kind="ball.kind"
        :style="targetStyle(ball)"
      >{{ ball.displayUnits }}</span>
    </div>

    <footer class="traffic-summary">
      <span>已处理 <strong>{{ processedTotal }}</strong></span>
      <span>已丢弃 <strong>{{ droppedTotal }}</strong></span>
      <span v-if="lastError" class="traffic-error">{{ lastError }}</span>
      <small>数字为教学等效订单量；每个小球仅发起一次真实 HTTP 请求。</small>
    </footer>
  </section>
</template>
