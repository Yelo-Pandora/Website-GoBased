<script setup>
import {BookOpen, FlaskConical, LoaderCircle, LogIn, LogOut, ServerCog, UserRound} from '@lucide/vue';
import {computed, onBeforeUnmount, onMounted, ref, watch} from 'vue';

import {
  ApiError,
  createLab,
  currentUser,
  getCourse,
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
import CourseTheory from './components/CourseTheory.vue';
import LabWorkspace from './components/LabWorkspace.vue';
import LoginDialog from './components/LoginDialog.vue';
import SiteIntroduction from './components/SiteIntroduction.vue';

const activeStatuses = new Set(['Preparing', 'Running', 'Expiring', 'Terminating']);
const busyOperationStatuses = new Set(['pending', 'claimed', 'running', 'compensating']);
const introductionKey = 'introduction';

const courseMode = ref('course');
const bootstrapping = ref(true);
const authBusy = ref(false);
const requestBusy = ref(false);
const snapshotLoading = ref(false);
const courseDetailLoading = ref(false);
const loginOpen = ref(false);
const loginIntent = ref('header');
const user = ref(null);
const courses = ref([]);
const selectedKey = ref(introductionKey);
const courseDetail = ref(null);
const snapshot = ref(null);
const error = ref(null);
const loginError = ref(null);
const courseDetailError = ref(null);
const now = ref(Date.now());

let pollTimer = null;
let clockTimer = null;
let courseRequestId = 0;

const selectedCourse = computed(() =>
  courses.value.find((course) => course.id === selectedKey.value) || null,
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
  if (!user.value || courseMode.value !== 'lab' || document.hidden || !snapshot.value?.lab?.id) return;
  const status = snapshot.value.lab.status;
  const operationStatus = snapshot.value.latestOperation?.status;
  const balancerStatus = snapshot.value.balancer?.status;
  const fast = ['Preparing', 'Expiring', 'Terminating'].includes(status) ||
    busyOperationStatuses.has(operationStatus) || balancerStatus === 'converging';
  pollTimer = window.setTimeout(() => loadSnapshot({silent: true}), fast ? 1000 : 5000);
}

async function loadCourseCatalog() {
  courses.value = await listCourses();
  if (selectedKey.value !== introductionKey && !selectedCourse.value) {
    selectedKey.value = introductionKey;
  }
}

async function loadCourseDetail(course = selectedCourse.value) {
  if (!course?.slug) {
    courseDetail.value = null;
    return;
  }
  const requestId = ++courseRequestId;
  courseDetailLoading.value = true;
  courseDetailError.value = null;
  try {
    const detail = await getCourse(course.slug);
    if (requestId === courseRequestId) courseDetail.value = detail;
  } catch (value) {
    if (requestId === courseRequestId) {
      courseDetail.value = null;
      courseDetailError.value = presentError(value);
    }
  } finally {
    if (requestId === courseRequestId) courseDetailLoading.value = false;
  }
}

async function restoreLab({activate = true} = {}) {
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
        if (activate) {
          selectedKey.value = restored.lab.courseId;
          courseMode.value = 'lab';
        }
        rememberLab(labId);
        schedulePoll();
        return true;
      }
      terminalSnapshot ||= restored;
    } catch (value) {
      if (!(value instanceof ApiError) || value.code !== 'LAB_NOT_FOUND') throw value;
    }
  }
  snapshot.value = terminalSnapshot;
  if (terminalSnapshot) {
    rememberLab(terminalSnapshot.lab.id);
  } else {
    rememberLab('');
  }
  return false;
}

async function restoreSession() {
  bootstrapping.value = true;
  error.value = null;
  try {
    try {
      const auth = await currentUser();
      user.value = auth.user;
    } catch (value) {
      if (!(value instanceof ApiError) || value.status !== 401) error.value = presentError(value);
      user.value = null;
    }
    await loadCourseCatalog();
    if (user.value) await restoreLab();
    if (selectedCourse.value) await loadCourseDetail();
  } catch (value) {
    error.value = presentError(value);
  } finally {
    bootstrapping.value = false;
  }
}

function openLogin(intent = 'header') {
  loginIntent.value = intent;
  loginError.value = null;
  loginOpen.value = true;
}

function closeLogin() {
  loginOpen.value = false;
  loginError.value = null;
}

async function handleLogin(credentials) {
  authBusy.value = true;
  loginError.value = null;
  const requestedCourseId = loginIntent.value === 'experiment' ? selectedCourse.value?.id : null;
  try {
    const auth = await login(credentials.username, credentials.password);
    user.value = auth.user;
    await loadCourseCatalog();
    const restoredActive = await restoreLab({activate: loginIntent.value === 'experiment'});
    if (loginIntent.value === 'experiment' && !restoredActive && requestedCourseId) {
      selectedKey.value = requestedCourseId;
      if (snapshot.value?.lab?.courseId !== requestedCourseId) snapshot.value = null;
      courseMode.value = 'lab';
    }
    if (selectedCourse.value) await loadCourseDetail();
    loginOpen.value = false;
    loginIntent.value = 'header';
  } catch (value) {
    loginError.value = presentError(value);
  } finally {
    authBusy.value = false;
  }
}

