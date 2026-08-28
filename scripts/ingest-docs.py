#!/usr/bin/env python3
"""Fetch public Go/Rust docs and bulk-ingest into rm_knowledge via llmo ingest-local."""
import subprocess, sys, textwrap, urllib.request, re, time

SOURCES = [
    {
        "url": "https://go.dev/doc/effective_go",
        "task_type": "architecture",
        "language": "go",
        "tags": "effective-go,best-practices",
        "strip_html": True,
    },
    {
        "url": "https://go.dev/doc/faq",
        "task_type": "debugging",
        "language": "go",
        "tags": "go-faq,best-practices",
        "strip_html": True,
    },
    {
        "url": "https://go.dev/doc/diagnostics",
        "task_type": "debugging",
        "language": "go",
        "tags": "go-diagnostics,profiling",
        "strip_html": True,
    },
    {
        "url": "https://go.dev/doc/gc-guide",
        "task_type": "performance",
        "language": "go",
        "tags": "go-gc,performance",
        "strip_html": True,
    },
    {
        "url": "https://go.dev/blog/error-handling-and-go",
        "task_type": "debugging",
        "language": "go",
        "tags": "go-errors,error-handling",
        "strip_html": True,
    },
    {
        "url": "https://go.dev/blog/context",
        "task_type": "architecture",
        "language": "go",
        "tags": "go-context,concurrency",
        "strip_html": True,
    },
    {
        "url": "https://go.dev/blog/pipelines",
        "task_type": "architecture",
        "language": "go",
        "tags": "go-concurrency,channels,pipelines",
        "strip_html": True,
    },
    {
        "url": "https://go.dev/blog/race-detector",
        "task_type": "debugging",
        "language": "go",
        "tags": "go-race,concurrency",
        "strip_html": True,
    },
    {
        "url": "https://go.dev/blog/pprof",
        "task_type": "performance",
        "language": "go",
        "tags": "go-pprof,profiling",
        "strip_html": True,
    },
    {
        "url": "https://go.dev/blog/slices-intro",
        "task_type": "implementation",
        "language": "go",
        "tags": "go-slices,data-structures",
        "strip_html": True,
    },
]

CHUNK_SIZE = 1200
CHUNK_OVERLAP = 200
LLMO_BIN = "./cmd/llmo"


def strip_html(html: str) -> str:
    text = re.sub(r"<script[^>]*>.*?</script>", " ", html, flags=re.S)
    text = re.sub(r"<style[^>]*>.*?</style>", " ", text, flags=re.S)
    text = re.sub(r"<[^>]+>", " ", text)
    text = re.sub(r"&amp;", "&", text)
    text = re.sub(r"&lt;", "<", text)
    text = re.sub(r"&gt;", ">", text)
    text = re.sub(r"&nbsp;", " ", text)
    text = re.sub(r"&#[0-9]+;", " ", text)
    text = re.sub(r"\s+", " ", text).strip()
    return text


def chunk(text: str, size: int, overlap: int):
    chunks = []
    start = 0
    while start < len(text):
        end = min(start + size, len(text))
        chunks.append(text[start:end])
        start += size - overlap
    return chunks


def ingest(content: str, task_type: str, language: str, tags: str) -> bool:
    result = subprocess.run(
        [
            "go", "run", LLMO_BIN + "/",
            "ingest-local",
            "-content", content,
            "-task-type", task_type,
            "-language", language,
            "-tags", tags,
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    return result.returncode == 0


total = 0
for src in SOURCES:
    print(f"\nFetching {src['url']} ...", flush=True)
    try:
        req = urllib.request.Request(src["url"], headers={"User-Agent": "Mozilla/5.0"})
        with urllib.request.urlopen(req, timeout=10) as r:
            raw = r.read().decode("utf-8", errors="replace")
    except Exception as e:
        print(f"  SKIP: {e}")
        continue

    text = strip_html(raw) if src.get("strip_html") else raw
    if len(text) < 200:
        print(f"  SKIP: too short ({len(text)} chars)")
        continue

    chunks = chunk(text, CHUNK_SIZE, CHUNK_OVERLAP)
    ok = fail = 0
    for i, c in enumerate(chunks):
        if len(c.strip()) < 100:
            continue
        if ingest(c, src["task_type"], src["language"], src["tags"]):
            ok += 1
        else:
            fail += 1
        if (i + 1) % 20 == 0:
            print(f"  {i+1}/{len(chunks)} chunks ...", flush=True)
        time.sleep(0.05)

    print(f"  done: {ok} ingested, {fail} failed")
    total += ok

print(f"\nTotal ingested: {total}")
