;(function () {
var state = {
accessToken: null,
refreshToken: null,
user: null,
friends: [],
currentFriend: null,
messages: [],
ws: null,
wsConnected: false
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
setStatus($("login-status"), "登录成功，user_id=" + data.user.id, "ok")
updateCurrentUser()
refreshFriends()
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
renderFriendList()
updateCurrentTarget()
state.messages = []
$("chat-box").innerHTML = ""
loadLatestHistory()
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
if (!state.currentFriend) {
el.textContent = "当前聊天对象: 未选择"
return
}
el.textContent = "当前聊天对象: " + state.currentFriend.username + " (id=" + state.currentFriend.id + ")"
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
state.ws = null
state.wsConnected = false
state.accessToken = null
state.refreshToken = null
state.user = null
state.friends = []
state.currentFriend = null
state.messages = []
$("friend-list").innerHTML = ""
$("chat-box").innerHTML = ""
updateCurrentUser()
updateCurrentTarget()
setWsStatus("", null)
setStatus($("friend-status"), "", null)
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
}
ws.onclose = function () {
state.wsConnected = false
setWsStatus("已断开", "error")
}
ws.onerror = function () {
state.wsConnected = false
setWsStatus("连接错误", "error")
}
ws.onmessage = function (event) {
handleIncomingWsMessage(event.data)
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
appendChatLine(prefix, data.content, isMe, data)
} else {
appendChatLine("system", raw, false)
}
}

function appendChatLine(sender, text, isMe, data) {
var box = $("chat-box")
var div = document.createElement("div")
div.className = "chat-message " + (isMe ? "me" : "other")
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
content.textContent = text
div.appendChild(meta)
div.appendChild(content)
box.appendChild(div)
box.scrollTop = box.scrollHeight
}

function appendHistoryMessages(messages) {
	messages.forEach(function (m) {
		var isMe = state.user && m.sender_id === state.user.id
		var sender = isMe ? "我" : "对方"
		appendChatLine(sender, m.content_text || "", isMe, { timestamp: m.created_at })
		state.messages.push({
			id: m.id,
			sender_id: m.sender_id,
			content_text: m.content_text || "",
			created_at: m.created_at
		})
	})
}

function getEarliestMessageId() {
if (!state.messages.length) {
return null
}
var earliest = state.messages[0]
for (var i = 1; i < state.messages.length; i++) {
		if (state.messages[i].created_at < earliest.created_at) {
earliest = state.messages[i]
}
}
return earliest.id || null
}

function loadLatestHistory() {
if (!state.accessToken || !state.user || !state.currentFriend) {
	return
}
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
state.messages = []
$("chat-box").innerHTML = ""
return
}
state.messages = []
$("chat-box").innerHTML = ""
appendHistoryMessages(list)
})
.catch(function () {
appendChatLine("system", "加载历史失败", false)
})
}

function loadMoreHistory() {
if (!state.accessToken || !state.user || !state.currentFriend) {
appendChatLine("system", "请先登录并选择好友", false)
return
}
var earliestId = getEarliestMessageId()
if (!earliestId) {
	loadLatestHistory()
	return
}
fetchWithRefresh(apiBase() + "/messages/history/before?message_id=" + encodeURIComponent(earliestId), {
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
if (!state.currentFriend) {
appendChatLine("system", "请先在好友列表中选择一个好友", false)
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

function bindEvents() {
$("login-button").onclick = login
$("logout-button").onclick = logout
$("add-friend-button").onclick = addFriend
$("refresh-friends-button").onclick = refreshFriends
$("connect-ws-button").onclick = connectWs
$("send-message-button").onclick = sendMessage
$("load-history-button").onclick = loadMoreHistory
$("chat-input").addEventListener("keydown", function (e) {
if (e.key === "Enter") {
sendMessage()
}
})
}

document.addEventListener("DOMContentLoaded", function () {
bindEvents()
updateCurrentUser()
updateCurrentTarget()
})
})()
