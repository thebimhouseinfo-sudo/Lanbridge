# Architecture — LAN Test Bridge

## Overview

Một binary Go, 3 lệnh con:
- `bridge host` — Windows. Gate quyền, file, lệnh build, MCP dự án, Start/Stop app.
- `bridge client` — macOS. Giữ kết nối tới host, pairing, chat.
- `bridge mcp` — macOS, do Mac agent khởi chạy (stdio). Nối vào `bridge client` qua localhost.

Mac ↔ Windows: JSON messages trên mTLS TCP (pin fingerprint), Wi-Fi LAN, cổng mặc định 54823.
Windows chỉ cần `bridge.exe`; mọi build làm trên Mac.

## Components

```
cmd/bridge/          — entrypoint: host | client | mcp
internal/
  transport/         — TLS, heartbeat, reconnect, mDNS discovery, pairing (CPace)
  protocol/          — kiểu message, mã lỗi (dùng chung 3 vai)
  store/             — đọc/ghi JSON an toàn (temp → rename)
  permissions/       — grants + Resolve (kiểm tra đường dẫn duy nhất)
  fileops/           — read, sync
  commands/          — 8 lệnh cố định theo project, Job Object, job/timeout
  mcp/               — kiểu JSON-RPC/MCP dùng chung
  agentmcp/          — `bridge mcp`: MCP server stdio cho Mac agent
  mcpfwd/            — phiên MCP tới server dự án trên host (tự mở lại khi lỗi)
  apps/              — Start / WM_CLOSE
  chat/              — relay text trong phiên
  ui/                — tray/menu bar + HTTP/SSE localhost + trang HTML nhúng
```

## Data flow

```
Mac agent ──stdio──► bridge mcp ──localhost──► bridge client ══mTLS══► bridge host (Windows)
Mac user  ──browser──► bridge client UI                                  │
                                                Windows user ◄─browser── host UI (Accept/Reject…)
host:  permissions.Resolve → fileops | commands | mcpfwd → CAI MCP server | apps
```

## Key decisions

- Windows: CGO tắt, 1 file exe, không cài runtime. Mac: build native (CGO cho menu bar).
- Không framework UI; trang HTML nhúng, chỉ nghe 127.0.0.1 kèm token.
- Ghép nối: mã 6 số + CPace (chép gói chuẩn kèm license) + xác nhận khóa 2 chiều → pin fingerprint. Không CA, không tự chế crypto.
- Sync: vào repo root hoặc `related_dirs` của project (ví dụ thư mục nạp add-in).
- Nhiều repo: mỗi project 1 mục trong `config.json` (root, 8 lệnh, MCP). Quyền do người dùng cấp lưu ở `state.json`.
- Gate đơn giản: path trong phạm vi + capability khớp → cho phép. Không sandbox, audit, per-tool policy.
- Lệnh build chạy trong Job Object, `CREATE_NO_WINDOW`; app (AutoCAD…) thì không. Mac Thoát → hủy lệnh; rớt mạng → lệnh vẫn chạy.
- MCP: chỉ chuyển tiếp tools (`tools/list`, `tools/call`), tên `<project>__<tool>`.
