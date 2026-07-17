<script setup>
import {Braces, FlaskConical, LoaderCircle, LogOut, ServerCog, UserRound} from '@lucide/vue';
import {computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, watch} from 'vue';

import {
  ApiError,
  createLab,
  currentUser,
  getLab,
  listCourses,
  login,
  logout,
  resetLab,
  submitLabAction,
  submitTrafficBatch,
  terminateLab,
} from './api/platform.js';
import CourseRail from './components/CourseRail.vue';
import LabWorkspace from './components/LabWorkspace.vue';
import LoginPanel from './components/LoginPanel.vue';

const ApiConsole = defineAsyncComponent(() => import('./components/ApiConsole.vue'));

const activeStatuses = new Set(['Preparing', 'Running', 'Expiring', 'Terminating']);
const busyOperationStatuses = new Set(['pending', 'claimed', 'running', 'compensating']);

const view = ref('lab');
const bootstrapping = ref(true);
const authBusy = ref(false);
const requestBusy = ref(false);
const snapshotLoading = ref(false);
const user = ref(null);
const courses = ref([]);
const selectedCourseId = ref(null);
const snapshot = ref(null);
const error = ref(null);
const now = ref(Date.now());

let pollTimer = null;
let clockTimer = null;

const selectedCourse = computed(() =>
  courses.value.find((course) => course.id === selectedCourseId.value) || courses.value[0] || null,
);
const activeCourseId = computed(() =>
  activeStatuses.has(snapshot.value?.lab?.status) ? snapshot.value.lab.courseId : null,
);
const labBusy = computed(() => requestBusy.value ||
  busyOperationStatuses.has(snapshot.value?.latestOperation?.status));

const messageByCode = {
  AUTH_REQUIRED: '登录状态已失效，请重新登录。',
  INVALID_CREDENTIALS: '账号或密码不正确。',
  LOGIN_RATE_LIMITED: '登录尝试过于频繁，请稍后再试。',
  CSRF_INVALID: '安全令牌已失效，请重新登录。',
  LAB_ALREADY_ACTIVE: '当前账号已经有一个运行中的实验。',
  LAB_BUSY: '实验正在执行另一项操作，请等待状态更新。',
  LAB_NOT_FOUND: '实验不存在或已不可访问。',
  LAB_NOT_RUNNING: '当前实验状态不允许执行该操作。',
  RESOURCE_CAPACITY_EXCEEDED: '实验资源已达到平台上限。',
  DOCKER_UNAVAILABLE: '实验资源暂时不可用。',
  NETWORK_ERROR: '无法连接平台服务。',
  TRAFFIC_REQUEST_INVALID: '批次参数或商品无效。',
  RATE_LIMITED: '请求批次过快，请降低生成频率。',
  LAB_UNAVAILABLE: '实验网关或应用实例暂时不可用。',
  INSTANCE_NOT_FOUND: '目标实例不存在。',
  MIN_INSTANCE_LIMIT: '集群必须至少保留一台实例。',
  MAX_INSTANCE_LIMIT: '集群最多允许四台实例。',
  ACTION_NOT_ALLOWED: '当前场景或负载均衡模式不允许该操作。',
};

function presentError(value) {
  const apiError = value instanceof ApiError ? value : new ApiError(String(value));
  return {
    code: apiError.code,
    message: messageByCode[apiError.code] || apiError.message,
    requestId: apiError.requestId,
  };
}

function storageKey() {
  return user.value ? `platform.currentLab.${user.value.id}` : '';
}

function rememberLab(labId) {
  if (!storageKey()) return;
  if (labId) localStorage.setItem(storageKey(), labId);
  else localStorage.removeItem(storageKey());
}

function clearPoll() {
  if (pollTimer) window.clearTimeout(pollTimer);
  pollTimer = null;
}

function schedulePoll() {
  clearPoll();
  if (!user.value || view.value !== 'lab' || document.hidden || !snapshot.value?.lab?.id) return;
  const status = snapshot.value.lab.status;
  const operationStatus = snapshot.value.latestOperation?.status;
  const balancerStatus = snapshot.value.balancer?.status;
  const fast = ['Preparing', 'Expiring', 'Terminating'].includes(status) ||
    busyOperationStatuses.has(operationStatus) || balancerStatus === 'converging';
  pollTimer = window.setTimeout(() => loadSnapshot({silent: true}), fast ? 1000 : 5000);
}