async function enterGuestMode() {
  clearPoll();
  user.value = null;
  snapshot.value = null;
  courseMode.value = 'course';
  loginIntent.value = 'header';
  try {
    await loadCourseCatalog();
    if (selectedCourse.value) await loadCourseDetail();
  } catch (value) {
    courseDetailError.value = presentError(value);
  }
}

async function recoverAuthentication(value) {
  if (!(value instanceof ApiError) || value.status !== 401) return false;
  await enterGuestMode();
  return true;
}

async function handleLogout() {
  requestBusy.value = true;
  error.value = null;
  try {
    await logout();
    await enterGuestMode();
  } catch (value) {
    if (value instanceof ApiError && value.status === 401) {
      await enterGuestMode();
    } else {
      error.value = presentError(value);
    }
  } finally {
    requestBusy.value = false;
  }
}

async function loadSnapshot({silent = false} = {}) {
  const labId = snapshot.value?.lab?.id;
  if (!labId) return;
  if (!silent) snapshotLoading.value = true;
  try {
    snapshot.value = await getLab(labId);
    selectedKey.value = snapshot.value.lab.courseId;
    error.value = null;
  } catch (value) {
    if (!await recoverAuthentication(value)) {
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
    courseMode.value = 'lab';
    schedulePoll();
  } catch (value) {
    if (await recoverAuthentication(value)) return;
    error.value = presentError(value);
    if (value instanceof ApiError && value.code === 'LAB_ALREADY_ACTIVE') {
      await loadCourseCatalog();
      await restoreLab();
      await loadCourseDetail();
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
    if (!await recoverAuthentication(value)) error.value = presentError(value);
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
    if (!await recoverAuthentication(value)) error.value = presentError(value);
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
    if (!await recoverAuthentication(value)) error.value = presentError(value);
  } finally {
    requestBusy.value = false;
  }
}

async function handleTrafficBatch(batch) {
  if (!snapshot.value?.lab?.id) throw new ApiError('实验不存在', {code: 'LAB_NOT_FOUND'});
  try {
    return await submitTrafficBatch(snapshot.value.lab.id, batch);
  } catch (value) {
    await recoverAuthentication(value);
    throw value;
  }
}

function selectCourse(course) {
  if (activeCourseId.value && activeCourseId.value !== course.id) return;
  selectedKey.value = course.id;
  courseMode.value = 'course';
  clearPoll();
  if (!activeCourseId.value && snapshot.value?.lab?.courseId !== course.id) snapshot.value = null;
  loadCourseDetail(course);
}

function selectIntroduction() {
  selectedKey.value = introductionKey;
  courseMode.value = 'course';
  courseDetail.value = null;
  courseDetailError.value = null;
  clearPoll();
}

function switchCourseMode(mode) {
  if (mode === 'lab' && !user.value) {
    openLogin('experiment');
    return;
  }
  if (mode === courseMode.value) return;
  courseMode.value = mode;
  if (mode === 'lab' && snapshot.value?.lab?.id) {
    loadSnapshot({silent: true});
  }
}

function handleVisibilityChange() {
  if (document.hidden) clearPoll();
  else if (courseMode.value === 'lab' && snapshot.value) loadSnapshot({silent: true});
}

watch(courseMode, schedulePoll);

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

  <div v-else class="app-shell">
    <header class="app-header">
      <div class="brand-lockup">
        <span class="brand-mark"><ServerCog :size="22" /></span>
        <div><strong>互联网后端架构学习平台</strong><small>理论课程与实验平台</small></div>
      </div>
      <div class="user-menu">
        <template v-if="user">
          <span><UserRound :size="17" />{{ user.username }}</span>
          <button class="icon-button" type="button" title="退出登录" :disabled="requestBusy" @click="handleLogout">
            <LogOut :size="18" />
          </button>
        </template>
        <button v-else class="button button--primary" type="button" @click="openLogin('header')">
          <LogIn :size="17" />登录实验
        </button>
      </div>
    </header>

    <div class="experiment-layout">
      <CourseRail
        :courses="courses"
        :selected-key="selectedKey"
        :active-course-id="activeCourseId"
        @select="selectCourse"
        @select-introduction="selectIntroduction"
      />
      <div class="course-area">
        <SiteIntroduction v-if="selectedKey === introductionKey" />
        <template v-else>
          <div
            v-if="selectedCourse?.labAvailable"
            class="course-mode-switch"
            role="tablist"
            aria-label="课程内容切换"
          >
            <button
              :class="{'is-active': courseMode === 'course'}"
              :aria-selected="courseMode === 'course'"
              type="button"
              role="tab"
              @click="switchCourseMode('course')"
            >
              <BookOpen :size="17" />课程
            </button>
            <button
              :class="{'is-active': courseMode === 'lab'}"
              :aria-selected="courseMode === 'lab'"
              type="button"
              role="tab"
              @click="switchCourseMode('lab')"
            >
              <FlaskConical :size="17" />实验
            </button>
          </div>
          <CourseTheory
            v-if="courseMode === 'course' || !selectedCourse?.labAvailable"
            :course="selectedCourse"
            :detail="courseDetail"
            :loading="courseDetailLoading"
            :error="courseDetailError"
            @retry="loadCourseDetail()"
          />
          <LabWorkspace
            v-else
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
        </template>
      </div>
    </div>
  </div>

  <LoginDialog
    :open="loginOpen"
    :busy="authBusy"
    :error="loginError"
    @close="closeLogin"
    @submit="handleLogin"
  />
</template>
