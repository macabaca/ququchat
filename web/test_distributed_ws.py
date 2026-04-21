#!/usr/bin/env python3
"""
测试跨 Pod WebSocket 路由：
- alice 连 Pod1 (10.244.1.5)
- gorgio 连 Pod2 (10.244.2.5)
- alice 发消息给 gorgio，验证 gorgio 能否收到（跨 Pod Redis Pub/Sub）
"""
import asyncio
import json
import urllib.request

API = "http://172.19.0.3:30080/api"
POD1 = "ws://localhost:8081/ws"
POD2 = "ws://localhost:8082/ws"

def login(username, password):
    body = json.dumps({"username": username, "password": password}).encode()
    req = urllib.request.Request(f"{API}/auth/login", data=body,
                                  headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req) as r:
        data = json.loads(r.read())
    return data["accessToken"], data["user"]["id"]

async def run():
    try:
        import websockets
    except ImportError:
        print("请先安装依赖: pip install websockets")
        return

    print("登录中...")
    token_alice, id_alice = login("alice", "123456")
    token_gorgio, id_gorgio = login("gorgio", "396pqww!")
    print(f"alice id={id_alice}, gorgio id={id_gorgio}")

    received = asyncio.Event()
    result = {}

    async def gorgio_listen():
        async with websockets.connect(f"{POD2}?token={token_gorgio}") as ws:
            print(f"gorgio 已连接 Pod2 ({POD2})")
            received_ready.set()
            async for msg in ws:
                data = json.loads(msg)
                if data.get("type") == "friend_message" and data.get("from_user_id") == id_alice:
                    result["msg"] = data.get("content")
                    received.set()
                    return

    async def alice_send():
        await received_ready.wait()
        await asyncio.sleep(0.5)
        async with websockets.connect(f"{POD1}?token={token_alice}") as ws:
            print(f"alice 已连接 Pod1 ({POD1})")
            payload = {"type": "friend_message", "to_user_id": id_gorgio, "content": "hello from alice via Pod1"}
            await ws.send(json.dumps(payload))
            print("alice 已发送消息")
            await received.wait()  # 等 gorgio 收到再退出

    received_ready = asyncio.Event()
    await asyncio.wait_for(
        asyncio.gather(gorgio_listen(), alice_send()),
        timeout=15
    )

    if result.get("msg"):
        print(f"\n[PASS] gorgio 在 Pod2 收到消息: '{result['msg']}'")
        print("跨 Pod Redis Pub/Sub 路由正常工作！")
    else:
        print("\n[FAIL] gorgio 未收到消息，跨 Pod 路由可能有问题")

asyncio.run(run())
