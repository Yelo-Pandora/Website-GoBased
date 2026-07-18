<script setup>
import {
  CircleStop,
  Database,
  Gauge,
  Layers3,
	MinusCircle,
  Play,
	PlusCircle,
  RotateCcw,
  Send,
  Server,
  Waypoints,
} from '@lucide/vue';
import {computed, onBeforeUnmount, onMounted, ref, watch} from 'vue';

const props = defineProps({
  instances: {type: Array, required: true},
  policy: {type: Object, default: () => ({})},
  cacheState: {type: Object, default: null},
  running: Boolean,
  submitBatch: {type: Function, required: true},
	busy: Boolean,
});

const emit = defineEmits(['action']);

const requestUnits = ref(Number(props.policy?.requestUnits?.default || 10));
const generationInterval = ref(Number(props.policy?.generationIntervalMs?.default || 250));
const generating = ref(false);
const inFlight = ref(0);
const flights = ref([]);
const catalog = ref([]);
const l1Instances = ref([]);
const redisState = ref({status: 'unknown', observedAt: '', entries: []});
const sequence = ref([]);
const sequenceIndex = ref(0);
const lastError = ref('');
const displayNow = ref(Date.now());
const totals = ref({http: 0, actual: 0, equivalent: 0, l1: 0, redis: 0, mysql: 0, degraded: 0, l1Disabled: 0, redisUnavailable: 0});

let generationTimer = null;
let clockTimer = null;
const removalTimers = new Set();

const unitsPolicy = computed(() => props.policy?.requestUnits || {minimum: 1, maximum: 100, step: 1});
const redisEntries = computed(() => redisState.value?.entries || []);

watch(() => props.cacheState, (value) => reconcile(value), {deep: true, immediate: true});

function clone(value) {
  return value ? JSON.parse(JSON.stringify(value)) : value;
}

function reconcile(value) {
  if (!value) return;
  catalog.value = clone(value.catalog || []);
  if (!sequence.value.length && catalog.value.length) refillSequence();
  const incomingInstances = clone(value.instances || []);
  for (const incoming of incomingInstances) {
    const current = l1Instances.value.find((item) => item.instanceId === incoming.instanceId);
    if (!current || timestamp(incoming.observedAt) >= timestamp(current.observedAt)) {
      const index = l1Instances.value.findIndex((item) => item.instanceId === incoming.instanceId);
      if (index >= 0) l1Instances.value.splice(index, 1, incoming);
      else l1Instances.value.push(incoming);
    }
  }
  if (timestamp(value.redis?.observedAt) >= timestamp(redisState.value?.observedAt)) {
    redisState.value = clone(value.redis || {status: 'unknown', observedAt: '', entries: []});
  }
}

function timestamp(value) {
  const parsed = new Date(value || 0).getTime();
  return Number.isFinite(parsed) ? parsed : 0;
}

function refillSequence() {
  const values = [...catalog.value];
  for (let index = values.length - 1; index > 0; index -= 1) {
    const target = Math.floor(Math.random() * (index + 1));
    [values[index], values[target]] = [values[target], values[index]];
  }
  sequence.value = values;
  sequenceIndex.value = 0;
}

function nextProduct() {
  if (!catalog.value.length) return null;
  if (sequenceIndex.value >= sequence.value.length) refillSequence();
  return sequence.value[sequenceIndex.value++];
}

function flightStyle(flight) {
  const index = Math.max(0, props.instances.findIndex((item) => item.instanceId === flight.instanceId));
  const count = Math.max(1, props.instances.length);
  return {
    '--instance-y': `${18 + ((index + 0.5) / count) * 64}%`,
    '--flight-duration': `${Math.max(120, Number(flight.latency || 120))}ms`,
  };
}

function l1State(instanceId) {
	return l1Instances.value.find((item) => item.instanceId === instanceId) ||
		{instanceId, status: 'stale', entries: []};
}

function toggleL1(instance) {
	const disabled = instance.status === 'disabled';
	emit('action', {
		actionType: disabled ? 'ADD_INSTANCE_L1' : 'REMOVE_INSTANCE_L1',
		targetInstanceId: instance.instanceId,
		parameters: {},
	});
}

function toggleRedis() {
	const absent = redisState.value.status === 'absent';
	emit('action', {
		actionType: absent ? 'ADD_SESSION_REDIS' : 'REMOVE_SESSION_REDIS',
		targetInstanceId: null,
		parameters: {},
	});
}

