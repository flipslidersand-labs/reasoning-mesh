#!/usr/bin/env python3
"""LLM育成日記 - 3ノード LLM 成長記録レポート生成

Usage:
    python3 llm-diary.py              # stdout + ファイル保存
    python3 llm-diary.py --discord    # + Discord 投稿 (gopass から webhook 取得)
"""

import json
import subprocess
import urllib.request
import sys
from datetime import datetime
from pathlib import Path

# --- Config ---
NODES = {
    "dev-nodee": {
        "host": "localhost",
        "ollama_port": 11434,
        "gpu": "GTX1080",
        "role": "ルーターくん",
        "ssh": None,
    },
    "YUKI": {
        "host": "192.168.68.56",
        "ollama_port": 11434,
        "gpu": "RTX4070 12GB",
        "role": "ナレッジ先輩",
        "ssh": "yuki-private",
        "windows": True,
    },
    "DS1": {
        "host": "192.168.68.60",
        "ollama_port": 11434,
        "gpu": "RTX4060",
        "role": "職人くん",
        "ssh": "ds1-wsl",
    },
}

QDRANT_URL = "http://localhost:6333"
RESULTS_DIR = Path(__file__).parent.parent / "results"
DIARY_DIR = RESULTS_DIR / "diary"
DIARY_DIR.mkdir(exist_ok=True)

KEY_COLLECTIONS = ["knowledge", "memory-system", "sessions", "llm-reports"]


# --- Data Collection ---

def _run_ssh(ssh_host, cmd, timeout=10):
    r = subprocess.run(["ssh", ssh_host, cmd], capture_output=True, text=True, timeout=timeout)
    return r.returncode, r.stdout.strip(), r.stderr.strip()


def get_ollama_models(name, cfg):
    try:
        if cfg["ssh"] is None:
            url = f"http://localhost:{cfg['ollama_port']}/api/tags"
            with urllib.request.urlopen(url, timeout=5) as r:
                data = json.loads(r.read())
        else:
            # Windows SSH hosts need curl.exe to avoid Invoke-WebRequest alias
            curl_cmd = "curl.exe" if cfg.get("windows") else "curl"
            code, out, _ = _run_ssh(cfg["ssh"], f"{curl_cmd} -s http://localhost:{cfg['ollama_port']}/api/tags")
            if code != 0 or not out:
                return None, "offline"
            data = json.loads(out)
        models = [m["name"] for m in data.get("models", [])]
        return models, "online"
    except Exception as e:
        return None, f"offline({type(e).__name__})"


def get_gpu_usage(name, cfg):
    cmd = "nvidia-smi --query-gpu=memory.used,memory.total --format=csv,noheader"
    try:
        if cfg["ssh"] is None:
            r = subprocess.run(cmd.split(), capture_output=True, text=True, timeout=5)
            out = r.stdout.strip()
        else:
            _, out, _ = _run_ssh(cfg["ssh"], cmd)
        if out:
            used, total = out.split(", ")
            used_mb = int(used.replace(" MiB", ""))
            total_mb = int(total.replace(" MiB", ""))
            return used_mb, total_mb, used_mb / total_mb * 100
    except Exception:
        pass
    return None, None, None


def parse_reasoning_results(n=5):
    """最新 n 件の reasoning-mesh 結果を集計"""
    files = sorted(RESULTS_DIR.glob("*.json"))[-n:]
    stats = {}
    for f in files:
        try:
            data = json.loads(f.read_text())
            for r in data.get("results", []):
                if r.get("error"):  # exclude infrastructure/network failures
                    continue
                m = r.get("model", "unknown")
                stats.setdefault(m, {"accuracy": [], "latency_ms": [], "keyword_recall": []})
                stats[m]["accuracy"].append(r.get("accuracy", 0))
                stats[m]["latency_ms"].append(r.get("latency_ms", 0))
                stats[m]["keyword_recall"].append(r.get("keyword_recall", 0))
        except Exception:
            pass
    result = {}
    for m, d in stats.items():
        accs, lats, kws = d["accuracy"], d["latency_ms"], d["keyword_recall"]
        if not accs:
            continue
        result[m] = {
            "avg_accuracy": sum(accs) / len(accs),
            "avg_latency_ms": sum(lats) / len(lats),
            "avg_keyword_recall": sum(kws) / len(kws),
            "samples": len(accs),
        }
    return result


def get_qdrant_stats():
    result = {}
    try:
        with urllib.request.urlopen(f"{QDRANT_URL}/collections", timeout=5) as r:
            cols = [c["name"] for c in json.loads(r.read()).get("result", {}).get("collections", [])]
        for col in KEY_COLLECTIONS:
            if col not in cols:
                continue
            try:
                with urllib.request.urlopen(f"{QDRANT_URL}/collections/{col}", timeout=5) as r2:
                    detail = json.loads(r2.read()).get("result", {})
                result[col] = detail.get("points_count") or detail.get("vectors_count", "?")
            except Exception:
                result[col] = "?"
    except Exception:
        pass
    return result


# --- Character Comments ---

