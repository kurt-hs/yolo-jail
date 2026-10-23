# MCP and LSP

Two kinds of server extend what an agent can do, and yolo writes both into the settings of every
selected agent that supports them:

- An **MCP server** gives an agent extra tools, such as driving a browser, through the
  [Model Context Protocol](https://modelcontextprotocol.io/introduction).
- A **language server** gives an agent code intelligence, such as go-to-definition and diagnostics,
  through the [Language Server Protocol](https://microsoft.github.io/language-server-protocol/) (LSP).

Both are configured explicitly; none is on by default. They go in either config file, usually the
project's `yolo-jail.jsonc`, and a change reaches the agents at the jail's next fresh start.

## Which agents receive them

| Agent | MCP servers | Language servers |
|---|---|---|
| Claude Code | Yes | Yes, through a plugin yolo generates |
| Copilot | Yes | Yes |
| Codex | Yes | No: Codex has no language-server support |
| opencode | Yes | Not yet |
| pi | Yes. With the `pi-subagents` extension installed, pi's subagents get them too | Not yet |
| agy | Yes | No: agy has no language-server support |

pi has started MCP servers itself since version 0.99.0, so it needs no MCP extension. yolo writes
your servers into pi's own MCP file, `~/.pi/agent/mcp.json`, beside any you add with `pi mcp add`,
and what you change with pi's `/mcp` command stays. If you installed the `pi-mcp-adapter` extension
for yolo's servers before, remove it: with both, pi can start a server twice, once through each. A
subagent that lists `mcp:` tools needs `pi-subagents` 0.74.0 or later to run them without the
adapter, so update `pi-subagents` first.

## MCP Servers

### The browser: the `chrome-devtools` pack

The `chrome-devtools` pack lets the agent drive a headless Chromium browser, which is built into
the jail. Turn it on by adding `chrome-devtools` to `packs` in your user config,
`~/.config/yolo-jail/config.jsonc`, beside your agents:

```jsonc
{
  "packs": ["claude", "chrome-devtools"]
}
```

A project's `yolo-jail.jsonc` cannot select a pack. yolo installs the server when an agent starts
and keeps it current along with the agent. On `macos-user` the pack uses the Chrome or Chromium
your Mac already has.

To turn the server off in one project without dropping the pack, set it to `null`:

```jsonc
{
  "mcp_servers": {
    "chrome-devtools": null
  }
}
```

### Your own MCP servers

Add your own MCP servers under `mcp_servers`. The `command` must exist inside the jail:

```jsonc
{
  "mcp_servers": {
    "my-server": {
      "command": "/workspace/scripts/my-mcp.py",
      "args": ["--port", "3333"]
    }
  }
}
```

### `mcp_presets` is gone

The `mcp_presets` key was removed, and a config that still has it is refused until you delete
it. Its two servers each have a replacement:

- `chrome-devtools`: add the `chrome-devtools` pack, as above.
- `sequential-thinking` was deleted. To keep it, write the server yourself:

  ```jsonc
  {
    "mcp_servers": {
      "sequential-thinking": {
        "command": "npx",
        "args": ["-y", "@modelcontextprotocol/server-sequential-thinking"]
      }
    }
  }
  ```

  yolo does not install or update a server you write yourself; `npx -y` fetches it when the agent
  starts it.

---

## LSP Servers

yolo hands the language servers you declare to the agents that read them. It never installs a
language server itself: its program must already be on the jail's `PATH`, for example from
`mise_tools` or `packages`.

### Adding Servers

```jsonc
{
  "lsp_servers": {
    "rust": {
      "command": "rust-analyzer",
      "args": [],
      "fileExtensions": {".rs": "rust"}
    }
  }
}
```

An agent starts a server when it opens a file with a matching extension.

---
