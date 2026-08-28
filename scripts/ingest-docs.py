#!/usr/bin/env python3
"""Fetch public tech-stack docs and bulk-ingest into rm_knowledge via llmo ingest-local.

Usage:
    cd /path/to/reasoning-mesh
    python3 scripts/ingest-docs.py
"""
import subprocess, urllib.request, re, time, os

CHUNK_SIZE = 1200
CHUNK_OVERLAP = 200
REPO_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

SOURCES = [
    # ── Go ────────────────────────────────────────────────────────
    {"url": "https://go.dev/doc/effective_go",              "task_type": "architecture",   "language": "go",     "tags": "effective-go,best-practices"},
    {"url": "https://go.dev/doc/faq",                       "task_type": "debugging",      "language": "go",     "tags": "go-faq,best-practices"},
    {"url": "https://go.dev/doc/diagnostics",               "task_type": "debugging",      "language": "go",     "tags": "go-diagnostics,profiling"},
    {"url": "https://go.dev/doc/gc-guide",                  "task_type": "performance",    "language": "go",     "tags": "go-gc,performance"},
    {"url": "https://go.dev/doc/code",                      "task_type": "implementation", "language": "go",     "tags": "go-basics,getting-started"},
    {"url": "https://go.dev/blog/error-handling-and-go",    "task_type": "debugging",      "language": "go",     "tags": "go-errors,error-handling"},
    {"url": "https://go.dev/blog/context",                  "task_type": "architecture",   "language": "go",     "tags": "go-context,concurrency"},
    {"url": "https://go.dev/blog/pipelines",                "task_type": "architecture",   "language": "go",     "tags": "go-concurrency,channels,pipelines"},
    {"url": "https://go.dev/blog/race-detector",            "task_type": "debugging",      "language": "go",     "tags": "go-race,concurrency"},
    {"url": "https://go.dev/blog/pprof",                    "task_type": "performance",    "language": "go",     "tags": "go-pprof,profiling"},
    {"url": "https://go.dev/blog/slices-intro",             "task_type": "implementation", "language": "go",     "tags": "go-slices,data-structures"},
    {"url": "https://go.dev/blog/maps",                     "task_type": "implementation", "language": "go",     "tags": "go-maps,data-structures"},
    {"url": "https://go.dev/blog/defer-panic-and-recover",  "task_type": "debugging",      "language": "go",     "tags": "go-defer,error-handling"},
    {"url": "https://go.dev/blog/laws-of-reflection",       "task_type": "implementation", "language": "go",     "tags": "go-reflection,advanced"},
    {"url": "https://go.dev/blog/json-and-go",              "task_type": "implementation", "language": "go",     "tags": "go-json,serialization"},
    {"url": "https://go.dev/blog/subtests",                 "task_type": "test",           "language": "go",     "tags": "go-testing,subtests"},
    {"url": "https://go.dev/blog/using-go-modules",         "task_type": "implementation", "language": "go",     "tags": "go-modules,dependency"},
    {"url": "https://go.dev/blog/intro-generics",           "task_type": "implementation", "language": "go",     "tags": "go-generics,type-system"},
    # ── Rust ──────────────────────────────────────────────────────
    {"url": "https://doc.rust-lang.org/book/ch04-01-what-is-ownership.html",           "task_type": "implementation", "language": "rust", "tags": "rust-ownership,memory"},
    {"url": "https://doc.rust-lang.org/book/ch04-02-references-and-borrowing.html",    "task_type": "implementation", "language": "rust", "tags": "rust-borrowing,memory"},
    {"url": "https://doc.rust-lang.org/book/ch06-00-enums.html",                       "task_type": "implementation", "language": "rust", "tags": "rust-enums,pattern-matching"},
    {"url": "https://doc.rust-lang.org/book/ch09-00-error-handling.html",              "task_type": "debugging",      "language": "rust", "tags": "rust-errors,result-option"},
    {"url": "https://doc.rust-lang.org/book/ch13-00-functional-features.html",         "task_type": "implementation", "language": "rust", "tags": "rust-iterators,closures"},
    {"url": "https://doc.rust-lang.org/book/ch16-00-concurrency.html",                 "task_type": "architecture",   "language": "rust", "tags": "rust-concurrency,threads"},
    {"url": "https://doc.rust-lang.org/book/ch17-00-async-await.html",                 "task_type": "architecture",   "language": "rust", "tags": "rust-async,tokio"},
    {"url": "https://doc.rust-lang.org/book/ch19-00-advanced-features.html",           "task_type": "architecture",   "language": "rust", "tags": "rust-unsafe,advanced"},
    {"url": "https://doc.rust-lang.org/book/ch11-00-testing.html",                     "task_type": "test",           "language": "rust", "tags": "rust-testing,tdd"},
    # ── Python ────────────────────────────────────────────────────
    {"url": "https://docs.python.org/3/library/asyncio-task.html",   "task_type": "architecture",   "language": "python", "tags": "asyncio,concurrency"},
    {"url": "https://docs.python.org/3/howto/logging.html",          "task_type": "debugging",      "language": "python", "tags": "python-logging,observability"},
    {"url": "https://docs.python.org/3/library/unittest.html",       "task_type": "test",           "language": "python", "tags": "python-testing,unittest"},
    {"url": "https://docs.python.org/3/library/typing.html",         "task_type": "implementation", "language": "python", "tags": "python-typing,type-hints"},
    # ── Kubernetes ────────────────────────────────────────────────
    {"url": "https://kubernetes.io/docs/concepts/workloads/pods/",                        "task_type": "architecture",   "language": "yaml", "tags": "kubernetes,pods,workloads"},
    {"url": "https://kubernetes.io/docs/concepts/services-networking/service/",           "task_type": "architecture",   "language": "yaml", "tags": "kubernetes,service,networking"},
    {"url": "https://kubernetes.io/docs/concepts/workloads/controllers/deployment/",     "task_type": "architecture",   "language": "yaml", "tags": "kubernetes,deployment,rollout"},
    {"url": "https://kubernetes.io/docs/concepts/configuration/configmap/",              "task_type": "implementation", "language": "yaml", "tags": "kubernetes,configmap,config"},
    {"url": "https://kubernetes.io/docs/concepts/configuration/secret/",                 "task_type": "security",       "language": "yaml", "tags": "kubernetes,secrets,security"},
    {"url": "https://kubernetes.io/docs/tasks/debug/debug-application/",                 "task_type": "debugging",      "language": "yaml", "tags": "kubernetes,debugging,kubectl"},
    # ── OpenTelemetry ─────────────────────────────────────────────
    {"url": "https://opentelemetry.io/docs/concepts/signals/traces/",         "task_type": "debugging",      "language": "go", "tags": "otel,tracing,observability"},
    {"url": "https://opentelemetry.io/docs/concepts/signals/metrics/",        "task_type": "debugging",      "language": "go", "tags": "otel,metrics,observability"},
    {"url": "https://opentelemetry.io/docs/languages/go/getting-started/",    "task_type": "implementation", "language": "go", "tags": "otel,go,getting-started"},
    # ── GitHub Actions ────────────────────────────────────────────
    {"url": "https://docs.github.com/en/actions/writing-workflows/workflow-syntax-for-github-actions", "task_type": "implementation", "language": "yaml", "tags": "github-actions,ci,workflow"},
    {"url": "https://docs.github.com/en/actions/using-workflows/reusing-workflows",                    "task_type": "architecture",   "language": "yaml", "tags": "github-actions,reusable-workflows"},
    # ── Qdrant ────────────────────────────────────────────────────
    {"url": "https://qdrant.tech/documentation/concepts/collections/", "task_type": "architecture",   "language": "go", "tags": "qdrant,vector-db,collections"},
    {"url": "https://qdrant.tech/documentation/concepts/search/",      "task_type": "implementation", "language": "go", "tags": "qdrant,vector-search,similarity"},
    {"url": "https://qdrant.tech/documentation/concepts/payload/",     "task_type": "implementation", "language": "go", "tags": "qdrant,payload,filtering"},
]