def character_comment(name, status, models, gpu_pct, model_stats):
    if status != "online":
        return f"今日はお休み… ({status})"

    models = models or []

    if name == "dev-nodee":
        acc = model_stats.get("qwen2.5:7b", {}).get("avg_accuracy")
        if acc is not None:
            if acc >= 0.9:
                return "今日もルーティング精度トップクラス。ベテランの渋さが光った。"
            elif acc >= 0.7:
                return "まずまずの判断精度。GTX1080の重さも味のうち。"
            return "少し判断に迷いが見えた日。明日に期待。"
        return "指揮を執った。動作は渋いが、確実。"

    elif name == "YUKI":
        is_tuned = "ornith-tuned:latest" in models
        acc = (model_stats.get("ornith-tuned:latest") or model_stats.get("ornith:9b") or {}).get("avg_accuracy")
        if is_tuned and acc is not None and acc >= 0.9:
            return "チューニング後の先輩、絶好調。知識の深さが一段上がった。"
        if is_tuned:
            return "ornith-tuned が元気に稼働。チューニングの成果が着々と育っている。"
        if acc is not None and acc >= 0.85:
            return "ornith:9b の知識、今日もよく回った。RTX4070の速さが気持ちいい。"
        return "今日も知識を展開中。頭の回転が速い先輩だ。"

    elif name == "DS1":
        # DS1 は coder 特化なので reasoning-mesh に出てこない場合が多い
        if "qwen2.5-coder:7b" in models:
            return "黙々とコードを出力中。余計なことは言わない、それが職人。"
        return "今日も静かに待機。呼ばれたら即コードを出す。"

    return "稼働中。"


# --- Report Generation ---

def generate_report():
    today = datetime.now().strftime("%Y-%m-%d")

    # 収集
    node_info = {}
    for name, cfg in NODES.items():
        models, status = get_ollama_models(name, cfg)
        used_mb, total_mb, gpu_pct = get_gpu_usage(name, cfg)
        node_info[name] = {
            "models": models or [],
            "status": status,
            "gpu_used": used_mb,
            "gpu_total": total_mb,
            "gpu_pct": gpu_pct,
        }

    model_stats = parse_reasoning_results()
    qdrant_stats = get_qdrant_stats()

    online_count = sum(1 for n in node_info.values() if n["status"] == "online")
    family_icon = "✅" if online_count == 3 else ("⚠️" if online_count > 0 else "❌")
    family_label = f"{family_icon} {online_count}/3 ノード稼働中"

    lines = [
        f"# LLM育成日記 {today}",
        "",
        f"**家族の状況**: {family_label}",
        "",
        "## 家族構成",
        "",
        "| ノード | 役割 | GPU | VRAM使用率 | 状態 |",
        "|--------|------|-----|-----------|------|",
    ]

    for name, info in node_info.items():
        cfg = NODES[name]
        icon = "🟢" if info["status"] == "online" else "🔴"
        if info["gpu_pct"] is not None:
            vram = f"{info['gpu_used']}MB / {info['gpu_total']}MB ({info['gpu_pct']:.0f}%)"
        else:
            vram = "—"
        lines.append(f"| **{name}** | {cfg['role']} | {cfg['gpu']} | {vram} | {icon} {info['status']} |")

    # モデル一覧
    lines += ["", "## 搭載モデル", ""]
    for name, info in node_info.items():
        cfg = NODES[name]
        if info["models"]:
            model_list = " / ".join(f"`{m}`" for m in info["models"])
        else:
            model_list = "—（オフライン）"
        lines.append(f"- **{name}**（{cfg['role']}）: {model_list}")

    # reasoning-mesh 成績
    if model_stats:
        lines += [
            "",
            "## 成長記録（reasoning-mesh 直近5回平均）",
            "",
            "| モデル | 精度 | Keyword Recall | レイテンシ | サンプル |",
            "|--------|------|---------------|-----------|---------|",
        ]
        for model, s in sorted(model_stats.items(), key=lambda x: -x[1]["avg_accuracy"]):
            lines.append(
                f"| `{model}` | {s['avg_accuracy']:.0%} | {s['avg_keyword_recall']:.0%}"
                f" | {s['avg_latency_ms']:.0f}ms | {s['samples']} |"
            )

    # Qdrant
    if qdrant_stats:
        lines += ["", "## ナレッジ蓄積 (Qdrant)", "", "| コレクション | 件数 |", "|-------------|------|"]
        for col, count in qdrant_stats.items():
            lines.append(f"| {col} | {count} |")

    # キャラクターひとこと
    lines += ["", "## 今日のひとこと", ""]
    for name, info in node_info.items():
        cfg = NODES[name]
        comment = character_comment(name, info["status"], info["models"], info.get("gpu_pct"), model_stats)
        lines.append(f"**{name}（{cfg['role']}）** — {comment}")
        lines.append("")

    lines += [
        "---",
        f"*generated at {datetime.now().strftime('%Y-%m-%d %H:%M:%S')} by llm-diary.py*",
    ]

    return "\n".join(lines)


# --- Discord ---

def post_discord(report, webhook_url):
    chunks = [report[i:i+1800] for i in range(0, len(report), 1800)]
    for chunk in chunks:
        payload = json.dumps({"content": f"```md\n{chunk}\n```"}).encode()
        req = urllib.request.Request(
            webhook_url,
            data=payload,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        try:
            urllib.request.urlopen(req, timeout=10)
        except Exception as e:
            print(f"Discord エラー: {e}", file=sys.stderr)


def get_discord_webhook():
    r = subprocess.run(
        ["gopass", "show", "infra/discord/webhook-mail-digest"],
        capture_output=True, text=True
    )
    if r.returncode == 0:
        return r.stdout.strip()
    return None


# --- Main ---

if __name__ == "__main__":
    report = generate_report()

    today = datetime.now().strftime("%Y-%m-%d")
    out_path = DIARY_DIR / f"{today}.md"
    out_path.write_text(report)
    print(report)
    print(f"\n→ 保存: {out_path}", file=sys.stderr)

    if "--discord" in sys.argv:
        url = get_discord_webhook()
        if url:
            post_discord(report, url)
            print("→ Discord 投稿完了", file=sys.stderr)
        else:
            print("→ Discord webhook が取得できませんでした", file=sys.stderr)
