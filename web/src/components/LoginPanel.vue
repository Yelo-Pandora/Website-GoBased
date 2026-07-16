<script setup>
import {LogIn, ServerCog, ShieldCheck} from '@lucide/vue';
import {ref} from 'vue';

defineProps({busy: Boolean, error: Object});
const emit = defineEmits(['submit']);

const username = ref('');
const password = ref('');

function submit() {
  emit('submit', {username: username.value.trim(), password: password.value});
}
</script>

<template>
  <main class="login-shell">
    <section class="login-panel" aria-labelledby="login-title">
      <div class="login-brand" aria-hidden="true">
        <ServerCog :size="26" />
      </div>
      <p class="eyebrow">互联网后端架构学习平台</p>
      <h1 id="login-title">登录实验环境</h1>
      <form class="login-form" @submit.prevent="submit">
        <label>
          <span>账号</span>
          <input v-model="username" name="username" autocomplete="username" required />
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
  </main>
</template>
