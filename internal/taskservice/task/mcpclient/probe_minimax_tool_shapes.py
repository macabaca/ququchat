#!/usr/bin/env python3
import json
import os
import time
import urllib.error
import urllib.request


ENDPOINT = os.getenv("MCP_ENDPOINT", "http://127.0.0.1:3000/rest")
OUT_LOG = os.getenv(
    "OUT_LOG",
    "/root/lzy/ququchat/internal/taskservice/task/mcpclient/minimax_tool_shapes.log",
)
TIMEOUT_SEC = float(os.getenv("MCP_TIMEOUT_SEC", "30"))


def rpc_call(method, params, req_id):
    payload = {
        "jsonrpc": "2.0",
        "id": req_id,
        "method": method,
        "params": params,
    }
    request = urllib.request.Request(
        ENDPOINT,
        data=json.dumps(payload).encode("utf-8"),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    started = time.time()
    try:
        with urllib.request.urlopen(request, timeout=TIMEOUT_SEC) as response:
            body = response.read().decode("utf-8")
        elapsed_ms = int((time.time() - started) * 1000)
        return elapsed_ms, json.loads(body), None
    except urllib.error.HTTPError as err:
        elapsed_ms = int((time.time() - started) * 1000)
        body = err.read().decode("utf-8", errors="ignore")
        return elapsed_ms, None, f"http_error status={err.code} body={body}"
    except Exception as err:
        elapsed_ms = int((time.time() - started) * 1000)
        return elapsed_ms, None, f"request_error {err}"


def placeholder_for_schema(prop_schema):
    schema_type = prop_schema.get("type")
    if schema_type == "string":
        return ""
    if schema_type == "number" or schema_type == "integer":
        return 0
    if schema_type == "boolean":
        return False
    if schema_type == "array":
        return []
    if schema_type == "object":
        return {}
    return None


def build_min_required_args(tool):
    schema = tool.get("inputSchema") or {}
    required = schema.get("required") or []
    properties = schema.get("properties") or {}
    args = {}
    for key in required:
        prop_schema = properties.get(key) or {}
        args[key] = placeholder_for_schema(prop_schema)
    tool_name = tool.get("name", "")
    if tool_name == "list_voices":
        args["voiceType"] = "all"
    if tool_name == "play_audio":
        args["inputFilePath"] = "/tmp/not_found.mp3"
    if tool_name == "voice_clone":
        args["voiceId"] = "test-voice"
        args["audioFile"] = "/tmp/not_found.wav"
    return args


def summarize_tool_call(tool_name, elapsed_ms, response_obj, err_msg):
    item = {
        "tool": tool_name,
        "elapsed_ms": elapsed_ms,
        "ok": err_msg is None and isinstance(response_obj, dict) and "error" not in response_obj,
        "transport_error": err_msg,
    }
    if isinstance(response_obj, dict):
        item["response_top_keys"] = list(response_obj.keys())
        if "error" in response_obj:
            item["rpc_error"] = response_obj.get("error")
        result = response_obj.get("result")
        if isinstance(result, dict):
            item["result_keys"] = list(result.keys())
            content = result.get("content")
            item["content_type"] = type(content).__name__
            item["content_len"] = len(content) if isinstance(content, list) else None
            text_preview = ""
            if isinstance(content, list) and content:
                first = content[0]
                if isinstance(first, dict):
                    text_preview = str(first.get("text", ""))[:200]
            item["content_preview"] = text_preview
            extra_keys = [k for k in result.keys() if k != "content"]
            item["result_extra_keys"] = extra_keys
    return item


def main():
    lines = []
    lines.append(f"endpoint={ENDPOINT}")
    init_elapsed, init_resp, init_err = rpc_call(
        "initialize",
        {
            "protocolVersion": "2024-11-05",
            "capabilities": {},
            "clientInfo": {"name": "shape-prober", "version": "1.0.0"},
        },
        1,
    )
    lines.append(
        "initialize="
        + json.dumps(
            {
                "elapsed_ms": init_elapsed,
                "ok": init_err is None and isinstance(init_resp, dict) and "error" not in init_resp,
                "error": init_err or (init_resp.get("error") if isinstance(init_resp, dict) else None),
            },
            ensure_ascii=False,
        )
    )
    list_elapsed, list_resp, list_err = rpc_call("tools/list", {}, 2)
    if list_err is not None or not isinstance(list_resp, dict):
        lines.append(f"tools_list_error={list_err}")
        with open(OUT_LOG, "w", encoding="utf-8") as f:
            f.write("\n".join(lines) + "\n")
        return
    tools = ((list_resp.get("result") or {}).get("tools")) or []
    lines.append(f"tools_count={len(tools)}")
    all_items = []
    req_id = 10
    for tool in tools:
        name = tool.get("name", "")
        args = build_min_required_args(tool)
        elapsed, resp, err = rpc_call("tools/call", {"name": name, "arguments": args}, req_id)
        req_id += 1
        item = summarize_tool_call(name, elapsed, resp, err)
        item["sent_arguments"] = args
        all_items.append(item)
    lines.append(json.dumps(all_items, ensure_ascii=False, indent=2))
    with open(OUT_LOG, "w", encoding="utf-8") as f:
        f.write("\n".join(lines) + "\n")
    print(f"written log: {OUT_LOG}")


if __name__ == "__main__":
    main()
