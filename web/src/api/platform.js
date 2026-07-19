let csrfToken = '';

export class ApiError extends Error {
  constructor(message, {status = 0, code = 'NETWORK_ERROR', requestId = ''} = {}) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.requestId = requestId;
  }
}

async function request(path, options = {}) {
  const headers = new Headers(options.headers || {});
  if (options.body !== undefined) {
    headers.set('Content-Type', 'application/json');
  }
  if (options.csrf) {
    headers.set('X-CSRF-Token', csrfToken);
  }

  let response;
  try {
    response = await fetch(path, {
      credentials: 'same-origin',
      ...options,
      headers,
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
    });
  } catch (error) {
    throw new ApiError(error instanceof Error ? error.message : '网络连接失败');
  }

  let payload = null;
  const contentType = response.headers.get('Content-Type') || '';
  if (contentType.includes('application/json')) {
    payload = await response.json();
  }
  if (!response.ok) {
    const detail = payload?.error;
    throw new ApiError(detail?.message || `请求失败 (${response.status})`, {
      status: response.status,
      code: detail?.code || 'REQUEST_FAILED',
      requestId: payload?.requestId || response.headers.get('X-Request-ID') || '',
    });
  }
  return payload;
}

export function setCSRFToken(value) {
  csrfToken = value || '';
}

export function newOperationId(prefix) {
  const random = crypto.randomUUID?.() || `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return `${prefix}-${random}`.slice(0, 64);
}

export async function login(username, password) {
  const payload = await request('/api/v1/auth/login', {
    method: 'POST',
    body: {username, password},
  });
  setCSRFToken(payload.data.csrfToken);
  return payload.data;
}

export async function currentUser() {
  const payload = await request('/api/v1/auth/me');
  setCSRFToken(payload.data.csrfToken);
  return payload.data;
}

export async function logout() {
  const payload = await request('/api/v1/auth/logout', {method: 'POST', csrf: true});
  setCSRFToken('');
  return payload.data;
}

export async function listCourses() {
  const payload = await request('/api/v1/courses');
  return payload.data.courses;
}

export async function getCourse(slug) {
  const payload = await request(`/api/v1/courses/${encodeURIComponent(slug)}`);
  return payload.data.course;
}

export async function createLab(courseId) {
  const payload = await request('/api/v1/labs', {
    method: 'POST',
    csrf: true,
    body: {courseId, operationId: newOperationId('create')},
  });
  return payload.data;
}

export async function getLab(labId) {
  const payload = await request(`/api/v1/labs/${encodeURIComponent(labId)}`);
  return payload.data;
}

export async function resetLab(labId) {
  const payload = await request(`/api/v1/labs/${encodeURIComponent(labId)}/reset`, {
    method: 'POST',
    csrf: true,
    body: {operationId: newOperationId('reset')},
  });
  return payload.data;
}

export async function terminateLab(labId) {
  const payload = await request(`/api/v1/labs/${encodeURIComponent(labId)}`, {
    method: 'DELETE',
    csrf: true,
    body: {operationId: newOperationId('terminate')},
  });
  return payload.data;
}

export async function submitLabAction(labId, action) {
  const payload = await request(`/api/v1/labs/${encodeURIComponent(labId)}/actions`, {
    method: 'POST',
    csrf: true,
    body: {
      operationId: newOperationId(action.actionType.toLowerCase()),
      actionType: action.actionType,
      targetInstanceId: action.targetInstanceId ?? null,
      parameters: action.parameters || {},
    },
  });
  return payload.data;
}

export async function submitTrafficBatch(labId, batch) {
  const payload = await request(`/api/v1/labs/${encodeURIComponent(labId)}/traffic-batches`, {
    method: 'POST',
    csrf: true,
    body: batch,
  });
  return payload.data.result;
}
