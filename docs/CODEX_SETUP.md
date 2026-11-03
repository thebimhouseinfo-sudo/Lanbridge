# Codex setup

LAN Test Bridge supports either Claude Code or Codex as its Mac-side MCP client.
Open Bridge Settings and select the provider to register. Bridge manages the
user-level `lanbridge` entry and refuses to switch while a `bridge mcp` process
is active. Other MCP entries are left alone.

Codex reads its user configuration from `~/.codex/config.toml`. The example
[`codex-config.toml.example`](./codex-config.toml.example) contains the stdio
entry for Bridge; replace `USERNAME` with the macOS account name if configuring
manually. Do not replace the rest of `config.toml`.

If configuring manually, remove the other Bridge provider first so only one
`lanbridge` entry is enabled:

```sh
codex mcp add lanbridge -- "$HOME/Applications/LAN Bridge.app/Contents/MacOS/bridge" mcp
```

Claude Code user-scope registration:

```sh
claude mcp add --scope user --transport stdio lanbridge -- "$HOME/Applications/LAN Bridge.app/Contents/MacOS/bridge" mcp
```

To switch manually, remove the previous user-scope entry before adding the
other provider:

```sh
codex mcp remove lanbridge
claude mcp remove --scope user lanbridge
```

Bridge reports a disabled or conflicting `lanbridge` entry instead of silently
enabling, replacing, or deleting a configuration it cannot verify. Resolve that
entry in the corresponding agent configuration and retry from Settings.

**- test required:** confirm registration, reload, process start, and disconnect
with Codex and Claude Code on the target Mac.

References: [Codex MCP servers](https://developers.openai.com/codex/mcp/) and
[Claude Code MCP](https://code.claude.com/docs/en/mcp).
