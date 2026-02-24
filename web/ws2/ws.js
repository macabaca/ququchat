;(function () {
var state = {
    accessToken: null,
    refreshToken: null,
    user: null,
    friends: [],
    groups: [],
    currentFriend: null,
    currentGroup: null,
    currentGroupMembers: [],
    messages: [],
    ws: null,
    wsConnected: false,
    currentRoomId: null, // Track current room ID for DB queries
    chunkSession: null,
    heartbeatTimer: null,
    lastHeartbeatAt: 0
  }
  var SESSION_KEY = "ququchat_ws2_session"

  function syncMessages(roomId) {
      // 1. Get local max sequence_id
      return ChatDB.getLastSequenceId(roomId)
          .then(function(lastSeqId) {
              console.log("Syncing room " + roomId + " from sequence_id: " + lastSeqId);
              
              // 2. Fetch from API
              return fetchWithRefresh(apiBase() + "/messages/history/after?room_id=" + roomId + "&after_sequence_id=" + lastSeqId, {
                  headers: authHeaders()
              });
          })
          .then(function(res) {
              return res.json();
          })
          .then(function(data) {
              if (data.messages && data.messages.length > 0) {
                  // 3. Save to DB
                  return ChatDB.bulkSaveMessages(data.messages).then(function() {
                      return data.messages.length;
                  });
              }
              return 0;
          })
          .catch(function(err) {
              console.error("Sync failed:", err);
          });
  }

  async function loadAndRenderMessages(roomId) {
      state.messages = [];
      $("chat-box").innerHTML = "";
      
      // Query DB by room_id, order by sequence_id
      var messages = await ChatDB.getMessages(roomId);
      appendHistoryMessages(messages);
  }

function $(id) {
return document.getElementById(id)
}

function setStatus(el, text, type) {
el.textContent = text || ""
el.className = "status" + (type ? " " + type : "")
}

function apiBase() {
return window.location.origin + "/api"
}

function refreshTokens() {
if (!state.refreshToken) {
return Promise.resolve(false)
}
return fetch(apiBase() + "/auth/refresh", {
method: "POST",
headers: {
"Content-Type": "application/json"
},
body: JSON.stringify({
refresh_token: state.refreshToken
})
})
.then(function (res) {
if (!res.ok) {
return false
}
return res.json().then(function (data) {
if (!data.accessToken || !data.refreshToken) {
return false
}
state.accessToken = data.accessToken
state.refreshToken = data.refreshToken
saveSessionState()
return true
})
})
.catch(function () {
return false
})
}

function fetchWithRefresh(url, options) {
options = options || {}
return fetch(url, options).then(function (res) {
if (res.status !== 401) {
return res
}
if (!state.refreshToken) {
clearSessionState()
setStatus($("login-status"), "登录已过期，请重新登录", "error")
return res
}
return refreshTokens().then(function (ok) {
if (!ok) {
clearSessionState()
setStatus($("login-status"), "登录已过期，请重新登录", "error")
return res
}
var newOptions = {}
for (var k in options) {
if (Object.prototype.hasOwnProperty.call(options, k)) {
newOptions[k] = options[k]
}
}
newOptions.headers = newOptions.headers || {}
var auth = authHeaders()
for (var h in auth) {
if (Object.prototype.hasOwnProperty.call(auth, h)) {
newOptions.headers[h] = auth[h]
}
}
return fetch(url, newOptions)
})
})
}

function login() {
var username = $("login-username").value.trim()
var password = $("login-password").value
if (!username || !password) {
setStatus($("login-status"), "请输入用户名和密码", "error")
return
}
setStatus($("login-status"), "登录中...")
fetch(apiBase() + "/auth/login", {
method: "POST",
headers: {
"Content-Type": "application/json"
},
body: JSON.stringify({
username: username,
password: password
})
})
.then(function (res) {
if (!res.ok) {
return res.json().catch(function () {
return {}
}).then(function (data) {
throw new Error(data.error || "登录失败")
})
}
return res.json()
})
.then(function (data) {
state.accessToken = data.accessToken
state.refreshToken = data.refreshToken || null
state.user = data.user
saveSessionState()
setStatus($("login-status"), "登录成功，user_id=" + data.user.id, "ok")
updateCurrentUser()
      refreshFriends()
      refreshGroups()
    })
    .catch(function (err) {
setStatus($("login-status"), err.message, "error")
})
}

function authHeaders() {
if (!state.accessToken) return {}
return {
Authorization: "Bearer " + state.accessToken
}
}

function saveSessionState() {
if (!state.accessToken || !state.user) {
try {
localStorage.removeItem(SESSION_KEY)
} catch (e) {
}
return
}
try {
localStorage.setItem(SESSION_KEY, JSON.stringify({
accessToken: state.accessToken,
refreshToken: state.refreshToken,
user: state.user
}))
} catch (e) {
}
}

function loadSessionState() {
var raw = null
try {
raw = localStorage.getItem(SESSION_KEY)
} catch (e) {
return false
}
if (!raw) return false
var data = null
try {
data = JSON.parse(raw)
} catch (e) {
return false
}
if (!data || !data.accessToken || !data.user) {
return false
}
state.accessToken = data.accessToken
state.refreshToken = data.refreshToken || null
state.user = data.user
updateCurrentUser()
updateCurrentTarget()
setStatus($("login-status"), "已恢复登录", "ok")
refreshFriends()
refreshGroups()
return true
}

function addFriend() {
if (!state.accessToken) {
setStatus($("friend-status"), "请先登录", "error")
return
}
var codeStr = $("friend-code-input").value.trim()
var code = parseInt(codeStr, 10)
if (!codeStr || isNaN(code) || code <= 0) {
setStatus($("friend-status"), "请输入有效的 user_code", "error")
return
}
setStatus($("friend-status"), "发送好友请求中...")
fetchWithRefresh(apiBase() + "/friends/add", {
method: "POST",
headers: Object.assign(
{
"Content-Type": "application/json"
},
authHeaders()
),
body: JSON.stringify({ target_user_code: code })
})
.then(function (res) {
return res.json().then(function (data) {
return { ok: res.ok, data: data }
})
})
.then(function (res) {
if (!res.ok) {
setStatus($("friend-status"), res.data.error || "发送失败", "error")
return
}
setStatus($("friend-status"), res.data.message || "好友请求已发送", "ok")
})
.catch(function (err) {
setStatus($("friend-status"), err.message, "error")
})
}

function refreshFriends() {
if (!state.accessToken) {
setStatus($("friend-status"), "请先登录", "error")
return
}
setStatus($("friend-status"), "加载好友列表中...")
fetchWithRefresh(apiBase() + "/friends/list", {
headers: authHeaders()
})
.then(function (res) {
return res.json().then(function (data) {
return { ok: res.ok, data: data }
})
})
.then(function (res) {
if (!res.ok) {
setStatus($("friend-status"), res.data.error || "加载失败", "error")
return
}
state.friends = res.data.friends || []
renderFriendList()
setStatus($("friend-status"), "好友数量：" + state.friends.length, "ok")
})
.catch(function (err) {
setStatus($("friend-status"), err.message, "error")
})
}

function renderFriendList() {
var ul = $("friend-list")
ul.innerHTML = ""
state.friends.forEach(function (f) {
var li = document.createElement("li")
if (state.currentFriend && state.currentFriend.id === f.id) {
li.classList.add("active")
}
var left = document.createElement("span")
left.textContent = f.username
var meta = document.createElement("span")
meta.className = "meta"
meta.textContent = "id=" + f.id + " code=" + f.user_code
var right = document.createElement("span")
right.appendChild(meta)
li.appendChild(left)
li.appendChild(right)
li.onclick = function () {
state.currentFriend = f
      state.currentGroup = null
      $("group-detail-section").style.display = "none"
      
      var input = $("chat-input")
      input.disabled = false
      input.placeholder = "输入消息..."
      
      renderFriendList()
renderGroupList() // update active state
updateCurrentTarget()
state.messages = []
$("chat-box").innerHTML = ""
loadLatestHistory()
}
ul.appendChild(li)
  })
}

function createGroup() {
  if (!state.accessToken) {
    setStatus($("group-status"), "请先登录", "error")
    return
  }
  var name = $("group-name-input").value.trim()
  if (!name) {
    setStatus($("group-status"), "请输入群名称", "error")
    return
  }
  setStatus($("group-status"), "创建群聊中...")
  fetchWithRefresh(apiBase() + "/groups/create", {
    method: "POST",
    headers: Object.assign(
      {
        "Content-Type": "application/json"
      },
      authHeaders()
    ),
    body: JSON.stringify({ name: name })
  })
    .then(function (res) {
      return res.json().then(function (data) {
        return { ok: res.ok, data: data }
      })
    })
    .then(function (res) {
      if (!res.ok) {
        setStatus($("group-status"), res.data.error || "创建失败", "error")
        return
      }
      setStatus($("group-status"), "群聊创建成功", "ok")
      $("group-name-input").value = ""
      refreshGroups()
    })
    .catch(function (err) {
      setStatus($("group-status"), err.message, "error")
    })
}

function refreshGroups() {
  if (!state.accessToken) {
    setStatus($("group-status"), "请先登录", "error")
    return
  }
  setStatus($("group-status"), "加载群聊列表中...")
  fetchWithRefresh(apiBase() + "/groups/my", {
    headers: authHeaders()
  })
    .then(function (res) {
      return res.json().then(function (data) {
        return { ok: res.ok, data: data }
      })
    })
    .then(function (res) {
      if (!res.ok) {
        setStatus($("group-status"), res.data.error || "加载失败", "error")
        return
      }
      state.groups = res.data.groups || []
      renderGroupList()
      setStatus($("group-status"), "群聊数量：" + state.groups.length, "ok")
    })
    .catch(function (err) {
      setStatus($("group-status"), err.message, "error")
    })
}

function renderGroupList() {
  var ul = $("group-list")
  ul.innerHTML = ""
  state.groups.forEach(function (g) {
    var li = document.createElement("li")
    if (state.currentGroup && state.currentGroup.id === g.id) {
      li.classList.add("active")
    }
    
    var statusText = ""
    if (g.status === "dismissed") {
        statusText = " [已解散]"
        li.style.color = "#999"
    } else if (g.status === "left") {
        statusText = " [已退群]"
        li.style.color = "#999"
    }

    var left = document.createElement("span")
    left.textContent = g.name + statusText
    var meta = document.createElement("span")
    meta.className = "meta"
    meta.textContent = "id=" + g.id + " (" + g.member_count + "人)"
    var right = document.createElement("span")
    right.appendChild(meta)
    li.appendChild(left)
    li.appendChild(right)
    li.onclick = function () {
      state.currentGroup = g
      state.currentFriend = null
      $("group-detail-section").style.display = "block"
      $("detail-group-name").textContent = g.name + " (ID: " + g.id + ")"
      
      var input = $("chat-input")
      if (g.status !== "active") {
        input.disabled = true
        input.placeholder = "无法发送消息 (" + (g.status === "dismissed" ? "群已解散" : "已退群") + ")"
      } else {
        input.disabled = false
        input.placeholder = "输入消息..."
      }

      // Try to fetch members, but load history regardless of success
      fetchGroupMembers(g.id).catch(function() {
        // If failed (e.g. 403 because left), clear member list
        state.currentGroupMembers = []
        renderGroupMembers([])
        setStatus($("group-detail-status"), "无法加载成员列表 (非成员)", "error")
      }).finally(function() {
        loadGroupHistory()
      })
      
      renderFriendList() // update active state
      renderGroupList()
      updateCurrentTarget()
      state.messages = []
      $("chat-box").innerHTML = ""
      appendChatLine("system", "已切换到群聊: " + g.name + statusText, false)
    }
    ul.appendChild(li)
  })
}

function updateCurrentUser() {
var el = $("current-user")
if (!state.user) {
el.textContent = ""
return
}
el.textContent = "当前用户: " + state.user.username + " (id=" + state.user.id + ", code=" + state.user.user_code + ")"
}

function updateCurrentTarget() {
var el = $("current-target")
if (state.currentFriend) {
el.textContent = "当前聊天对象: [好友] " + state.currentFriend.username + " (id=" + state.currentFriend.id + ")"
} else if (state.currentGroup) {
el.textContent = "当前聊天对象: [群组] " + state.currentGroup.name + " (id=" + state.currentGroup.id + ")"
} else {
el.textContent = "当前聊天对象: 未选择"
}
var fileInput = $("file-input")
var uploadButton = $("upload-file-button")
var chunkInput = $("chunk-file-input")
var chunkButton = $("chunk-upload-button")
var chunkSizeInput = $("chunk-size-input")
var canSendFile = false
if (state.currentGroup) {
canSendFile = state.currentGroup.status === "active"
} else if (state.currentFriend) {
canSendFile = true
}
if (fileInput) fileInput.disabled = !canSendFile
if (uploadButton) uploadButton.disabled = !canSendFile
if (chunkInput) chunkInput.disabled = !canSendFile
if (chunkButton) chunkButton.disabled = !canSendFile
if (chunkSizeInput) chunkSizeInput.disabled = !canSendFile
}

function buildWsUrl() {
var loc = window.location
var protocol = loc.protocol === "https:" ? "wss:" : "ws:"
var host = loc.host
var token = state.accessToken || ""
return protocol + "//" + host + "/ws?token=" + encodeURIComponent(token)
}

function logout() {
if (!state.accessToken && !state.user) {
clearSessionState()
setStatus($("login-status"), "已退出登录", "ok")
return
}
var payload = {}
if (state.refreshToken) {
	payload.refresh_token = state.refreshToken
}
fetchWithRefresh(apiBase() + "/auth/logout", {
method: "POST",
headers: Object.assign(
{
"Content-Type": "application/json"
},
authHeaders()
),
body: JSON.stringify(payload)
})
.then(function () {
})
.catch(function () {
})
.finally(function () {
clearSessionState()
setStatus($("login-status"), "已退出登录", "ok")
})
}

function clearSessionState() {
if (state.ws) {
try {
state.ws.close()
} catch (e) {
}
}
stopHeartbeat()
try {
localStorage.removeItem(SESSION_KEY)
} catch (e) {
}
state.ws = null
state.wsConnected = false
state.accessToken = null
state.refreshToken = null
state.user = null
state.friends = []
  state.groups = []
  state.currentFriend = null
  state.currentGroup = null
  state.messages = []
  $("friend-list").innerHTML = ""
  $("group-list").innerHTML = ""
  $("chat-box").innerHTML = ""
  if ($("file-input")) $("file-input").value = ""
  updateCurrentUser()
updateCurrentTarget()
setWsStatus("", null)
setStatus($("friend-status"), "", null)
setStatus($("file-upload-status"), "", null)
}

function connectWs() {
if (!state.accessToken || !state.user) {
setWsStatus("请先登录", "error")
return
}
if (state.ws && state.wsConnected) {
setWsStatus("已连接", "ok")
return
}
var url = buildWsUrl()
setWsStatus("连接中...")
var ws = new WebSocket(url)
state.ws = ws
ws.onopen = function () {
state.wsConnected = true
setWsStatus("已连接", "ok")
startHeartbeat()
// On Reconnect, we could try to sync current room
if (state.currentRoomId) {
    syncMessages(state.currentRoomId).then(function(count) {
        if(count > 0) loadAndRenderMessages(state.currentRoomId);
    });
}
}
ws.onclose = function () {
state.wsConnected = false
setWsStatus("已断开", "error")
stopHeartbeat()
}
ws.onerror = function () {
state.wsConnected = false
setWsStatus("连接错误", "error")
stopHeartbeat()
}
ws.onmessage = function (event) {
    var msg = JSON.parse(event.data);
    if (msg.type === "ping") {
        if (state.ws && state.wsConnected) {
            state.ws.send(JSON.stringify({ type: "pong", ts: Date.now() }))
        }
        return
    }
    if (msg.type === "pong") {
        state.lastHeartbeatAt = Date.now()
        return
    }
    
    // Intercept messages to save to DB first (Push)
    if (msg.type === 'group_message' || msg.type === 'friend_message') {
        var dbMsg = {
            id: msg.id || ('temp_' + Date.now()),
            room_id: msg.room_id || (msg.type === 'group_message' ? msg.to_user_id : null),
            sequence_id: msg.sequence_id || 0,
            sender_id: msg.from_user_id,
            content_text: msg.content,
            created_at: msg.timestamp,
            content_type: 'text'
        };
        
        ChatDB.saveMessage(dbMsg).then(function() {
             if (state.currentRoomId && state.currentRoomId === dbMsg.room_id) {
                 handleIncomingWsMessage(event.data); 
             }
        });
    } else if (msg.type === 'file_message') {
        var fileDbMsg = {
            id: msg.id || ('temp_' + Date.now()),
            room_id: msg.room_id || null,
            sequence_id: msg.sequence_id || 0,
            sender_id: msg.from_user_id,
            content_text: (msg.attachment && msg.attachment.file_name) || "",
            created_at: msg.timestamp,
            content_type: 'file',
            attachment_id: msg.attachment_id,
            payload_json: msg.attachment
        };
        
        ChatDB.saveMessage(fileDbMsg).then(function() {
             if (state.currentRoomId && state.currentRoomId === fileDbMsg.room_id) {
                 handleIncomingWsMessage(event.data); 
             }
        });
    } else {
        handleIncomingWsMessage(event.data);
    }
}
}

function startHeartbeat() {
stopHeartbeat()
if (!state.ws || !state.wsConnected) return
state.lastHeartbeatAt = Date.now()
state.heartbeatTimer = setInterval(function () {
if (!state.ws || !state.wsConnected) return
try {
state.ws.send(JSON.stringify({ type: "ping", ts: Date.now() }))
} catch (e) {
}
}, 45000)
}

function stopHeartbeat() {
if (state.heartbeatTimer) {
clearInterval(state.heartbeatTimer)
state.heartbeatTimer = null
}
}

function setWsStatus(text, type) {
var el = $("ws-status")
el.textContent = text || ""
if (type === "error") {
el.style.color = "#d32f2f"
} else if (type === "ok") {
el.style.color = "#388e3c"
} else {
el.style.color = "#555"
}
}

function handleIncomingWsMessage(raw) {
var data
try {
data = JSON.parse(raw)
} catch (e) {
appendChatLine("system", "收到非JSON消息: " + raw, false)
return
}
if (data.type === "friend_message") {
    var isMe = state.user && data.from_user_id === state.user.id
    var prefix = isMe ? "我" : "对方"
    // Only display if current target is this friend
    if (state.currentFriend && (isMe || state.currentFriend.id === data.from_user_id)) {
      appendChatLine(prefix, data.content, isMe, data)
    }
  } else if (data.type === "group_message") {
    var isMe = state.user && data.from_user_id === state.user.id
    // Only display if current target is this group
    if (state.currentGroup && state.currentGroup.id === data.room_id) {
      // Find sender name in group members if possible
      var senderName = "成员" + data.from_user_id.substring(0, 4)
      if (state.currentGroupMembers) {
        var m = state.currentGroupMembers.find(function(mem) { return mem.user_id === data.from_user_id })
        if (m) senderName = m.nickname || m.username || senderName
      }
      var prefix = isMe ? "我" : senderName
      appendChatLine(prefix, data.content, isMe, data)
    }
  } else if (data.type === "file_message") {
    var isMe = state.user && data.from_user_id === state.user.id
    if (state.currentGroup && state.currentGroup.id === data.room_id) {
      var senderName = "成员" + data.from_user_id.substring(0, 4)
      if (state.currentGroupMembers) {
        var gm = state.currentGroupMembers.find(function(mem) { return mem.user_id === data.from_user_id })
        if (gm) senderName = gm.nickname || gm.username || senderName
      }
      var prefix = isMe ? "我" : senderName
      appendChatLine(prefix, data.content || "", isMe, data)
      return
    }
    if (state.currentFriend && (isMe || state.currentFriend.id === data.from_user_id)) {
      var friendPrefix = isMe ? "我" : "对方"
      appendChatLine(friendPrefix, data.content || "", isMe, data)
    }
  } else {
    appendChatLine("system", raw, false)
  }
}

function normalizePayload(payload) {
if (!payload) return null
if (typeof payload === "string") {
    try {
        return JSON.parse(payload)
    } catch (e) {
        return null
    }
}
return payload
}

function getAttachmentId(data) {
if (!data) return ""
if (data.attachment_id) return data.attachment_id
var payload = normalizePayload(data.attachment || data.payload_json)
if (payload && payload.attachment_id) return payload.attachment_id
return ""
}

function getAttachmentName(data, fallback) {
var payload = normalizePayload(data.attachment || data.payload_json)
if (payload && payload.file_name) return payload.file_name
if (data && data.file_name) return data.file_name
return fallback || "未命名文件"
}

function requestDownloadUrl(attachmentId) {
if (!attachmentId) {
appendChatLine("system", "无法下载：缺少附件ID", false)
return
}
setStatus($("file-upload-status"), "获取下载链接中...")
fetchWithRefresh(apiBase() + "/files/" + encodeURIComponent(attachmentId) + "/url", {
headers: authHeaders()
})
.then(function (res) {
return res.json().then(function (data) {
return { ok: res.ok, data: data }
})
})
.then(function (res) {
if (!res.ok) {
setStatus($("file-upload-status"), res.data.error || "获取下载链接失败", "error")
return
}
if (!res.data || !res.data.url) {
setStatus($("file-upload-status"), "下载链接缺失", "error")
return
}
setStatus($("file-upload-status"), "已生成下载链接", "ok")
window.open(res.data.url, "_blank")
})
.catch(function (err) {
setStatus($("file-upload-status"), err.message, "error")
})
}

function appendChatLine(sender, text, isMe, data) {
var box = $("chat-box")
var div = document.createElement("div")
var isFile = data && (data.type === "file_message" || data.content_type === "file")
div.className = "chat-message " + (isMe ? "me" : "other") + (isFile ? " file-message" : "")
var meta = document.createElement("span")
meta.className = "meta"
var timeText = ""
if (data && data.timestamp) {
var d = new Date(data.timestamp * 1000)
timeText = d.toLocaleTimeString()
}
meta.textContent = sender + (timeText ? " " + timeText : "")
var content = document.createElement("span")
content.className = "content"
if (isFile) {
var fileName = getAttachmentName(data, text)
var label = document.createElement("span")
label.className = "file-name"
label.textContent = fileName
var btn = document.createElement("button")
btn.textContent = "下载"
btn.onclick = function () {
requestDownloadUrl(getAttachmentId(data))
}
content.appendChild(label)
content.appendChild(btn)
} else {
content.textContent = text
}
div.appendChild(meta)
div.appendChild(content)
box.appendChild(div)
box.scrollTop = box.scrollHeight
}

function appendHistoryMessages(messages) {
	messages.forEach(function (m) {
		var isMe = state.user && m.sender_id === state.user.id
		var sender = isMe ? "我" : "对方"

		if (state.currentGroup && !isMe) {
			sender = "成员" + m.sender_id.substring(0, 4)
			if (state.currentGroupMembers) {
				var mem = state.currentGroupMembers.find(function(u) { return u.user_id === m.sender_id })
				if (mem) {
					sender = mem.nickname || mem.username || sender
				}
			}
		}

		appendChatLine(sender, m.content_text || "", isMe, {
            timestamp: m.created_at,
            content_type: m.content_type,
            attachment_id: m.attachment_id,
            payload_json: m.payload_json
        })
		state.messages.push({
			id: m.id,
			room_id: m.room_id,
			sender_id: m.sender_id,
			content_text: m.content_text || "",
			created_at: m.created_at,
            content_type: m.content_type,
            attachment_id: m.attachment_id,
            payload_json: m.payload_json
		})
	})
}

function getEarliestMessage() {
if (!state.messages.length) {
return null
}
var earliest = state.messages[0]
for (var i = 1; i < state.messages.length; i++) {
		if (state.messages[i].created_at < earliest.created_at) {
earliest = state.messages[i]
}
}
return earliest
}

function loadGroupHistory() {
	if (!state.accessToken || !state.user || !state.currentGroup) {
		return
	}
    
    var groupId = state.currentGroup.id;
    state.currentRoomId = groupId;

    // 1. Render local data immediately (Instant Load)
    loadAndRenderMessages(groupId).then(function() {
        // 2. Sync remote data (Pull)
        return syncMessages(groupId);
    }).then(function(count) {
        if (count && count > 0) {
            // 3. If new data arrived, re-render
            loadAndRenderMessages(groupId);
        }
    });
}

function loadLatestHistory() {
if (!state.accessToken || !state.user || !state.currentFriend) {
	return
}

// Improved Local-First Strategy for Friends
if (state.currentFriend.room_id) {
    var roomId = state.currentFriend.room_id;
    state.currentRoomId = roomId;
    
    // 1. Render local data immediately
    loadAndRenderMessages(roomId).then(function() {
        // 2. Sync remote data
        return syncMessages(roomId);
    }).then(function(count) {
        if (count && count > 0) {
            // 3. If new data, re-render
            loadAndRenderMessages(roomId);
        }
    });
    return;
}

// Fallback for when room_id is unknown (e.g. first chat)
fetchWithRefresh(apiBase() + "/messages/history/latest?friend_id=" + encodeURIComponent(state.currentFriend.id), {
	headers: authHeaders()
})
.then(function (res) {
return res.json().then(function (data) {
return { ok: res.ok, data: data }
})
})
.then(function (res) {
if (!res.ok) {
appendChatLine("system", res.data.error || "加载历史失败", false)
return
}
var list = res.data.messages || []
if (!list.length) {
    state.messages = [];
    $("chat-box").innerHTML = "";
    return;
}

// Save to DB and then Render
ChatDB.bulkSaveMessages(list).then(function() {
    if (list.length > 0 && list[0].room_id) {
        state.currentRoomId = list[0].room_id;
        // Cache room_id for next time
        if (state.currentFriend) {
            state.currentFriend.room_id = list[0].room_id;
        }
        loadAndRenderMessages(state.currentRoomId);
    } else {
        // Fallback if no room_id (shouldn't happen with new API)
        state.messages = []
        $("chat-box").innerHTML = ""
        appendHistoryMessages(list)
    }
});
})
.catch(function () {
appendChatLine("system", "加载历史失败", false)
})
}

function loadMoreHistory() {
if (!state.accessToken || !state.user || (!state.currentFriend && !state.currentGroup)) {
appendChatLine("system", "请先登录并选择会话", false)
return
}
var earliest = getEarliestMessage()
if (!earliest) {
	if (state.currentGroup) {
		loadGroupHistory()
	} else {
		loadLatestHistory()
	}
	return
}
var roomId = earliest.room_id
if (!roomId && state.currentGroup) {
    roomId = state.currentGroup.id
}
// For DMs, if historical messages missed room_id (old version), we might fail to send room_id, 
// but backend will require it. 
// However, since we just added room_id to state.messages, only newly loaded messages have it.
// We should probably rely on state if earliest.room_id is missing.
// But DMs are tricky because state.currentFriend doesn't have room_id.
// Let's assume message has it because we get it from backend.

fetchWithRefresh(apiBase() + "/messages/history/before?message_id=" + encodeURIComponent(earliest.id) + "&room_id=" + encodeURIComponent(roomId), {
	headers: authHeaders()
})
.then(function (res) {
return res.json().then(function (data) {
return { ok: res.ok, data: data }
})
})
.then(function (res) {
if (!res.ok) {
appendChatLine("system", res.data.error || "加载更多历史失败", false)
return
}
		var list = res.data.messages || []
		if (!list.length) {
appendChatLine("system", "没有更多历史消息了", false)
return
}
		var existing = state.messages.slice()
		var merged = list.concat(existing)
		merged.sort(function (a, b) {
			return a.created_at - b.created_at
		})
		state.messages = []
		$("chat-box").innerHTML = ""
		appendHistoryMessages(merged)
		var box = $("chat-box")
		box.scrollTop = 0
})
.catch(function () {
appendChatLine("system", "加载更多历史失败", false)
})
}

function sendMessage() {
if (!state.ws || !state.wsConnected) {
    setWsStatus("尚未连接 WebSocket", "error")
    return
  }
  if (state.currentGroup) {
    if (state.currentGroup.status !== "active") {
      appendChatLine("system", "无法发送消息: " + (state.currentGroup.status === "dismissed" ? "群已解散" : "已退群"), false)
      return
    }
    var input = $("chat-input")
    var text = input.value.trim()
    if (!text) return
    var payload = {
      type: "group_message",
      room_id: state.currentGroup.id,
      content: text
    }
    state.ws.send(JSON.stringify(payload))
    input.value = ""
    return
  }
  if (!state.currentFriend) {
    appendChatLine("system", "请先在好友列表或群聊列表中选择一个对象", false)
    return
  }
  var input = $("chat-input")
var text = input.value.trim()
if (!text) {
return
}
var payload = {
type: "friend_message",
to_user_id: state.currentFriend.id,
content: text
}
state.ws.send(JSON.stringify(payload))
input.value = ""
}

function getChunkSizeBytes() {
var input = $("chunk-size-input")
var sizeMb = input ? parseInt(input.value, 10) : 5
if (!sizeMb || sizeMb <= 0) {
sizeMb = 5
if (input) input.value = "5"
}
return sizeMb * 1024 * 1024
}

function setChunkProgress(percent, text) {
var bar = $("chunk-upload-progress")
var label = $("chunk-upload-progress-text")
if (bar) {
bar.style.width = Math.max(0, Math.min(100, percent)) + "%"
}
if (label) {
label.textContent = text || ""
}
}

function resetChunkSession() {
state.chunkSession = null
setStatus($("chunk-upload-status"), "", null)
setChunkProgress(0, "")
}

function startChunkSession(file) {
return fetchWithRefresh(apiBase() + "/files/multipart/start", {
method: "POST",
headers: Object.assign({"Content-Type": "application/json"}, authHeaders()),
body: JSON.stringify({
file_name: file.name,
mime_type: file.type || ""
})
})
.then(function (res) {
return res.json().then(function (data) {
return { ok: res.ok, data: data }
})
})
.then(function (res) {
if (!res.ok) {
throw new Error(res.data.error || "初始化分片上传失败")
}
state.chunkSession = {
fileKey: file.name + "|" + file.size + "|" + file.lastModified,
uploadId: res.data.upload_id,
storageKey: res.data.storage_key,
attachmentId: res.data.attachment_id,
fileName: res.data.file_name || file.name,
mimeType: res.data.mime_type || file.type || ""
}
return state.chunkSession
})
}

function listChunkParts(session) {
var url = apiBase() + "/files/multipart/parts?upload_id=" + encodeURIComponent(session.uploadId) + "&storage_key=" + encodeURIComponent(session.storageKey)
return fetchWithRefresh(url, { headers: authHeaders() })
.then(function (res) {
return res.json().then(function (data) {
return { ok: res.ok, data: data }
})
})
.then(function (res) {
if (!res.ok) {
throw new Error(res.data.error || "获取分片列表失败")
}
return res.data.parts || []
})
}

function uploadChunkPart(session, partNumber, blob) {
var form = new FormData()
form.append("upload_id", session.uploadId)
form.append("storage_key", session.storageKey)
form.append("part_number", String(partNumber))
form.append("file", blob, "part-" + partNumber)
return fetchWithRefresh(apiBase() + "/files/multipart/part", {
method: "POST",
headers: authHeaders(),
body: form
})
.then(function (res) {
return res.json().then(function (data) {
return { ok: res.ok, data: data }
})
})
.then(function (res) {
if (!res.ok) {
throw new Error(res.data.error || "上传分片失败")
}
return res.data
})
}

function completeChunkUpload(session, expectedSHA256) {
return fetchWithRefresh(apiBase() + "/files/multipart/complete", {
method: "POST",
headers: Object.assign({"Content-Type": "application/json"}, authHeaders()),
body: JSON.stringify({
upload_id: session.uploadId,
storage_key: session.storageKey,
attachment_id: session.attachmentId,
file_name: session.fileName,
mime_type: session.mimeType,
expected_sha256: expectedSHA256 || ""
})
})
.then(function (res) {
return res.json().then(function (data) {
return { ok: res.ok, data: data }
})
})
.then(function (res) {
if (!res.ok) {
throw new Error(res.data.error || "完成上传失败")
}
return res.data.attachment
})
}

function sendChunkedFileMessage() {
if (!state.accessToken) {
setStatus($("chunk-upload-status"), "请先登录", "error")
return
}
if (!state.ws || !state.wsConnected) {
setStatus($("chunk-upload-status"), "尚未连接 WebSocket", "error")
return
}
if (state.currentGroup && state.currentGroup.status !== "active") {
setStatus($("chunk-upload-status"), "无法发送文件: " + (state.currentGroup.status === "dismissed" ? "群已解散" : "已退群"), "error")
return
}
if (!state.currentFriend && !state.currentGroup) {
setStatus($("chunk-upload-status"), "请先选择会话", "error")
return
}
var input = $("chunk-file-input")
var file = input.files && input.files[0]
if (!file) {
setStatus($("chunk-upload-status"), "请选择文件", "error")
return
}
var fileKey = file.name + "|" + file.size + "|" + file.lastModified
var sessionPromise
if (state.chunkSession && state.chunkSession.fileKey === fileKey) {
sessionPromise = Promise.resolve(state.chunkSession)
} else {
sessionPromise = startChunkSession(file)
}
setStatus($("chunk-upload-status"), "初始化分片上传...")
setChunkProgress(0, "")
sessionPromise
.then(function (session) {
return listChunkParts(session).then(function (parts) {
return { session: session, parts: parts }
})
})
.then(function (ctx) {
var session = ctx.session
var uploaded = {}
ctx.parts.forEach(function (p) {
uploaded[p.PartNumber] = true
})
var chunkSize = getChunkSizeBytes()
var totalParts = Math.ceil(file.size / chunkSize)
setChunkProgress(0, "0%")
var chain = Promise.resolve()
for (var i = 0; i < totalParts; i++) {
var partNumber = i + 1
if (uploaded[partNumber]) {
continue
}
chain = chain.then((function (partNo) {
return function () {
var start = (partNo - 1) * chunkSize
var end = Math.min(file.size, start + chunkSize)
var blob = file.slice(start, end)
var percent = Math.floor((end / file.size) * 100)
setStatus($("chunk-upload-status"), "上传分片 " + partNo + "/" + totalParts + " (" + percent + "%)")
setChunkProgress(percent, percent + "%")
return uploadChunkPart(session, partNo, blob)
}
})(partNumber))
}
return chain.then(function () { return session })
})
.then(function (session) {
setStatus($("chunk-upload-status"), "合并分片中...")
setChunkProgress(100, "合并中...")
return completeChunkUpload(session, "")
})
.then(function (attachment) {
if (!attachment || !attachment.id) {
throw new Error("上传响应缺少附件ID")
}
var payload = {
type: "file_message",
attachment_id: attachment.id
}
if (state.currentGroup) {
payload.room_id = state.currentGroup.id
} else {
payload.to_user_id = state.currentFriend.id
}
state.ws.send(JSON.stringify(payload))
input.value = ""
resetChunkSession()
setStatus($("chunk-upload-status"), "已发送", "ok")
setChunkProgress(100, "完成")
})
.catch(function (err) {
setStatus($("chunk-upload-status"), err.message, "error")
setChunkProgress(0, "")
})
}
function sendFileMessage() {
if (!state.accessToken) {
setStatus($("file-upload-status"), "请先登录", "error")
return
}
if (!state.ws || !state.wsConnected) {
setStatus($("file-upload-status"), "尚未连接 WebSocket", "error")
return
}
if (state.currentGroup && state.currentGroup.status !== "active") {
setStatus($("file-upload-status"), "无法发送文件: " + (state.currentGroup.status === "dismissed" ? "群已解散" : "已退群"), "error")
return
}
if (!state.currentFriend && !state.currentGroup) {
setStatus($("file-upload-status"), "请先选择会话", "error")
return
}
var input = $("file-input")
var file = input.files && input.files[0]
if (!file) {
setStatus($("file-upload-status"), "请选择文件", "error")
return
}
setStatus($("file-upload-status"), "上传中...")
var form = new FormData()
form.append("file", file)
fetchWithRefresh(apiBase() + "/files/upload", {
method: "POST",
headers: authHeaders(),
body: form
})
.then(function (res) {
return res.json().then(function (data) {
return { ok: res.ok, data: data }
})
})
.then(function (res) {
if (!res.ok) {
setStatus($("file-upload-status"), res.data.error || "上传失败", "error")
return
}
var attachment = res.data && res.data.attachment
if (!attachment || !attachment.id) {
setStatus($("file-upload-status"), "上传响应缺少附件ID", "error")
return
}
var payload = {
type: "file_message",
attachment_id: attachment.id
}
if (state.currentGroup) {
payload.room_id = state.currentGroup.id
} else {
payload.to_user_id = state.currentFriend.id
}
state.ws.send(JSON.stringify(payload))
input.value = ""
setStatus($("file-upload-status"), "已发送", "ok")
})
.catch(function (err) {
setStatus($("file-upload-status"), err.message, "error")
})
}

function fetchGroupMembers(groupId) {
    if (!state.accessToken) return Promise.reject("Not logged in")
    setStatus($("group-detail-status"), "加载成员中...")
    return fetchWithRefresh(apiBase() + "/groups/" + groupId + "/members", {
      headers: authHeaders()
    })
      .then(function (res) {
        return res.json().then(function (data) {
          return { ok: res.ok, data: data }
        })
      })
      .then(function (res) {
        if (!res.ok) {
          setStatus($("group-detail-status"), res.data.error || "加载成员失败", "error")
          return
        }
        state.currentGroupMembers = res.data.members || []
        renderGroupMembers(state.currentGroupMembers)
        updateAdminButtonVisibility()
        setStatus($("group-detail-status"), "成员加载完成", "ok")
      })
      .catch(function (err) {
        setStatus($("group-detail-status"), err.message, "error")
      })
  }

  function renderGroupMembers(members) {
    var ul = $("group-member-list")
    ul.innerHTML = ""
    var currentUserId = state.user ? state.user.id : ""
    
    // Find my role
    var myRole = "member"
    var me = members.find(function(m) { return m.user_id === currentUserId })
    if (me) myRole = me.role

    // Show/Hide dismiss button based on role
    var dismissBtn = $("dismiss-group-button")
    if (myRole === "owner") {
      dismissBtn.style.display = "inline-block"
    } else {
      dismissBtn.style.display = "none"
    }

    members.forEach(function (m) {
      var li = document.createElement("li")
      var info = (m.nickname || m.user_id) + " (" + m.role + ")"
      if (m.user_id === currentUserId) {
        info += " [我]"
      }
      
      var left = document.createElement("span")
      left.textContent = info
      
      var right = document.createElement("span")
      
      // Kick button logic: Owner can kick anyone (except self), Admin can kick member
      var canKick = false
      if (m.user_id !== currentUserId) {
        if (myRole === "owner") canKick = true
        if (myRole === "admin" && m.role === "member") canKick = true
      }
      
      if (canKick) {
        var btn = document.createElement("button")
        btn.textContent = "踢出"
        btn.style.marginLeft = "10px"
        btn.style.fontSize = "0.8em"
        btn.onclick = function() {
          kickMember(m.user_id)
        }
        right.appendChild(btn)
      }
      
      li.appendChild(left)
      li.appendChild(right)
      ul.appendChild(li)
    })
  }

  function openInviteModal() {
    if (!state.currentGroup) return
    var modal = $("invite-friend-modal")
    modal.style.display = "block"
    renderInviteList()
  }

  function closeInviteModal() {
    $("invite-friend-modal").style.display = "none"
  }

  function renderInviteList() {
    var container = $("invite-friend-list-container")
    container.innerHTML = ""
    
    // Filter friends who are NOT in currentGroupMembers
    var currentMemberIds = state.currentGroupMembers.map(function(m) { return m.user_id })
    var availableFriends = state.friends.filter(function(f) {
      return currentMemberIds.indexOf(f.id) === -1
    })

    if (availableFriends.length === 0) {
      container.innerHTML = "<div style='padding:10px; color:#666;'>没有可邀请的好友 (都在群里了或没有好友)</div>"
      return
    }

    availableFriends.forEach(function(f) {
      var div = document.createElement("div")
      div.className = "friend-item"
      
      var checkbox = document.createElement("input")
      checkbox.type = "checkbox"
      checkbox.value = f.id
      checkbox.id = "invite-friend-" + f.id
      
      var label = document.createElement("label")
      label.htmlFor = "invite-friend-" + f.id
      label.textContent = f.username + " (ID: " + f.id + ")"
      label.style.cursor = "pointer"
      
      div.appendChild(checkbox)
      div.appendChild(label)
      container.appendChild(div)
    })
  }

  function confirmInvite() {
    if (!state.currentGroup) return
    
    var checkboxes = document.querySelectorAll("#invite-friend-list-container input[type='checkbox']:checked")
    var ids = []
    checkboxes.forEach(function(cb) {
      ids.push(cb.value)
    })
    
    if (ids.length === 0) {
      alert("请至少选择一个好友")
      return
    }

    setStatus($("group-detail-status"), "邀请中...")
    fetchWithRefresh(apiBase() + "/groups/" + state.currentGroup.id + "/members/add", {
      method: "POST",
      headers: Object.assign({ "Content-Type": "application/json" }, authHeaders()),
      body: JSON.stringify({ user_ids: ids })
    })
    .then(function(res) { return res.json().then(function(d) { return {ok: res.ok, data: d} }) })
    .then(function(res) {
      if (!res.ok) {
        setStatus($("group-detail-status"), res.data.error || "邀请失败", "error")
        return
      }
      setStatus($("group-detail-status"), "邀请成功", "ok")
      closeInviteModal()
      fetchGroupMembers(state.currentGroup.id)
    })
    .catch(function(err) { setStatus($("group-detail-status"), err.message, "error") })
  }

  function kickMember(targetUserId) {
    if (!state.currentGroup) return
    if (!confirm("确定要踢出该成员吗？")) return
    
    setStatus($("group-detail-status"), "踢人中...")
    fetchWithRefresh(apiBase() + "/groups/" + state.currentGroup.id + "/members/remove", {
      method: "POST",
      headers: Object.assign({ "Content-Type": "application/json" }, authHeaders()),
      body: JSON.stringify({ user_id: targetUserId })
    })
    .then(function(res) { return res.json().then(function(d) { return {ok: res.ok, data: d} }) })
    .then(function(res) {
      if (!res.ok) {
        setStatus($("group-detail-status"), res.data.error || "踢人失败", "error")
        return
      }
      setStatus($("group-detail-status"), "已踢出", "ok")
      fetchGroupMembers(state.currentGroup.id)
    })
    .catch(function(err) { setStatus($("group-detail-status"), err.message, "error") })
  }

  function leaveGroup() {
    if (!state.currentGroup) return
    if (!confirm("确定要退出群聊吗？")) return
    
    setStatus($("group-detail-status"), "退出中...")
    fetchWithRefresh(apiBase() + "/groups/" + state.currentGroup.id + "/leave", {
      method: "POST",
      headers: authHeaders()
    })
    .then(function(res) { return res.json().then(function(d) { return {ok: res.ok, data: d} }) })
    .then(function(res) {
      if (!res.ok) {
        setStatus($("group-detail-status"), res.data.error || "退出失败", "error")
        return
      }
      setStatus($("group-status"), "已退出群聊", "ok")
      $("group-detail-section").style.display = "none"
      state.currentGroup = null
      updateCurrentTarget()
      refreshGroups()
    })
    .catch(function(err) { setStatus($("group-detail-status"), err.message, "error") })
  }

  function dismissGroup() {
    if (!state.currentGroup) return
    if (!confirm("确定要解散群聊吗？此操作不可逆！")) return
    
    setStatus($("group-detail-status"), "解散中...")
    fetchWithRefresh(apiBase() + "/groups/" + state.currentGroup.id + "/dismiss", {
      method: "POST",
      headers: authHeaders()
    })
    .then(function(res) { return res.json().then(function(d) { return {ok: res.ok, data: d} }) })
    .then(function(res) {
      if (!res.ok) {
        setStatus($("group-detail-status"), res.data.error || "解散失败", "error")
        return
      }
      setStatus($("group-status"), "群聊已解散", "ok")
      $("group-detail-section").style.display = "none"
      state.currentGroup = null
      updateCurrentTarget()
      refreshGroups()
    })
    .catch(function(err) { setStatus($("group-detail-status"), err.message, "error") })
  }

  function updateAdminButtonVisibility() {
    var btn = $("set-admin-button")
    if (!state.currentGroup || !state.user) {
      btn.style.display = "none"
      return
    }
    // Check if I am owner
    if (state.currentGroup.owner_id === state.user.id) {
      btn.style.display = "inline-block"
    } else {
      btn.style.display = "none"
    }
  }

  function openSetAdminModal() {
    if (!state.currentGroupMembers) return
    var modal = $("set-admin-modal")
    var container = $("admin-member-list-container")
    container.innerHTML = ""
    
    // Filter out self
    var candidates = state.currentGroupMembers.filter(function(m) {
      return m.user_id !== state.user.id
    })
    
    if (candidates.length === 0) {
      container.innerHTML = "<div style='padding:10px;color:#999'>没有其他成员可操作</div>"
    } else {
      candidates.forEach(function(m) {
        var div = document.createElement("div")
        div.className = "friend-item"
        
        var checkbox = document.createElement("input")
        checkbox.type = "checkbox"
        checkbox.value = m.user_id
        checkbox.id = "admin-chk-" + m.user_id
        
        var label = document.createElement("label")
        label.htmlFor = "admin-chk-" + m.user_id
        var name = m.nickname || m.username || m.user_id
        label.textContent = name + (m.role === 'admin' ? " (已是管理员)" : "")
        
        div.appendChild(checkbox)
        div.appendChild(label)
        container.appendChild(div)
      })
    }
    modal.style.display = "block"
  }

  function closeAdminModal() {
    $("set-admin-modal").style.display = "none"
  }

  function submitSetAdmins() {
    var checkboxes = document.querySelectorAll("#admin-member-list-container input[type='checkbox']:checked")
    var userIds = []
    checkboxes.forEach(function(cb) {
      userIds.push(cb.value)
    })
    
    if (userIds.length === 0) {
      alert("请至少选择一个成员")
      return
    }
    
    fetchWithRefresh(apiBase() + "/groups/" + state.currentGroup.id + "/admins/add", {
      method: "POST",
      headers: Object.assign({"Content-Type": "application/json"}, authHeaders()),
      body: JSON.stringify({ user_ids: userIds })
    })
    .then(function(res) {
      return res.json().then(function(data) { return {ok: res.ok, data: data} })
    })
    .then(function(res) {
      if (!res.ok) {
        alert(res.data.error || "操作失败")
      } else {
        alert("操作成功")
        closeAdminModal()
        // Refresh member list
        fetchGroupMembers(state.currentGroup.id)
      }
    })
    .catch(function(err) {
      alert("请求错误: " + err.message)
    })
  }

function bindEvents() {
$("login-button").onclick = login
$("logout-button").onclick = logout
$("add-friend-button").onclick = addFriend
  $("refresh-friends-button").onclick = refreshFriends
  $("create-group-button").onclick = createGroup
  $("refresh-groups-button").onclick = refreshGroups
  $("connect-ws-button").onclick = connectWs
$("send-message-button").onclick = sendMessage
$("upload-file-button").onclick = sendFileMessage
  $("chunk-upload-button").onclick = sendChunkedFileMessage
$("load-history-button").onclick = loadMoreHistory
$("chat-input").addEventListener("keydown", function (e) {
if (e.key === "Enter") {
sendMessage()
}
})
  $("invite-member-button").onclick = openInviteModal
  $("set-admin-button").onclick = openSetAdminModal
  $("leave-group-button").onclick = leaveGroup
  $("dismiss-group-button").onclick = dismissGroup
  $("chunk-file-input").addEventListener("change", resetChunkSession)
  
  // Modal bindings
  $("close-invite-modal").onclick = closeInviteModal
  $("cancel-invite-button").onclick = closeInviteModal
  $("confirm-invite-button").onclick = confirmInvite

  $("close-admin-modal").onclick = closeAdminModal
  $("cancel-admin-button").onclick = closeAdminModal
  $("confirm-admin-button").onclick = submitSetAdmins
  
  // Close modal when clicking outside
  window.onclick = function(event) {
    if (event.target == $("invite-friend-modal")) {
      closeInviteModal()
    }
    if (event.target == $("set-admin-modal")) {
      closeAdminModal()
    }
  }
}

document.addEventListener("DOMContentLoaded", function () {
bindEvents()
if (!loadSessionState()) {
updateCurrentUser()
updateCurrentTarget()
}
})
})()
