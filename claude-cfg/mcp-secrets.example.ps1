# Template for per-stick MCP secrets. Copy to `mcp-secrets.ps1` (gitignored) and
# fill in real values. profile.ps1 dot-sources mcp-secrets.ps1 BEFORE launching
# claude, so these land in the session env and are inherited by the bundled MCP
# server child processes (see claude-cfg\mcp-servers.json).
#
# github MCP requires a GitHub Personal Access Token (classic; `repo` scope, or
# fine-grained with the access you need). Without it, the github server starts
# but every call fails auth. The other three servers (context7, sequential-
# thinking, playwright) need no secret and work out of the box.
$env:GITHUB_PERSONAL_ACCESS_TOKEN = 'ghp_REPLACE_ME'

# Optional: context7 is keyless by default (npx @upstash/context7-mcp). A key is
# only needed for higher rate limits AND requires switching mcp-servers.json to
# the HTTP transport (url https://mcp.context7.com/mcp, header CONTEXT7_API_KEY).
# $env:CONTEXT7_API_KEY = 'ctx7sk-...'