function applyDeltas(result) {
  const observedAt = result.cache?.observedAt || result.occurredAt;
  for (const delta of result.cache?.deltas || []) {
    if (delta.scope === 'instance') {
      let target = l1Instances.value.find((item) => item.instanceId === delta.instanceId);
      if (!target) {
        target = {instanceId: delta.instanceId, status: 'live', observedAt, entries: []};
        l1Instances.value.push(target);
      }
      mutateEntries(target.entries, delta);
      target.observedAt = observedAt;
      target.status = 'live';
    } else if (delta.scope === 'redis') {
      mutateEntries(redisState.value.entries, delta);
      redisState.value.observedAt = observedAt;
      redisState.value.status = 'running';
    }
  }
}

function mutateEntries(entries, delta) {
  const index = entries.findIndex((entry) => entry.product.id === delta.productId);
  if (delta.operation === 'remove') {
    if (index >= 0) entries.splice(index, 1);
    return;
  }
  const entry = {product: clone(delta.product), expiresAt: delta.expiresAt, ttlMs: 0};
  if (index >= 0) entries.splice(index, 1, entry);
  else entries.unshift(entry);
}

async function submitOneBatch() {
  if (!props.running || inFlight.value >= 8) return;
  const product = nextProduct();
  if (!product) return;
  const batchId = `cache-${crypto.randomUUID?.() || Date.now()}`.slice(0, 64);
  const units = Number(requestUnits.value);
  inFlight.value += 1;
  totals.value.http += 1;
  totals.value.equivalent += units;
  lastError.value = '';
  try {
    const result = await props.submitBatch({batchId, productId: product.id, requestUnits: units});
    if (!result.cache) throw new Error('CACHE_RESULT_MISSING');
    totals.value.actual += Number(result.cache.actualLookupCount || 0);
    const layer = result.cache.resolvedBy;
    if (Object.hasOwn(totals.value, layer)) totals.value[layer] += 1;
		if (result.cache.trace.some((step) => step.layer === 'l1' && step.result === 'disabled')) totals.value.l1Disabled += 1;
		if (result.cache.trace.some((step) => step.layer === 'redis' && step.result === 'unavailable')) totals.value.redisUnavailable += 1;
    applyDeltas(result);
    const flight = {
      id: batchId,
      product: result.cache.product || product,
      instanceId: result.targetInstanceId,
      resolvedBy: layer,
      latency: result.cache.simulatedLatencyMs,
      units,
    };
    flights.value.push(flight);
    const timer = window.setTimeout(() => {
      flights.value = flights.value.filter((item) => item.id !== batchId);
      removalTimers.delete(timer);
    }, Number(flight.latency) + 700);
    removalTimers.add(timer);
  } catch (error) {
    lastError.value = error?.code || error?.message || 'NETWORK_ERROR';
  } finally {
    inFlight.value -= 1;
  }
}

function start() {
  if (!props.running || generating.value) return;
  generating.value = true;
  submitOneBatch();
  generationTimer = window.setInterval(submitOneBatch, Number(generationInterval.value));
}

function stop() {
  generating.value = false;
  if (generationTimer !== null) window.clearInterval(generationTimer);
  generationTimer = null;
}

function ttlSeconds(entry) {
  return Math.max(0, Math.ceil((timestamp(entry.expiresAt) - displayNow.value) / 1000));
}

function categoryLabel(value) {
  return {electronics: '电子', books: '图书', home: '家居', sports: '运动'}[value] || value;
}

function resetView() {
  totals.value = {http: 0, actual: 0, equivalent: 0, l1: 0, redis: 0, mysql: 0, degraded: 0, l1Disabled: 0, redisUnavailable: 0};
  flights.value = [];
}

onMounted(() => {
  clockTimer = window.setInterval(() => { displayNow.value = Date.now(); }, 250);
});

onBeforeUnmount(() => {
  stop();
  if (clockTimer !== null) window.clearInterval(clockTimer);
  for (const timer of removalTimers) window.clearTimeout(timer);
});
</script>

