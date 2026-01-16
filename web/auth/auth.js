const logEl = document.getElementById('log');
const accessEl = document.getElementById('access');
const manualAccessEl = document.getElementById('manualAccess');
const manualRefreshEl = document.getElementById('manualRefresh');
const friendCodeEl = document.getElementById('friendCode');
const friendListEl = document.getElementById('friendList');
const userCodeEl = document.getElementById('myUserCode');
const friendRequestListEl = document.getElementById('friendRequestList');
const state = { accessToken: '', userCode: '' };

function updateAccessClip() {
  const current = (manualAccessEl?.value || '').trim() || state.accessToken;
  accessEl.textContent = current ? current.slice(0, 24) + '…' : '';
}

function updateUserCodeDisplay() {
  if (!userCodeEl) return;
  const stored = localStorage.getItem('ququchat_user_code') || '';
  const current = state.userCode || stored;
  userCodeEl.textContent = current ? String(current) : '未登录';
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

function getAccessTokenForAuth() {
  const manual = (manualAccessEl?.value || '').trim();
  return manual || state.accessToken;
}

async function onAddFriend() {
  const token = getAccessTokenForAuth();
  if (!token) {
    appendLog('err', '请先登录，或在输入框中提供访问令牌');
    return;
  }
  const raw = (friendCodeEl?.value || '').trim();
  const code = Number(raw);
  if (!code || code <= 0) {
    appendLog('err', '请输入有效的 user_code（正整数）');
    return;
  }
  const resp = await api('/api/friends/add', {
    headers: { Authorization: `Bearer ${token}` },
    body: { target_user_code: code },
  });
  if (resp.ok) {
    appendLog('ok', '加好友成功', resp.data);
    await onListFriends();
  } else {
    appendLog('err', `加好友失败 (${resp.status})`, resp.data);
  }
}

async function onRemoveFriend() {
  const token = getAccessTokenForAuth();
  if (!token) {
    appendLog('err', '请先登录，或在输入框中提供访问令牌');
    return;
  }
  const raw = (friendCodeEl?.value || '').trim();
  const code = Number(raw);
  if (!code || code <= 0) {
    appendLog('err', '请输入有效的 user_code（正整数）');
    return;
  }
  const resp = await api('/api/friends/remove', {
    headers: { Authorization: `Bearer ${token}` },
    body: { target_user_code: code },
  });
  if (resp.ok) {
    appendLog('ok', '删除好友成功', resp.data);
    await onListFriends();
  } else {
    appendLog('err', `删除好友失败 (${resp.status})`, resp.data);
  }
}

async function onListFriends() {
  const token = getAccessTokenForAuth();
  if (!token) {
    appendLog('err', '请先登录，或在输入框中提供访问令牌');
    return;
  }
  try {
    const res = await fetch('/api/friends/list', {
      method: 'GET',
      headers: {
        Authorization: `Bearer ${token}`,
      },
      credentials: 'same-origin',
    });
    const json = await res.json().catch(() => ({}));
    if (res.ok) {
      appendLog('ok', '获取好友列表成功', json);
      if (friendListEl) {
        friendListEl.innerHTML = '';
        const list = Array.isArray(json.friends) ? json.friends : [];
        if (!list.length) {
          const li = document.createElement('li');
          li.textContent = '暂无好友';
          friendListEl.appendChild(li);
        } else {
          list.forEach((f) => {
            const li = document.createElement('li');
            li.className = 'friend-item';
            li.textContent = `(${f.user_code}) ${f.username} [${f.status}]`;
            friendListEl.appendChild(li);
          });
        }
      }
    } else {
      appendLog('err', `获取好友列表失败 (${res.status})`, json);
    }
  } catch (e) {
    appendLog('err', `网络错误: ${e.message}`);
  }
}

async function onListFriendRequests() {
  const token = getAccessTokenForAuth();
  if (!token) {
    appendLog('err', '请先登录，或在输入框中提供访问令牌');
    return;
  }
  try {
    const res = await fetch('/api/friends/requests/incoming', {
      method: 'GET',
      headers: {
        Authorization: `Bearer ${token}`,
      },
      credentials: 'same-origin',
    });
    const json = await res.json().catch(() => ({}));
    if (res.ok) {
      appendLog('ok', '获取好友请求列表成功', json);
      if (friendRequestListEl) {
        friendRequestListEl.innerHTML = '';
        const list = Array.isArray(json.requests) ? json.requests : [];
        if (!list.length) {
          const li = document.createElement('li');
          li.textContent = '暂无好友请求';
          friendRequestListEl.appendChild(li);
        } else {
          list.forEach((r) => {
            const li = document.createElement('li');
            li.className = 'friend-request-item';
            const basic = `(${r.from_user_code}) ${r.from_username}`;
            const msg = r.message ? `：${r.message}` : '';
            const idShort = r.request_id ? ` [${r.request_id.slice(0, 8)}…]` : '';
            li.textContent = `${basic}${msg}${idShort}`;

            const acceptBtn = document.createElement('button');
            acceptBtn.textContent = '接受';
            acceptBtn.addEventListener('click', async () => {
              await respondFriendRequest(r.request_id, 'accept');
              await onListFriends();
              await onListFriendRequests();
            });

            const rejectBtn = document.createElement('button');
            rejectBtn.textContent = '拒绝';
            rejectBtn.addEventListener('click', async () => {
              await respondFriendRequest(r.request_id, 'reject');
              await onListFriendRequests();
            });

            li.appendChild(document.createTextNode(' '));
            li.appendChild(acceptBtn);
            li.appendChild(document.createTextNode(' '));
            li.appendChild(rejectBtn);
            friendRequestListEl.appendChild(li);
          });
        }
      }
    } else {
      appendLog('err', `获取好友请求列表失败 (${res.status})`, json);
    }
  } catch (e) {
    appendLog('err', `网络错误: ${e.message}`);
  }
}

async function respondFriendRequest(requestId, action) {
  const token = getAccessTokenForAuth();
  if (!token) {
    appendLog('err', '请先登录，或在输入框中提供访问令牌');
    return;
  }
  const resp = await api('/api/friends/requests/respond', {
    headers: { Authorization: `Bearer ${token}` },
    body: { request_id: requestId, action },
  });
  if (resp.ok) {
    appendLog('ok', `${action === 'accept' ? '已接受' : '已拒绝'}好友请求`, resp.data);
  } else {
    appendLog('err', `处理好友请求失败 (${resp.status})`, resp.data);
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
    const user = resp.data && resp.data.user;
    if (user && Object.prototype.hasOwnProperty.call(user, 'user_code')) {
      const userCode = user.user_code;
      state.userCode = userCode;
      localStorage.setItem('ququchat_user_code', String(userCode));
    }
    updateUserCodeDisplay();
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
    const user = resp.data && resp.data.user;
    if (user && Object.prototype.hasOwnProperty.call(user, 'user_code')) {
      const userCode = user.user_code;
      state.userCode = userCode;
      localStorage.setItem('ququchat_user_code', String(userCode));
    }
    updateAccessClip();
    updateUserCodeDisplay();
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
    state.userCode = '';
    localStorage.removeItem('ququchat_user_code');
    updateAccessClip();
    updateUserCodeDisplay();
    appendLog('ok', '已登出当前设备', resp.data);
  } else {
    appendLog('err', `登出失败 (${resp.status})`, resp.data);
  }
}

document.getElementById('btnRegister').addEventListener('click', onRegister);
document.getElementById('btnLogin').addEventListener('click', onLogin);
document.getElementById('btnRefresh').addEventListener('click', onRefresh);
document.getElementById('btnLogout').addEventListener('click', onLogout);
document.getElementById('btnAddFriend').addEventListener('click', onAddFriend);
document.getElementById('btnRemoveFriend').addEventListener('click', onRemoveFriend);
document.getElementById('btnListFriends').addEventListener('click', onListFriends);
document.getElementById('btnRefreshFriendRequests').addEventListener('click', onListFriendRequests);

if (manualAccessEl) {
  manualAccessEl.addEventListener('input', updateAccessClip);
}
updateUserCodeDisplay();
