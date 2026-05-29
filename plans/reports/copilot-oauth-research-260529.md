# GitHub Copilot OAuth Research

Date: 2026-05-29

## Sources Checked

- 9router OAuth constants: https://github.com/decolua/9router/blob/7aa7a115994325e1c8b6739c8d23adf3fa3fbae1/src/lib/oauth/constants/oauth.js
- 9router token refresh/provider headers: https://github.com/decolua/9router/blob/7aa7a115994325e1c8b6739c8d23adf3fa3fbae1/open-sse/services/tokenRefresh.js
- OpenClaw GitHub Copilot OAuth: https://github.com/openclaw/openclaw/blob/8b12be05ec23f7a7add0fccf67370a1050fd58ad/src/llm/utils/oauth/github-copilot.ts
- Crush Copilot OAuth Go flow: https://github.com/charmbracelet/crush/blob/a4181d6d4ecc3a246ae949784b115f6c970bd105/internal/oauth/copilot/oauth.go
- Goose GitHub Copilot provider: https://github.com/aaif-goose/goose/blob/25ff547487ee3a80dfb7b995b20d32ad085f3fa3/crates/goose/src/providers/githubcopilot.rs
- Official GitHub Copilot CLI README: https://github.com/github/copilot-cli

## Implementation Decisions

- Use GitHub OAuth device flow with client ID `Iv1.b507a08c87ecfe98` and `read:user` scope.
- Exchange the GitHub OAuth token through `https://api.github.com/copilot_internal/v2/token`.
- Persist both the GitHub OAuth token and the internal Copilot token. Runtime calls use the Copilot token as the OpenAI-compatible bearer token.
- Use the Copilot token response endpoint when available; otherwise parse `proxy-ep` from the token and fall back to `https://api.individual.githubcopilot.com`.
- Apply Copilot-compatible request headers (`User-Agent`, `Editor-Version`, `Editor-Plugin-Version`, `Copilot-Integration-Id`, `OpenAI-Intent`, and GitHub API version) on proxy requests.
- Refresh the internal Copilot token before expiry using the stored GitHub token.
