#!/usr/bin/env python3
"""Servidor MCP minimo sobre stdio, para probar el cliente de ailoop."""
import json, sys

TOOLS = [
    {
        "name": "find_code",
        "description": "Find code by natural language. Searches the indexed graph and returns matching symbols with file and line numbers, ranked by relevance.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "query": {"type": "string", "description": "the natural language query"},
                "max_results": {"type": "integer", "description": "cap on results"},
            },
            "required": ["query"],
        },
    },
    {
        "name": "find_dead_code",
        "description": "List functions with no callers anywhere in the indexed repository.",
        "inputSchema": {"type": "object", "properties": {}},
    },
]

def send(obj):
    sys.stdout.write(json.dumps(obj) + "\n")
    sys.stdout.flush()

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        msg = json.loads(line)
    except Exception:
        continue

    method = msg.get("method")
    mid = msg.get("id")

    if method == "initialize":
        send({"jsonrpc": "2.0", "id": mid, "result": {
            "protocolVersion": "2024-11-05",
            "serverInfo": {"name": "fake-codegraph", "version": "0.1"},
            "capabilities": {},
        }})
    elif method == "notifications/initialized":
        # una notificacion no lleva respuesta; ademas mandamos ruido para
        # verificar que el cliente no lo confunda con un resultado
        send({"jsonrpc": "2.0", "method": "notifications/message",
              "params": {"level": "info", "data": "servidor listo"}})
    elif method == "tools/list":
        send({"jsonrpc": "2.0", "id": mid, "result": {"tools": TOOLS}})
    elif method == "tools/call":
        p = msg.get("params", {})
        name = p.get("name")
        args = p.get("arguments", {})
        if name == "find_code":
            txt = "internal/convert/convert.go:83  func Normalizar(s string) string\ninternal/convert/convert.go:68  texto := Normalizar(crudo)\n(query: %s)" % args.get("query")
            send({"jsonrpc": "2.0", "id": mid, "result": {
                "content": [{"type": "text", "text": txt}], "isError": False}})
        else:
            send({"jsonrpc": "2.0", "id": mid, "result": {
                "content": [{"type": "text", "text": "no such tool: %s" % name}], "isError": True}})
