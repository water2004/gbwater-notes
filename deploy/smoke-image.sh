#!/usr/bin/env bash
set -euo pipefail

sudo install -d -m 0755 -o 10001 -g 10001 content frontend data/gbwater data/images data/render
docker compose config --quiet
docker compose run --rm --no-deps api export-frontend
test -s frontend/index.html
test -s frontend/admin/editor/index.html
grep -qF '/mypdf/assets/' frontend/index.html
test "$(cat frontend/catalog.json)" = '[]'
if docker compose run --rm --no-deps api export-frontend; then
  echo 'Export unexpectedly accepted a populated directory.' >&2
  exit 1
fi

sudo install -d -m 0755 -o 10001 -g 10001 content/math/Smoke
sudo tee content/math/Smoke/Smoke.md >/dev/null <<'NOTE'
---
title: Smoke
category: math
course: Smoke
courseSlug: smoke
slug: smoke
draft: false
---

# Linux build

Folia math $x^2 + 1$.

```mermaid
flowchart LR
  A[开始] --> B[End]
```
NOTE
docker compose up -d --wait --wait-timeout 150
curl --fail --silent --show-error http://127.0.0.1:8080/api/health

python3 - <<'PY'
import json
import os
import pathlib
import time
import urllib.request

base = "http://127.0.0.1:8080"
headers = {"Content-Type": "application/json"}

def request(route, payload=None):
    data = None if payload is None else json.dumps(payload).encode("utf-8")
    return urllib.request.urlopen(
        urllib.request.Request(base + route, headers=headers, data=data), timeout=15
    )

with request("/api/auth/login", {
    "identifier": "admin", "password": os.environ["GBWATER_ADMIN_PASSWORD"]
}) as response:
    assert json.load(response)["user"]["isAdmin"]
    headers["Cookie"] = response.headers["Set-Cookie"].split(";", 1)[0]

def build():
    with request("/api/admin/build", {}) as response:
        assert response.status == 202
    for _ in range(120):
        with request("/api/admin/build") as response:
            status = json.load(response)
        if status["state"] == "success":
            return status["log"]
        if status["state"] == "failed":
            raise RuntimeError(status["log"])
        time.sleep(1)
    raise TimeoutError("Linux note build timed out")

first = build()
assert "[native] 1 notes, 1 public, 1 formulas, 1 diagrams" in first, first
root = pathlib.Path("frontend")
page = root / "notes/smoke/index.html"
article = page.read_text()
assert "math-image" in article and "mermaid-figure" in article, article[:500]
assert len(list((root / "generated/math").glob("*.svg"))) == 1
assert len(list((root / "generated/mermaid").glob("*.svg"))) == 2
mtime = page.stat().st_mtime_ns
second = build()
assert "rendered 0, reused " in second and "updated 0, removed 0" in second, second
assert page.stat().st_mtime_ns == mtime
print("Verified Linux native rendering and an unchanged repeat build.")
PY
