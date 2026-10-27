# Fill ops runbook

The local fill path on macOS: browser → socket bridge → origin. This is the
ops map for the crash/desync class of bugs from the week of 2026-10-26.

## Topology

| Piece | Identity / path | Role |
|---|---|---|
| Native messaging host | Go binary + `fill.json` (beside `fill.sock`) | Speaks extension native-messaging JSON; carry no `VEIL_HOME`, so config lives next to the socket, not in env |
| Fill bridge LaunchAgent | `nyc.veil.fill.bridge` (`~/Library/LaunchAgents/nyc.veil.fill.bridge.plist`) | Per-user agent that owns `~/.veil/fill.sock`. Safari appexes cannot spawn the host — the agent runs it |
| Fill socket | `~/.veil/fill.sock` | Frame endpoint; `veil fill --bridge` serves fill frames here instead of stdio |
| SSH agent socket | `~/.veil/ssh.sock` | Owned by the **same bridge** — one LaunchAgent, two sockets |
| Safari extension | `nyc.veil.fill.Extension` / `nyc.veil.fill` helper / `nyc.veil.fill.AutoFill` | appex side of the frame protocol |

`fill.json` outlives reinstalls — an existing file is the human's config and
is not clobbered. Browser-launched hosts read `fill.json`'s `token_file`
first, `VEIL_HUMAN_TOKEN_FILE` otherwise.

## Install / restart

```bash
veil fill install        # writes the LaunchAgent plist + native host config
launchctl kickstart -k gui/$UID/nyc.veil.fill.bridge   # restart the bridge
launchctl print gui/$UID/nyc.veil.fill.bridge          # state, last exit, pid
```

`InstallBridgeAgent` registers and (re)starts the agent; `BootstrapBridgeAgent`
restarts the existing plist. Both are macOS-only no-ops elsewhere.

## Troubleshooting

- **Stale socket after crash** — `launchctl kickstart -k` the agent, then
  `ls -l ~/.veil/fill.sock ~/.veil/ssh.sock` and confirm the mtime moved. A
  socket file that survives its process is the classic desync.
- **Bridge desync / fill hangs** — the bridge owns both sockets; a half-dead
  bridge shows in `launchctl print` as running-but-stale. Kickstart it; if
  the appex still can't connect, check `log show --predicate 'process ==
  "veil"' --last 10m` for the host's stderr.
- **Extension silent** — native host is the binary plus `fill.json`; a
  missing or malformed `fill.json` fails the host fast. Verify the file
  beside `~/.veil/fill.sock`, not in the app bundle.
- **After an OS or Veil.app update** — the appex path is rebuilt, the
  LaunchAgent plist is not. `veil fill install` rewrites it; stale plists
  pointing at old binary paths are the usual post-update break.
- **Not macOS** — none of this applies; the Linux/Windows tray story is a
  different surface (VTX-795).