async function loadCourseCatalog() {
  courses.value = await listCourses();
  if (!selectedCourseId.value) {
    selectedCourseId.value = courses.value.find((course) => course.labAvailable)?.id || courses.value[0]?.id;
  }
}

async function restoreLab() {
  const candidates = [];
  const remembered = storageKey() ? localStorage.getItem(storageKey()) : '';
  if (remembered) candidates.push(remembered);
  for (const course of courses.value) {
    const lastLabId = course.progress?.lastLabId;
    if (lastLabId && !candidates.includes(lastLabId)) candidates.push(lastLabId);
  }
  let terminalSnapshot = null;
  for (const labId of candidates) {
    try {
      const restored = await getLab(labId);
      if (activeStatuses.has(restored.lab.status)) {
        snapshot.value = restored;
        selectedCourseId.value = restored.lab.courseId;
        rememberLab(labId);
        schedulePoll();
        return;
      }
      terminalSnapshot ||= restored;
    } catch (value) {
      if (!(value instanceof ApiError) || value.code !== 'LAB_NOT_FOUND') throw value;
    }
  }
  snapshot.value = terminalSnapshot;
  if (terminalSnapshot) {
    selectedCourseId.value = terminalSnapshot.lab.courseId;
    rememberLab(terminalSnapshot.lab.id);
  } else {
    rememberLab('');
  }
}

async function restoreSession() {
  bootstrapping.value = true;
  try {
    const auth = await currentUser();
    user.value = auth.user;
    await loadCourseCatalog();
    await restoreLab();
  } catch (value) {
    if (!(value instanceof ApiError) || value.status !== 401) error.value = presentError(value);
    user.value = null;
  } finally {
    bootstrapping.value = false;
  }
}

async function handleLogin(credentials) {
  authBusy.value = true;
  error.value = null;
  try {
    const auth = await login(credentials.username, credentials.password);
    user.value = auth.user;
    await loadCourseCatalog();
    await restoreLab();
  } catch (value) {
    error.value = presentError(value);
  } finally {
    authBusy.value = false;
  }
}

async function handleLogout() {
  requestBusy.value = true;
  error.value = null;
  try {
    await logout();
  } catch (value) {
    if (!(value instanceof ApiError) || value.status !== 401) error.value = presentError(value);
  } finally {
    clearPoll();
    user.value = null;
    courses.value = [];
    snapshot.value = null;
    selectedCourseId.value = null;
    requestBusy.value = false;
  }
}

async function loadSnapshot({silent = false} = {}) {
  const labId = snapshot.value?.lab?.id;
  if (!labId) return;
  if (!silent) snapshotLoading.value = true;
  try {
    snapshot.value = await getLab(labId);
    selectedCourseId.value = snapshot.value.lab.courseId;
    error.value = null;
  } catch (value) {
    if (value instanceof ApiError && value.status === 401) {
      clearPoll();
      user.value = null;
    } else {
      error.value = presentError(value);
    }
  } finally {
    snapshotLoading.value = false;
    schedulePoll();
  }
}

async function handleCreate() {
  if (!selectedCourse.value?.labAvailable) return;
  requestBusy.value = true;
  error.value = null;
  try {
    const data = await createLab(selectedCourse.value.id);
    snapshot.value = {
      lab: data.lab,
      topology: {instances: [], redis: null, gateway: {status: 'unknown'}},
      latestOperation: data.operation,
    };
    rememberLab(data.lab.id);
    schedulePoll();
  } catch (value) {
    error.value = presentError(value);
    if (value instanceof ApiError && value.code === 'LAB_ALREADY_ACTIVE') {
      await loadCourseCatalog();
      await restoreLab();
    }
  } finally {
    requestBusy.value = false;
  }
}

