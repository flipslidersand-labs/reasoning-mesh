---
title: "sakura VPS の SSH 逆トンネルが外部公開できない (GatewayPorts no)"
tags: [ssh, reverse-tunnel, sakura, nginx, proxy]
severity: medium
date: "2026-08-22"
---

## 症状

`ssh -R 8765:localhost:8765 sakura` で逆トンネルを張っても、
外部から `133.167.78.203:8765` にアクセスできない。

## 原因

sakura の `/etc/ssh/sshd_config` が `GatewayPorts no` (デフォルト)。
逆トンネルの bind は `127.0.0.1` のみになり、外部からは届かない。
`sudo` が必要なため password なしでは変更不可。

## 解決策

sakura のパケットフィルタ (コントロールパネル) は port 80/443 を開放済み。
nginx コンテナ (`/home/ubuntu/nginx/conf.d/webhook.conf`) に location を追加して
HTTPS 経由でプロキシ。

```
逆トンネルを別ポートで張る: -R 18765:localhost:8765
Python TCP プロキシ (sakura 上): 0.0.0.0:8765 → 127.0.0.1:18765
nginx: location /v1/ → proxy_pass http://127.0.0.1:8765/v1/
```

逆トンネルの永続化は `~/.config/systemd/user/rm-tunnel.service`。

## 予防

sakura への SSH 逆トンネルでポートを外部公開したい場合は
必ず nginx 経由か `GatewayPorts yes` (要 sudo) が必要。
既存 nginx の `conf.d/` に location を足すのが最速。