<template>
  <section class="workspace-section cache-panel" aria-labelledby="cache-title">
    <div class="section-heading section-heading--split">
      <span><Layers3 :size="18" /><h2 id="cache-title">真实多级缓存请求流</h2></span>
      <span class="traffic-mode" :data-running="generating">{{ generating ? `${inFlight} 个批次在途` : '已停止' }}</span>
    </div>

    <div class="traffic-toolbar cache-toolbar">
      <label><span>教学等效数量</span><input v-model.number="requestUnits" type="number" :min="unitsPolicy.minimum" :max="unitsPolicy.maximum" :step="unitsPolicy.step" /></label>
      <label><span>批次间隔</span><select v-model.number="generationInterval" :disabled="generating"><option v-for="value in [250, 500, 1000, 2000]" :key="value" :value="value">{{ value }} ms</option></select></label>
      <button v-if="!generating" class="button button--primary" type="button" :disabled="!running || !catalog.length" @click="start"><Play :size="17" />开始</button>
      <button v-else class="button" type="button" @click="stop"><CircleStop :size="17" />停止</button>
      <button class="button" type="button" :disabled="!running || inFlight >= 8 || !catalog.length" @click="submitOneBatch"><Send :size="17" />发送一批</button>
      <button class="icon-button" type="button" title="清空统计" @click="resetView"><RotateCcw :size="17" /></button>
    </div>

    <div class="cache-stage" aria-label="多级缓存命中动画">
      <article class="cache-node cache-node--source"><Waypoints :size="20" /><strong>请求流</strong><small>HTTP 批次</small></article>
      <article class="cache-node cache-node--gateway"><Gauge :size="20" /><strong>Nginx</strong><small>固定等权</small></article>
      <div class="cache-app-stack">
        <article v-for="instance in instances" :key="instance.instanceId" class="cache-node cache-node--app" :data-l1="l1State(instance.instanceId).status">
          <Server :size="18" /><span><strong>{{ instance.instanceName }}</strong><small>{{ l1State(instance.instanceId).status === 'disabled' ? 'L1 已去除' : 'L1 · 9 项' }}</small></span>
        </article>
      </div>
      <article class="cache-node cache-node--redis" :data-status="redisState.status"><Database :size="20" /><strong>Redis L2</strong><small>共享缓存</small></article>
      <article class="cache-node cache-node--mysql"><Database :size="20" /><strong>MySQL</strong><small>真实回源</small></article>
      <span v-for="flight in flights" :key="flight.id" class="cache-flight" :class="`cache-flight--${flight.resolvedBy}`" :style="flightStyle(flight)" :title="`${flight.product.name} · ${flight.resolvedBy} · ${flight.latency}ms`">{{ flight.product.id }}</span>
    </div>

    <div class="cache-metrics">
      <span>HTTP <strong>{{ totals.http }}</strong></span><span>真实查询 <strong>{{ totals.actual }}</strong></span><span>教学等效 <strong>{{ totals.equivalent }}</strong></span>
      <span class="cache-hit cache-hit--l1">L1 <strong>{{ totals.l1 }}</strong></span><span class="cache-hit cache-hit--redis">Redis <strong>{{ totals.redis }}</strong></span><span class="cache-hit cache-hit--mysql">MySQL <strong>{{ totals.mysql }}</strong></span>
		<span>L1 已跳过 <strong>{{ totals.l1Disabled }}</strong></span><span>Redis 不可用 <strong>{{ totals.redisUnavailable }}</strong></span>
      <span v-if="lastError" class="traffic-error">{{ lastError }}</span>
    </div>

    <div class="cache-inventories">
      <article v-for="instance in l1Instances" :key="instance.instanceId" class="cache-inventory" :data-status="instance.status">
        <header><span><Server :size="17" /><strong>{{ instance.instanceId }} L1</strong></span><span class="cache-inventory__actions"><small>{{ instance.status === 'disabled' ? '0/0 · 已去除' : `${instance.entries.length}/9 · ${instance.status}` }}</small><button class="button button--compact" type="button" :disabled="busy || !running" @click="toggleL1(instance)"><PlusCircle v-if="instance.status === 'disabled'" :size="14" /><MinusCircle v-else :size="14" />{{ instance.status === 'disabled' ? '添加 L1' : '去除 L1' }}</button></span></header>
        <div class="cache-entry-grid"><span v-for="entry in instance.entries" :key="entry.product.id" class="cache-entry" :data-category="entry.product.category"><b>#{{ entry.product.id }}</b><em>{{ categoryLabel(entry.product.category) }}</em><small>v{{ entry.product.version }} · {{ ttlSeconds(entry) }}s</small></span><small v-if="!instance.entries.length" class="cache-empty">空</small></div>
      </article>
      <article class="cache-inventory cache-inventory--redis" :data-status="redisState.status">
        <header><span><Database :size="17" /><strong>共享 Redis L2</strong></span><span class="cache-inventory__actions"><small>{{ redisState.status === 'absent' ? '0/0 · 未部署' : `${redisEntries.length}/${catalog.length} · ${redisState.status}` }}</small><button class="button button--compact" type="button" :disabled="busy || !running" @click="toggleRedis"><PlusCircle v-if="redisState.status === 'absent'" :size="14" /><MinusCircle v-else :size="14" />{{ redisState.status === 'absent' ? '添加 Redis' : '去除 Redis' }}</button></span></header>
        <div class="cache-entry-grid"><span v-for="entry in redisEntries" :key="entry.product.id" class="cache-entry" :data-category="entry.product.category"><b>#{{ entry.product.id }}</b><em>{{ categoryLabel(entry.product.category) }}</em><small>v{{ entry.product.version }} · {{ ttlSeconds(entry) }}s</small></span><small v-if="!redisEntries.length" class="cache-empty">空</small></div>
      </article>
    </div>
  </section>
</template>
