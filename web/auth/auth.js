const logEl = document.getElementById('log');
const accessEl = document.getElementById('access');
const manualAccessEl = document.getElementById('manualAccess');
const manualRefreshEl = document.getElementById('manualRefresh');
const state = { accessToken: '' };

function updateAccessClip() {
  const current = (manualAccessEl?.value || '').trim() || state.accessToken;
  accessEl.textContent = current ? current.slice(0, 24) + '…' : '';
}

function appendLog(type, msg, data) {
  const time = new Date().toLocaleTimeString();
  const prefix = type === 'ok' ? '[OK]' : type === 'err' ? '[ERR]' : '[INFO]';
  const line = `${time} ${prefix} ${msg}`;
  const detail = data ? `\n${JSON.stringify(data, null, 2)}` : '';
  logEl.textContent = `${line}${detail}\n\n${logEl.textContent}`;
}

async function api(path, options = {}) {
  try {
    const res = await fetch(path, {
      method: options.method || 'POST',
      headers: {
        'Content-Type': 'application/json',
        ...(options.headers || {}),
      },
      body: options.body ? JSON.stringify(options.body) : undefined,
      // 同源请求默认会携带 Cookie；显式声明更清晰
      credentials: 'same-origin',
    });
    const json = await res.json().catch(() => ({}));
    return { status: res.status, ok: res.ok, data: json };
  } catch (e) {
    appendLog('err', `网络错误: ${e.message}`);
    return { status: 0, ok: false, data: null };
  }
}

async function onRegister() {
  const username = document.getElementById('username').value.trim();
  const password = document.getElementById('password').value;
  if (!username || password.length < 6) {
    appendLog('err', '用户名不能为空，密码至少 6 位');
    return;
  }
  const resp = await api('/api/auth/register', { body: { username, password } });
  if (resp.ok) {
    appendLog('ok', '注册成功', resp.data);
  } else {
    appendLog('err', `注册失败 (${resp.status})`, resp.data);
  }
}

async function onLogin() {
  const username = document.getElementById('username').value.trim();
  const password = document.getElementById('password').value;
  if (!username || !password) {
    appendLog('err', '用户名和密码不能为空');
    return;
  }
  const resp = await api('/api/auth/login', { body: { username, password } });
  if (resp.ok) {
    state.accessToken = resp.data?.accessToken || '';
    if (manualAccessEl) manualAccessEl.value = state.accessToken;
    if (manualRefreshEl) manualRefreshEl.value = resp.data?.refreshToken || '';
    updateAccessClip();
    appendLog('ok', '登录成功（刷新令牌已写入 Cookie）', resp.data);
  } else {
    appendLog('err', `登录失败 (${resp.status})`, resp.data);
  }
}

async function onRefresh() {
  const manualRefresh = (manualRefreshEl?.value || '').trim();
  const resp = await api('/api/auth/refresh', {
    body: manualRefresh ? { refresh_token: manualRefresh } : undefined,
  });
  if (resp.ok) {
    state.accessToken = resp.data?.accessToken || '';
    if (manualAccessEl) manualAccessEl.value = state.accessToken;
    if (manualRefreshEl) manualRefreshEl.value = resp.data?.refreshToken || manualRefresh || '';
    updateAccessClip();
    appendLog('ok', '访问令牌已刷新（刷新令牌已轮换并写入 Cookie）', resp.data);
  } else {
    appendLog('err', `刷新失败 (${resp.status})`, resp.data);
  }
}

async function onLogout() {
  const manualAccess = (manualAccessEl?.value || '').trim();
  const tokenToUse = manualAccess || state.accessToken;
  if (!tokenToUse) {
    appendLog('err', '请先登录或在输入框中提供访问令牌');
    return;
  }
  const manualRefresh = (manualRefreshEl?.value || '').trim();
  const resp = await api('/api/auth/logout', {
    headers: { Authorization: `Bearer ${tokenToUse}` },
    body: manualRefresh ? { refresh_token: manualRefresh } : undefined,
    // 留空 body 时，后端会从 Cookie 读取 refresh_token
  });
  if (resp.ok) {
    state.accessToken = '';
    updateAccessClip();
    appendLog('ok', '已登出当前设备', resp.data);
  } else {
    appendLog('err', `登出失败 (${resp.status})`, resp.data);
  }
}

document.getElementById('btnRegister').addEventListener('click', onRegister);
document.getElementById('btnLogin').addEventListener('click', onLogin);
document.getElementById('btnRefresh').addEventListener('click', onRefresh);
document.getElementById('btnLogout').addEventListener('click', onLogout);

if (manualAccessEl) {
  manualAccessEl.addEventListener('input', updateAccessClip);
}