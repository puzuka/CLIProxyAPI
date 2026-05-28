# Kiro Provider (Amazon Q Developer / AWS CodeWhisperer)

The Kiro provider exposes Claude models served via **AWS CodeWhisperer** to your
OpenAI-/Claude-/Gemini-compatible clients through CLIProxyAPI. It is the AWS-side
counterpart of Anthropic Claude Code: same Claude model family, different upstream
contract, different auth.

> **Compatibility note.** The Kiro provider was ported from
> [HALDRO/CLIProxyAPI-Extended](https://github.com/HALDRO/CLIProxyAPI-Extended) and
> adapted to the v7 codebase. The legacy translator path (Claude shape) is wired up;
> the canonical-IR variant from that fork is intentionally not included.

## Available models

Subscription-dependent; the most common entries on the free tier are:

| Family | Models |
|---|---|
| Claude | `claude-sonnet-4-5`, `claude-haiku-4-5`, `claude-sonnet-4` |
| Open models | `glm-5`, `deepseek-v3.2`, `minimax-m2.5`, `minimax-m2.1`, `qwen3-coder-next` |

Model IDs are normalized — `claude-sonnet-4-5`, `claude-sonnet-4.5`, and
`claude-sonnet-4-5-20250929` all resolve to the same upstream model.

## Authentication flows

CLIProxyAPI ships five Kiro login flows. Pick one and run it once; tokens are saved
under `auth-dir` and refreshed automatically by the watcher:

| Command | Flow | When to use |
|---|---|---|
| `cli-proxy-api -kiro-login` | Google OAuth via `kiro://` callback | Default — Kiro IDE Google login parity |
| `cli-proxy-api -kiro-aws-login` | AWS Builder ID **device-code** flow | Headless / SSH boxes, CI |
| `cli-proxy-api -kiro-aws-authcode-login` | AWS Builder ID **authorization-code** flow (browser callback) | Local desktop with browser |
| `cli-proxy-api -kiro-idc-login -kiro-idc-start-url=URL -kiro-idc-region=us-east-1 -kiro-idc-flow=device` | IAM Identity Center (Enterprise SSO) | Corporate / Okta / Entra ID |
| `cli-proxy-api -kiro-import` | Import existing Kiro IDE / `kiro-cli` cache | You already logged in via the official client |

After login the proxy creates a token JSON in `auth-dir`. It is auto-refreshed in the
background until the refresh token expires.

### Sources the importer can consume

- `~/.aws/sso/cache/kiro-auth-token.json` (Kiro IDE)
- `~/.aws/sso/cache/<sso-hash>.json` (IAM Identity Center)
- `~/.local/share/kiro-cli/data.sqlite3` (kiro-cli)
- `~/.local/share/amazon-q/data.sqlite3` (legacy Amazon Q Developer CLI)

## Inline credentials in `config.yaml` (optional)

Useful for CI / containers where running an interactive login is awkward. See the
`kiro:` block in `config.example.yaml`. Each entry can either point at a token JSON
file, or carry an inline access/refresh token pair plus profile ARN and region.

```yaml
kiro:
  - token-file: "~/.aws/sso/cache/kiro-auth-token.json"
    region: "us-east-1"
    preferred-endpoint: "codewhisperer"  # or "amazonq"
  - access-token: "eyJ..."
    refresh-token: "eyJ..."
    profile-arn: "arn:aws:codewhisperer:us-east-1:..."
    region: "us-east-1"
```

## Endpoint selection

Two upstreams are available:

- `codewhisperer` (default) — IDE quota, Kiro IDE behavior.
- `amazonq` — CLI quota, Amazon Q Developer CLI behavior.

Set globally with `kiro-preferred-endpoint:` or per-credential with
`preferred-endpoint:` inside a `kiro:` entry. Per-credential overrides the global
default.

## Fingerprint pinning (advanced)

By default, every Kiro request rotates a User-Agent / SDK / OS fingerprint pulled
from a built-in pool to mimic genuine Kiro IDE traffic. Pin a fixed fingerprint when
you need stable telemetry or have to satisfy a corporate proxy that allow-lists
exact User-Agent strings:

```yaml
kiro-fingerprint:
  oidc-sdk-version: "aws-sdk-js/3.749.0"
  runtime-sdk-version: "node/v22.11.0"
  streaming-sdk-version: "aws-sdk-js/3.749.0"
  os-type: "Mac OS X"
  os-version: "14.6.1"
  node-version: "v22.11.0"
  kiro-version: "0.1.30"
  kiro-hash: "abc123..."
```

Empty fields fall back to the random pool, so you can pin only what you need.

## Multi-account login

Set `incognito-browser: true` in `config.yaml` to suggest opening OAuth URLs in a
private window. This currently only flips a preference flag — the OS browser
invocation itself is unchanged — but the OAuth flows respect the flag and inform
the user accordingly.

## Caveats and risks

- AWS does not offer a public consumer API for CodeWhisperer/Kiro. Reverse-proxying
  tokens issued by `kiro-cli` is a grey area under the Kiro EULA. **Read the EULA
  before deploying publicly or sharing accounts.** Account suspension is possible.
- Endpoint availability varies by region. Most reliable: `us-east-1`, `eu-central-1`.
  In restricted networks (PRC, corporate proxies) configure a `proxy-url` per
  credential or a global proxy.
- Free-tier model availability changes over time. As of January 2026, Claude Opus 4.5
  was removed from the free tier.
- Kiro responses are returned as **AWS Event Stream binary frames**, not JSON SSE.
  The internal translator handles this transparently — clients see standard OpenAI /
  Claude responses.

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `503 Service Unavailable` on every request | Kiro endpoint unreachable in this region | Set per-credential `region:` to `us-east-1` or `eu-central-1`, or configure a proxy. |
| OIDC works but chat returns 4xx | Region mismatch between SSO and CodeWhisperer | Override `region` in the `kiro:` entry. |
| `403` after ~10 minutes of idle | Access token expired and refresh failed | Run the corresponding `-kiro-*-login` again to refresh the refresh token. |
| Tokens not picked up | File outside `auth-dir` | Move/symlink the JSON into `auth-dir`, or use `-kiro-import`. |
