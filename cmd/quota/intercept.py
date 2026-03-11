"""
mitmproxy addon: capture ALL claude.ai API calls and save to JSON.

Usage:
  mitmdump -s intercept.py --listen-port 8899 --quiet
  # Then in another terminal:
  HTTPS_PROXY=http://localhost:8899 \
  NODE_EXTRA_CA_CERTS=~/.mitmproxy/mitmproxy-ca-cert.pem \
  claude
  # Open /usage in the Claude Code TUI, then Ctrl+C here
"""

import json
import os
import sys
from datetime import datetime

OUTPUT_FILE = os.path.expanduser("~/.claude/captured_quota.json")
captured = {}


def response(flow):
    host = flow.request.host
    if host not in ("claude.ai", "api.anthropic.com"):
        return

    path = flow.request.path.split("?")[0]

    try:
        body = json.loads(flow.response.content)
    except Exception:
        body = flow.response.text[:500] if flow.response.text else None

    key = f"{host}{path}"
    entry = {
        "host": host,
        "path": path,
        "url": flow.request.pretty_url,
        "status": flow.response.status_code,
        "captured_at": datetime.utcnow().isoformat() + "Z",
        "request_headers": dict(flow.request.headers),
        "data": body,
    }
    captured[key] = entry

    with open(OUTPUT_FILE, "w") as f:
        json.dump(captured, f, indent=2)

    print(f"[intercept] {flow.response.status_code} {host}{path}", file=sys.stderr)
