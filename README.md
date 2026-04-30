# ee-tunnel

Open-source CLI that connects a local Ollama instance to your
[Entity Enricher](https://entityenricher.ai) organization.

A tunnel lets the multi-tenant production platform call your laptop's local
Ollama as if it were a regular LLM provider — useful for using models you
already have downloaded, for offline-first workflows, or for keeping certain
data on your machine.

- **Source:** https://github.com/TOT-Concept/ee-tunnel  *(this directory is
  maintained as a subtree of the main monorepo and synced here)*
- **License:** MIT
- **Docs:** see `docs/ARCHITECTURE.md` (Self-Service Ollama Tunnel section) in
  the main monorepo.

## How it works

```
┌────────── Entity Enricher (production) ──────────┐
│  enrichment job → custom httpx transport         │
│  hostname ollama.<your-org>.tunnel               │
│           ↓                                      │
│  in-process WebSocket bridge ───────────┐        │
└─────────────────────────────────────────┼────────┘
                                          │ WSS over :443
                                          │
                                  ┌───────┴────────┐
                                  │ ee-tunnel CLI  │
                                  │ (this binary)  │
                                  │ → localhost:11434
                                  └────────────────┘
```

No new public ports. Everything goes through the existing HTTPS endpoint.
Authentication is per-tunnel JWT (refresh + 15-min access tokens), bound to
your organization. Revoke instantly from the Entity Enricher UI.

## Install

```sh
curl -fsSL https://entityenricher.ai/install.sh | sh
```

The installer prints what it's about to do (download URL, signature method,
install path), pauses 5 seconds, and verifies a cosign signature before
making the binary executable. Source `https://entityenricher.ai/install.sh`
in `less` to audit it before running.

## Usage

```sh
# 1. Create a tunnel in the Entity Enricher UI (Models → Tunnels → + New tunnel).
#    Copy the refresh token from the modal.

# 2. Pair this device:
ee-tunnel pair --server https://entityenricher.ai <refresh-token>

# 3. Connect:
ee-tunnel
# ✓ Tunnel ready. Press Ctrl+C to stop.

# Other commands:
ee-tunnel status         # show pairing state
ee-tunnel disconnect     # forget local credentials (does NOT revoke server-side)
ee-tunnel version        # print version
```

## Environment

| Variable                | Effect                                          |
|---                      |---                                              |
| `EE_TUNNEL_OLLAMA_URL`  | Override the local Ollama URL at connect time.  |

## Build from source

Requires Go 1.23+.

```sh
go build -o ee-tunnel .
./ee-tunnel version
```

## Reporting issues

https://github.com/TOT-Concept/ee-tunnel/issues
