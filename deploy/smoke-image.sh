#!/usr/bin/env bash
set -euo pipefail

mkdir -p data/db data/meilisearch
sudo install -d -m 0755 -o 10001 -g 10001 content frontend data/gbwater data/gbwater/content-assets data/render
docker compose config --quiet

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
for index in $(seq 1 64); do
  printf '\nThread check $x_{%s} + %s$.\n' "$index" "$index"
done | sudo tee -a content/math/Smoke/Smoke.md >/dev/null
docker compose up -d --wait --wait-timeout 150 db search
GBWATER_DOCKER_GATEWAY=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.Gateway}}{{end}}' "$(docker compose ps -q db)")
test -n "$GBWATER_DOCKER_GATEWAY"
export GBWATER_DOCKER_GATEWAY
docker compose up -d --wait --wait-timeout 150
test -s frontend/login/index.html
test -s frontend/admin/editor/index.html
test -s frontend/favicon.svg
grep -qF '/mypdf/favicon.svg' frontend/login/index.html
grep -qF '/mypdf/admin-assets/' frontend/login/index.html
grep -rqF '/mypdf/api/' frontend/admin-assets
test ! -e frontend/index.html
sudo test -s data/db/18/docker/PG_VERSION
test -d data/meilisearch/data.ms
curl --fail --silent --show-error http://127.0.0.1:8080/api/health

python3 - <<'PY'
import io
import base64
import json
import os
import pathlib
import struct
import time
import urllib.request
import zipfile
import zlib

base = "http://127.0.0.1:8080"
headers = {"Content-Type": "application/json"}

def request(route, payload=None, archive=None, method=None):
    data = None if payload is None else json.dumps(payload).encode("utf-8")
    request_headers = dict(headers)
    if archive is not None:
        data = archive
        request_headers["Content-Type"] = "application/zip"
    return urllib.request.urlopen(
        urllib.request.Request(base + route, headers=request_headers, data=data, method=method), timeout=15
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
assert "[native] 1 notes, 1 public, 65 formulas, 1 diagrams" in first, first
root = pathlib.Path("frontend")
page = root / "notes/smoke/index.html"
article = page.read_text()
assert "math-image" in article and "mermaid-figure" in article, article[:500]
assert "x^2 + 1" not in article and "flowchart LR" not in article
assert len(list((root / "generated/math").glob("*.svg"))) == 65
assert len(list((root / "generated/mermaid").glob("*.svg"))) == 2
mtime = page.stat().st_mtime_ns
second = build()
assert "[articles] rendered 0, reused 1" in second, second
assert "rendered 0, reused " in second and "updated 0, removed 0" in second, second
assert page.stat().st_mtime_ns == mtime

def png_chunk(kind, data):
    return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))

png = b"\x89PNG\r\n\x1a\n"
png += png_chunk(b"IHDR", struct.pack(">IIBBBBB", 1, 1, 8, 6, 0, 0, 0))
png += png_chunk(b"IDAT", zlib.compress(b"\x00\x22\x88\x55\xff"))
png += png_chunk(b"IEND", b"")
favicon = "data:image/png;base64," + base64.b64encode(png).decode()
icon = root / "favicon.svg"
original_icon = icon.read_bytes()
with request("/api/admin/site-settings", {"favicon": favicon}, method="PUT") as response:
    assert response.status == 200
third = build()
assert "[pages] rendered 0, reused " in third and "updated 1, removed 0" in third, third
assert favicon in icon.read_text() and page.stat().st_mtime_ns == mtime
with request("/api/admin/site-settings", {"favicon": ""}, method="PUT") as response:
    assert response.status == 200
fourth = build()
assert "updated 1, removed 0" in fourth and icon.read_bytes() == original_icon, fourth

with request("/api/admin/document-packages/export", {"ids": ["math/Smoke/Smoke"]}) as response:
    package = response.read()
with zipfile.ZipFile(io.BytesIO(package)) as archive:
    manifest = json.loads(archive.read("manifest.json"))
    assert manifest["version"] == 1
    assert manifest["notes"][0]["metadata"]["slug"] == "smoke"
with request("/api/admin/document-packages?preview=true", archive=package) as response:
    plan = json.load(response)
    assert plan["notes"][0]["action"] == "reuse" and not plan["conflicts"]
print("Verified Linux native rendering, repeat publishing, and favicon changes.")
PY