def strip_html(html: str) -> str:
    text = re.sub(r"<script[^>]*>.*?</script>", " ", html, flags=re.S)
    text = re.sub(r"<style[^>]*>.*?</style>", " ", text, flags=re.S)
    text = re.sub(r"<[^>]+>", " ", text)
    text = re.sub(r"&amp;", "&", text)
    text = re.sub(r"&lt;", "<", text)
    text = re.sub(r"&gt;", ">", text)
    text = re.sub(r"&nbsp;", " ", text)
    text = re.sub(r"&#[0-9]+;", " ", text)
    text = re.sub(r"&[a-zA-Z]+;", " ", text)
    return re.sub(r"\s+", " ", text).strip()


def chunk(text: str, size: int = CHUNK_SIZE, overlap: int = CHUNK_OVERLAP):
    chunks, start = [], 0
    while start < len(text):
        chunks.append(text[start:min(start + size, len(text))])
        start += size - overlap
    return chunks


def ingest(content: str, task_type: str, language: str, tags: str) -> bool:
    result = subprocess.run(
        ["go", "run", "./cmd/llmo/", "ingest-local",
         "-content", content, "-task-type", task_type,
         "-language", language, "-tags", tags],
        capture_output=True, text=True, timeout=30, cwd=REPO_DIR,
    )
    return result.returncode == 0


total = 0
for src in SOURCES:
    print(f"\nFetching {src['url']} ...", flush=True)
    try:
        req = urllib.request.Request(src["url"], headers={"User-Agent": "Mozilla/5.0"})
        with urllib.request.urlopen(req, timeout=15) as r:
            raw = r.read().decode("utf-8", errors="replace")
    except Exception as e:
        print(f"  SKIP: {e}")
        continue

    text = strip_html(raw)
    if len(text) < 200:
        print(f"  SKIP: too short ({len(text)} chars)")
        continue

    chunks = chunk(text)
    ok = fail = 0
    for i, c in enumerate(chunks):
        if len(c.strip()) < 100:
            continue
        if ingest(c, src["task_type"], src["language"], src["tags"]):
            ok += 1
        else:
            fail += 1
        if (i + 1) % 20 == 0:
            print(f"  {i+1}/{len(chunks)} ...", flush=True)
        time.sleep(0.05)

    print(f"  {ok} ingested, {fail} failed")
    total += ok

print(f"\nTotal ingested: {total}")
