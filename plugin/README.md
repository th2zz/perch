# plugin/ · herdr plugin

This directory is what gets published to the herdr Marketplace (GitHub topic
`herdr-plugin`, indexed every 30 minutes). Installing it is the product's
one-line install.

## What is here

| File | Purpose |
|---|---|
| `herdr-plugin.toml` | The manifest: startup hook, actions, log pane, event hook |

The supervisor units are **not** here. They live in
`bridge/internal/service/units/` and are embedded into the binary, so the unit
that gets installed is byte-identical to the one that was reviewed. Generate
them with:

```bash
perch-bridge install-service
```

## How the pieces divide the work

The division reflects how `herdr` plugins and host supervisors work together:

| Concern | Owner | Why |
|---|---|---|
| Install / discover / update | **herdr plugin** | One command, reuses trust the user already has |
| Start-up nudge | `[[startup]]` → `ensure-running` | herdr's hook is a one-shot command, and this is a one-shot job |
| Staying alive | **systemd / launchd** | herdr does not supervise plugin processes |
| Event delivery | **the daemon's own socket subscription** | `[[events]]` spawns a process per event; the hook is only a redundancy |
| Acting on herdr | `agent.send_keys` / `agent.prompt` (decisions), `pane.send_keys` (the escape hatch's key bar) | The four decision classes, plus one scoped exception the user drives themselves — see constitution IV and contracts §2.2 |

## Install (once the binary exists)

```bash
go build -o ~/.local/bin/perch-bridge ./bridge/cmd/perch-bridge   # from the repo root
perch-bridge install-service                                 # keeps it alive
herdr plugin link "$PWD/plugin"                              # registers hooks and actions
herdr plugin action invoke perch.bridge.pair                 # pair a phone
```

## Uninstall

```bash
perch-bridge uninstall-service      # stops it, removes the unit, unlinks the plugin
```

Your herdr configuration is never modified, so there is nothing else to undo.
The only other thing that exists is the bridge's state directory, and
`uninstall-service` prints its path rather than deleting it.
