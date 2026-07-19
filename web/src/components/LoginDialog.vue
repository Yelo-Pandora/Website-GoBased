<script setup>
import {LogIn, ServerCog, ShieldCheck, X} from '@lucide/vue';
import {nextTick, onBeforeUnmount, onMounted, ref, watch} from 'vue';

const props = defineProps({
  open: Boolean,
  busy: Boolean,
  error: Object,
});

const emit = defineEmits(['close', 'submit']);

const username = ref('');
const password = ref('');
const dialog = ref(null);
const usernameInput = ref(null);
let previouslyFocusedElement = null;

function close() {
  if (props.busy) return;
  emit('close');
}

function submit() {
  emit('submit', {username: username.value.trim(), password: password.value});
}

function handleKeydown(event) {
  if (!props.open) return;
  if (event.key === 'Escape') {
    close();
    return;
  }
  if (event.key !== 'Tab') return;
  const focusableElements = [...dialog.value.querySelectorAll(
    'button:not(:disabled), input:not(:disabled)',
  )];
  if (!focusableElements.length) return;
  const firstElement = focusableElements[0];
  const lastElement = focusableElements.at(-1);
  if (event.shiftKey && document.activeElement === firstElement) {
    event.preventDefault();
    lastElement.focus();
  } else if (!event.shiftKey && document.activeElement === lastElement) {
    event.preventDefault();
    firstElement.focus();
  }
}

watch(() => props.open, async (open) => {
  document.body.classList.toggle('dialog-open', open);
  if (open) {
    previouslyFocusedElement = document.activeElement;
    await nextTick();
    usernameInput.value?.focus();
  } else {
    password.value = '';
    await nextTick();
    previouslyFocusedElement?.focus();
    previouslyFocusedElement = null;
  }
});

onMounted(() => document.addEventListener('keydown', handleKeydown));

onBeforeUnmount(() => {
  document.body.classList.remove('dialog-open');
  document.removeEventListener('keydown', handleKeydown);
});
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="login-dialog-backdrop" @click.self="close">
      <section
        ref="dialog"
        class="login-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="login-dialog-title"
        aria-describedby="login-dialog-description"
      >
        <button
          class="icon-button login-dialog__close"
          type="button"
          title="关闭登录"
          aria-label="关闭登录"
          :disabled="busy"
          @click="close"
        >
          <X :size="18" />
        </button>
        <div class="login-brand" aria-hidden="true">
          <ServerCog :size="26" />
        </div>
        <p class="eyebrow">互联网后端架构学习平台</p>
        <h1 id="login-dialog-title">登录实验环境</h1>
        <p id="login-dialog-description" class="login-dialog__intro">
          理论课程可直接阅读，登录后即可创建和操作实验资源。
        </p>
        <form class="login-form" @submit.prevent="submit">
          <label>
            <span>账号</span>
            <input
              ref="usernameInput"
              v-model="username"
              name="username"
              autocomplete="username"
              required
            />
          </label>
          <label>
            <span>密码</span>
            <input
              v-model="password"
              name="password"
              type="password"
              autocomplete="current-password"
              required
            />
          </label>
          <p v-if="error" class="form-error" role="alert">
            <strong>{{ error.code }}</strong>
            {{ error.message }}
          </p>
          <button class="button button--primary login-submit" type="submit" :disabled="busy">
            <LogIn :size="18" />
            {{ busy ? '正在登录' : '登录' }}
          </button>
        </form>
        <div class="login-security">
          <ShieldCheck :size="16" />
          <span>Session Cookie · CSRF Protection</span>
        </div>
      </section>
    </div>
  </Teleport>
</template>