async function handleReset() {
  if (!snapshot.value?.lab?.id) return;
  requestBusy.value = true;
  error.value = null;
  try {
    const data = await resetLab(snapshot.value.lab.id);
    snapshot.value = {...snapshot.value, latestOperation: data.operation};
    schedulePoll();
  } catch (value) {
    error.value = presentError(value);
  } finally {
    requestBusy.value = false;
  }
}

async function handleTerminate() {
  if (!snapshot.value?.lab?.id || !window.confirm('结束当前实验并清理全部实验资源？')) return;
  requestBusy.value = true;
  error.value = null;
  try {
    const data = await terminateLab(snapshot.value.lab.id);
    snapshot.value = {...snapshot.value, latestOperation: data.operation};
    schedulePoll();
  } catch (value) {
    error.value = presentError(value);
  } finally {
    requestBusy.value = false;
  }
}

async function handleLabAction(action) {
  if (!snapshot.value?.lab?.id) return;
  requestBusy.value = true;
  error.value = null;
  try {
    const data = await submitLabAction(snapshot.value.lab.id, action);
    snapshot.value = {...snapshot.value, latestOperation: data.operation};
    schedulePoll();
  } catch (value) {
    error.value = presentError(value);
  } finally {
    requestBusy.value = false;
  }
}

async function handleTrafficBatch(batch) {
  if (!snapshot.value?.lab?.id) throw new ApiError('实验不存在', {code: 'LAB_NOT_FOUND'});
  return submitTrafficBatch(snapshot.value.lab.id, batch);
}

function selectCourse(course) {
  if (activeCourseId.value && activeCourseId.value !== course.id) return;
  selectedCourseId.value = course.id;
  if (!activeCourseId.value && snapshot.value?.lab?.courseId !== course.id) snapshot.value = null;
}

function handleVisibilityChange() {
  if (document.hidden) clearPoll();
  else if (snapshot.value) loadSnapshot({silent: true});
}

watch(view, schedulePoll);

onMounted(() => {
  clockTimer = window.setInterval(() => { now.value = Date.now(); }, 1000);
  document.addEventListener('visibilitychange', handleVisibilityChange);
  restoreSession();
});

onBeforeUnmount(() => {
  clearPoll();
  window.clearInterval(clockTimer);
  document.removeEventListener('visibilitychange', handleVisibilityChange);
});
</script>

<template>
  <div v-if="bootstrapping" class="loading-screen">
    <LoaderCircle class="spin" :size="28" />
    <span>正在恢复会话</span>
  </div>

  <LoginPanel v-else-if="!user" :busy="authBusy" :error="error" @submit="handleLogin" />

  <div v-else class="app-shell">
    <header class="app-header">
      <div class="brand-lockup">
        <span class="brand-mark"><ServerCog :size="22" /></span>
        <div><strong>互联网后端架构学习平台</strong><small>实验控制台</small></div>
      </div>
      <div class="view-switch" role="tablist" aria-label="视图切换">
        <button :class="{'is-active': view === 'lab'}" type="button" role="tab" @click="view = 'lab'">
          <FlaskConical :size="17" />实验
        </button>
        <button :class="{'is-active': view === 'api'}" type="button" role="tab" @click="view = 'api'">
          <Braces :size="17" />API
        </button>
      </div>
      <div class="user-menu">
        <span><UserRound :size="17" />{{ user.username }}</span>
        <button class="icon-button" type="button" title="退出登录" :disabled="requestBusy" @click="handleLogout">
          <LogOut :size="18" />
        </button>
      </div>
    </header>

    <div v-if="view === 'lab'" class="experiment-layout">
      <CourseRail
        :courses="courses"
        :selected-course-id="selectedCourseId"
        :active-course-id="activeCourseId"
        @select="selectCourse"
      />
      <LabWorkspace
        :course="selectedCourse"
        :snapshot="snapshot"
        :now="now"
        :busy="labBusy"
        :loading="snapshotLoading"
        :error="error"
        :submit-batch="handleTrafficBatch"
        @create="handleCreate"
        @reset="handleReset"
        @terminate="handleTerminate"
        @refresh="loadSnapshot"
        @action="handleLabAction"
      />
    </div>
    <ApiConsole v-else />
  </div>
</template>
