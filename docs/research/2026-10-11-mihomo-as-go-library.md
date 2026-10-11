# Ticket #35: How mihomo embeds as a Go library

Research date: 2026-10-11

Pinned sources:

- mihomo `Meta` branch HEAD
  [`88dcbf7f`](https://github.com/MetaCubeX/mihomo/tree/88dcbf7f1614a67c3b36b848ee3592dfa92ada36)
  (2026-09-30). This is exactly the latest release tag **`v1.19.32`**
  (`git rev-list -n1 v1.19.32` = `88dcbf7f`), so the two pins are the same.
- sing-tun **`v0.4.27`** as pinned by that mihomo `go.mod`, commit
  [`27e28c30`](https://github.com/MetaCubeX/sing-tun/tree/27e28c30de350cff262e32d9ed92b87215a0ff7c).
- Local toolchain `go1.27.1 linux/amd64`.

Tags: **[verified]** means I read the code at the pin or ran the command.
**[inferred]** means a conclusion drawn from the code that I did not run.

## Verdict

- **It embeds cleanly.** Import `hub/executor`, not `hub`. Then
  `executor.ParseWithBytes` + `executor.ApplyConfig(cfg, true)` brings up the
  whole core with **no REST controller**: `hub/route` is never linked. Every
  REST endpoint Omihomo uses today is a short call into `tunnel`,
  `adapter/outboundgroup`, `tunnel/statistic`, `listener` or `log`. Section 1
  maps each one. A scratch embedder built and ran (non-root, TUN off): it
  applied a config, listed groups, set a selection, wrote `cache.db` and shut
  down. **[verified]**
- **Everything is process-global.** There is one mihomo per process.
  `ApplyConfig` is serialized by a package mutex, returns no error, and can call
  `os.Exit(2)`. It blacks out traffic until providers finish loading. The core
  interface must own TUN state and drive reloads itself. **[verified]**
- **Build:** official releases use `-tags with_gvisor -trimpath -ldflags "-X
  …constant.Version=… -X …constant.BuildTime=… -w -s -buildid="` with
  `CGO_ENABLED=0`. The `linux-amd64` asset is `GOAMD64=v3`. It is built with
  MetaCubeX's patched Go 1.26 (the binary reports `go1.26.8`). There is one
  `replace` (protobuf). It is **not inherited**, and leaving it out costs about
  10 MB. A second non-inherited setting also matters: mihomo's `go 1.20`
  directive gives official binaries Go 1.20 `GODEBUG` defaults. An embedder
  gets Go 1.27 defaults unless it adds `godebug default=go1.20`. **[verified]**
- **resolvectl:** yes, sing-tun still runs `resolvectl` from inside the hosting
  process every time a TUN comes up on Linux. It runs `domain … ~.`,
  `default-route … true` and `dns … 198.18.0.2`, then `revert` on close. This
  happens whether or not `auto-route` or `dns-hijack` is set. mihomo exposes no
  option to turn it off. The only off-switch is making `resolvectl` unfindable
  on `PATH`. **[verified]**
- **Size:** the embedding binary is 60.5 MB stripped with `with_gvisor` (22.5 MB
  gzip, 17.4 MB zstd-19). That is 0.7 MB smaller than full mihomo built the same
  way, and 1.5 MB smaller than the official 62.0 MB binary. Without
  `with_gvisor` it is 49.8 MB. **[verified]**
- **License:** the `LICENSE` file is byte-identical to the FSF GPLv3 text, with
  no "or later" notice. The README says "GPL-3.0". No `.go` file has a header.
  Best-fit SPDX is **`GPL-3.0-only`**. One upstream artifact conflicts: their
  fpm packaging metadata says `GPL-3.0-or-later`. The README also asks
  non-affiliated downstream projects not to put "mihomo" in their names.
  **Omihomo does.** **[verified]**

## 1. Entry points (the in-process API)

The REST controller in `hub/route` is a thin wrapper around package functions.
The table shows what each Omihomo-relevant endpoint calls. That is what the core
interface should call. **[verified]**

| Need | REST endpoint | In-process call at the pin |
|---|---|---|
| Parse | `PUT /configs` | `executor.ParseWithBytes(buf []byte) (*config.Config, error)` → `config.Parse(buf)` ([executor.go#L78-L81](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/hub/executor/executor.go#L78-L81), [config.go#L474-L481](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/config/config.go#L474-L481)) |
| Apply | `PUT /configs?force=` | `executor.ApplyConfig(cfg *config.Config, force bool)` ([configs.go#L401-L450](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/hub/route/configs.go#L401-L450)) |
| TUN on/off | `PATCH /configs {"tun":…}` | `listener.ReCreateTun(tunConf LC.Tun, tunnel C.Tunnel)` with `listener.LastTunConf` / `listener.GetTunConf() LC.Tun` ([configs.go#L377](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/hub/route/configs.go#L377), [listener.go#L77-L82](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/listener/listener.go#L77-L82)) |
| List proxies | `GET /proxies` | `tunnel.Proxies() map[string]C.Proxy` ([proxies.go#L62-L67](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/hub/route/proxies.go#L62-L67), [tunnel.go#L214-L222](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/tunnel/tunnel.go#L214-L222)) |
| List groups | `GET /group` | filter `tunnel.Proxies()` by `p.Adapter().(outboundgroup.ProxyGroup)` ([groups.go#L31-L41](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/hub/route/groups.go#L31-L41)) |
| Providers | `GET /providers/proxies` | `tunnel.Providers() map[string]P.ProxyProvider` |
| Select | `PUT /proxies/{group}` | `p.Adapter().(outboundgroup.SelectAble).Set(name)` **then** `cachefile.Cache().SetSelected(group, name)` ([proxies.go#L74-L104](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/hub/route/proxies.go#L74-L104)) |
| Unfix auto group | `DELETE /proxies/{group}` | `SelectAble.ForceSet("")` + `SetSelected(group, "")` ([proxies.go#L150-L160](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/hub/route/proxies.go#L150-L160)) |
| Proxy delay | `GET /proxies/{n}/delay` | `C.Proxy.URLTest(ctx, url string, expected utils.IntRanges[uint16]) (uint16, error)` ([proxies.go#L106-L148](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/hub/route/proxies.go#L106-L148)) |
| Group delay | `GET /group/{n}/delay` | `ProxyGroup.URLTest(ctx, url, expected) (map[string]uint16, error)`. It first clears a fixed choice on non-Selector groups ([groups.go#L53-L93](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/hub/route/groups.go#L53-L93)) |
| Connections | `GET /connections` | `statistic.DefaultManager.Snapshot() *statistic.Snapshot` ([connections.go#L24-L71](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/hub/route/connections.go#L24-L71)) |
| Close one / all | `DELETE /connections[/{id}]` | `DefaultManager.Get(id).Close()` / `DefaultManager.Range(func(statistic.Tracker) bool)` ([connections.go#L73-L87](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/hub/route/connections.go#L73-L87)) |
| Traffic | `GET /traffic` | 1 s ticker over `DefaultManager.Now() (up, down int64)` + `Total()` ([server.go#L371-L415](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/hub/route/server.go#L371-L415)) |
| Memory | `GET /memory` | `DefaultManager.Memory() uint64`, the RSS of the **whole process**, so it includes the daemon ([manager.go#L79-L104](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/tunnel/statistic/manager.go#L79-L104)) |
| Logs | `GET /logs` | `log.Subscribe() observable.Subscription[log.Event]` / `log.UnSubscribe(sub)` ([server.go#L515-L528](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/hub/route/server.go#L515-L528)) |
| Shutdown | (process exit) | `executor.Shutdown()` ([executor.go#L532-L538](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/hub/executor/executor.go#L532-L538)) |

Interfaces involved, at the pin:

- `C.Proxy` adds `Adapter()`, `DelayHistory()`, `LastDelayForTestUrl()` and
  `URLTest()` on top of `C.ProxyAdapter`. `ProxyAdapter` has `Name()`, `Type()`
  and `MarshalJSON()`; that JSON is what `/proxies` returns
  ([adapters.go#L126-L174](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/constant/adapters.go#L126-L174)).
- `outboundgroup.ProxyGroup` has `Providers()`, `Proxies()`, `Now()`,
  `Hidden()`, `Icon()` and `URLTest(...) (map[string]uint16, error)`.
  `outboundgroup.SelectAble` has `Set(string) error` and `ForceSet(string)`.
  `Fallback`, `URLTest` and `Selector` implement it
  ([util.go#L11-L37](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/adapter/outboundgroup/util.go#L11-L37)).
- `statistic.Tracker` has `ID()`, `Close()` and `Info() *TrackerInfo`.
  `TrackerInfo` holds the UUID, metadata, upload/download totals, start time,
  chains and rule ([tracker.go#L17-L34](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/tunnel/statistic/tracker.go#L17-L34)).

`hub` vs `executor` **[verified]**:

- `hub.Parse(bytes, opts...)` is executor parsing plus `hub.ApplyConfig`.
  `hub.ApplyConfig` = `applyRoute(cfg)` (`route.ReCreateServer`, the HTTP/TLS/
  unix controller) + `executor.ApplyConfig(cfg, true)`
  ([hub.go#L54-L106](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/hub/hub.go#L54-L106)).
  Its comment says it covers "all parts include ExternalController".
- `executor.ApplyConfig` is "without ExternalController"
  ([executor.go#L83-L123](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/hub/executor/executor.go#L83-L123)).
- **Do not import `hub` or `hub/route`.** Calling `executor` directly means no
  listener and no secret, and the controller code (including `/restart` and
  `/upgrade`, which self-update the core) is never linked. In the scratch module,
  `go list -deps` shows no `hub/route`. Pass `force=true` so the config's inbound
  ports are applied.
- `main.go` shows the whole lifecycle: `C.SetHomeDir` → `hub.Parse` →
  `updater.RegisterGeoUpdater()` if `geo-auto-update` → wait for signals →
  `executor.Shutdown()`. SIGHUP re-runs `hub.Parse`
  ([main.go#L126-L252](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/main.go#L126-L252)).

TUN start/stop **[verified]**:

- `ApplyConfig` calls `updateTun(cfg.General)` → `listener.ReCreateTun`
  ([executor.go#L210-L212](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/hub/executor/executor.go#L210-L212)).
- `ReCreateTun` holds `tunMux`. If the new config `Equal`s `LastTunConf`, it
  only calls `OnReload()` and returns. Otherwise it closes the old listener and,
  if `Enable`, calls `sing_tun.New`.
- **Errors are only logged.** On failure `LastTunConf.Enable` is set to `false`,
  so `listener.GetTunConf().Enable` is the only way to detect failure
  ([listener.go#L498-L535](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/listener/listener.go#L498-L535)).
- To stop TUN at runtime, copy `GetTunConf()`, set `Enable=false` and call
  `ReCreateTun`. This is exactly what `PATCH /configs` does.
- Teardown goes through `sing_tun.Listener.Close`. That closes the stack, the
  TUN (routes, rules, `resolvectl revert`), auto-redirect and the monitors, and
  resets the global dialer hooks
  ([server.go#L670-L688](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/listener/sing_tun/server.go#L670-L688)).
  A failed `New` closes itself
  ([server.go#L326-L331](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/listener/sing_tun/server.go#L326-L331)).
- **Gotcha [inferred from code]:** `executor.Shutdown()` →
  `listener.Cleanup()` closes the TUN but does **not** reset `LastTunConf`
  ([listener.go#L720-L729](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/listener/listener.go#L720-L729)).
  `Tun.Equal` compares `Enable` first
  ([tun.go#L99-L102](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/listener/config/tun.go#L99-L102)).
  So re-applying the same config after `Shutdown()` would silently not recreate
  the TUN. Use `ReCreateTun(Enable=false)` for "stop", not `Shutdown`.
- **Gotcha [verified]:** every `ApplyConfig` re-applies `cfg.General.Tun`. A TUN
  the user turned off comes back on the next reload if the YAML has
  `tun.enable: true`. The core must overwrite `cfg.General.Tun.Enable` from its
  own state before every `ApplyConfig`.

Selection persistence **[verified]**:

- `SelectAble.Set` changes only memory. The REST handler is what writes
  `cachefile.Cache().SetSelected`, and the core must do the same.
- After loading providers, `ApplyConfig` restores selections from the
  cachefile when `profile.store-selected` is true (`updateProfile` →
  `patchSelectGroup` → `ForceSet`)
  ([executor.go#L439-L467](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/hub/executor/executor.go#L439-L467)).
- `SetSelected` / `SelectedMap` are no-ops when `store-selected` is false
  ([cache.go#L33-L74](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/component/profile/cachefile/cache.go#L33-L74)).

Logs **[verified]**:

- Every `log.Infoln` etc. does a blocking send on an **unbuffered** channel
  ([log.go#L12-L41](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/log/log.go#L12-L41)).
- That channel fans out under a mutex to subscribers. Each subscriber's
  200-slot buffer is written with a **blocking** send
  ([observable.go#L16-L25](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/common/observable/observable.go#L16-L25),
  [subscriber.go#L14-L32](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/common/observable/subscriber.go#L14-L32)).
- So one slow subscriber stalls all mihomo logging, and with it the code paths
  that log. The REST handler avoids this by draining into its own channel with a
  `select { default: }` drop ([server.go#L515-L528](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/hub/route/server.go#L515-L528)).
  The core must do the same: one internal subscriber that drains and drops,
  then fans out to clients itself.
- `log` also writes every event to stdout through logrus. That `init()` cannot
  be turned off, but under systemd it lands in the journal, which is fine
  ([log.go#L18-L26](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/log/log.go#L18-L26)).
- `log.Fatalln` calls `os.Exit`.

**Smoke run [verified]:** `/tmp/mihomo-embed-size/main.go` ran as uid 1000 with
TUN off, DNS off and a `select` group. It called `ParseWithBytes` →
`ApplyConfig(cfg, true)` → `ReCreateTun(Enable=false)`, listed groups (GLOBAL is
synthesized), called `Set` + `SetSelected`, ran `URLTest` against
`127.0.0.1:1`, then took a `Snapshot` and called `Shutdown`. Exit 0. It created
only `<home>/cache.db`.

## 2. Global state and hot reload

**One instance per process [verified].** Almost all state is package-level:

- `tunnel.proxies/providers/rules/mode`
  ([tunnel.go#L40-L75](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/tunnel/tunnel.go#L40-L75)).
- `listener.tunLister/LastTunConf`.
- `resolver.DefaultResolver`.
- `statistic.DefaultManager`, created in `init()` together with a ticker
  goroutine ([manager.go#L12-L26](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/tunnel/statistic/manager.go#L12-L26)).
- `C.Path`, `dialer.DefaultInterface/RoutingMark`, `geodata` URLs and the
  cachefile singleton.

There is no handle type to instantiate twice.

What `ApplyConfig` does, in order
([executor.go#L84-L123](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/hub/executor/executor.go#L84-L123)) **[verified]**:

1. It takes the package mutex `mux`. Concurrent calls serialize, and reloads are
   safe against each other.
2. It calls `tunnel.OnSuspend()`. While suspended, `isHandle` drops every new
   TCP/UDP connection that is not INNER
   ([tunnel.go#L270-L273](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/tunnel/tunnel.go#L270-L273)).
3. It swaps proxies, rules, sniffer, hosts, general settings, DNS (and patches
   the fake-IP mapper from the old one), NTP, listeners, TUN, iptables and
   tunnels.
4. It sets `OnInnerLoading`, then **synchronously** runs `loadProvider` for proxy
   providers, `updateProfile` and `loadProvider` for rule providers. HTTP
   providers download here (unlimited concurrency on 386/amd64/arm64, 1 on
   mips/mipsle, 5 elsewhere). Then it runs `runtime.GC()` and `OnRunning()`.

   **New connections therefore fail for as long as provider fetches take.**
   This is a visible reload blackout when provider files are missing or stale.
5. It runs `updateUpdater`: an external-UI auto-download if `external-ui` is set
   ([executor.go#L374-L382](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/hub/executor/executor.go#L374-L382)).

Preserved across reloads **[verified]**:

- Existing connections (`statistic` is untouched; they keep their old outbound).
- Traffic totals.
- Fake-IP mappings.
- The TUN, if its config is `Equal`.
- Selections, through the cachefile when `store-selected` is on.

Old proxies and providers are not explicitly closed. They are released by
finalizers after `runtime.GC()`
([provider.go#L220](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/adapter/provider/provider.go#L220)).

Library-hostile behaviour **[verified]**:

- `ApplyConfig` has **no error return**. Sub-step failures are only logged.
- `updateIPTables` calls **`os.Exit(2)`** if `iptables.enable` is set and setup
  fails ([executor.go#L469-L483](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/hub/executor/executor.go#L469-L483)).
  The core should reject `iptables.enable: true` before applying.
- `config.Parse` is **not pure**. `ParseRawConfig` temporarily applies the new
  `General` to the live globals (via a `//go:linkname` into `executor`) and rolls
  it back after parsing. This is so geodata/rule-provider parsing sees the new
  URLs ([config.go#L635-L639](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/config/config.go#L635-L639),
  [executor.go#L384-L391](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/hub/executor/executor.go#L384-L391)).
  It does not take `executor`'s mutex. **Parsing (including a "validate only"
  dry run) concurrently with `ApplyConfig` races on globals.** The core must
  serialize parse+apply under its own lock.
- Because of that linkname, `config` cannot link without `hub/executor`
  [inferred].
- Parsing can **download** geodata (see below). A "validate" call is not
  side-effect free.
- `executor.Shutdown()` only closes the TUN, cleans tproxy iptables and stores
  the fake-IP pool. It does not close other listeners, the DNS server, providers
  or the cachefile DB. It is meant to run right before process exit, not as a
  reusable "stop".

Process-global paths **[verified]**
([path.go](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/constant/path.go#L28-L69)):

- `C.Path` is computed at package init. It uses `$HOME/.config/mihomo`, falls
  back to `$XDG_CONFIG_HOME/mihomo` if that is missing, and reads
  `SKIP_SAFE_PATH_CHECK` and `SAFE_PATHS` from the environment.
- Call **`C.SetHomeDir(dir)` before anything else**. `C.SetConfig` only matters
  for `executor.Parse()`; it is not needed with `ParseWithBytes`.
- Relative provider/geodata/UI paths resolve against the home dir.
  `IsSafePath` confines rule-provider paths, `external-ui` and TLS key paths to
  home dir + `SAFE_PATHS`.
- `cache.db` is `<home>/cache.db`
  ([path.go#L189-L191](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/constant/path.go#L189-L191)).
  It is opened lazily by a `sync.Once` with mode **0666** (before umask), never
  closed, and uses a bbolt file lock with a 1 s timeout
  ([cache.go#L80-L106](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/component/profile/cachefile/cache.go#L80-L106)).
  Set `UMask=0077` in the unit so this file stays owner-only.
- Geodata files live in the home dir as `GeoIP.dat`, `GeoSite.dat`,
  `geoip.metadb`/`Country.mmdb` and `ASN.mmdb`.
- Other environment knobs read at runtime: `DISABLE_NFTABLES`,
  `DISABLE_EMBED_CA`, `DISABLE_SYSTEM_CA`, and `QUIC_GO_DISABLE_*` (written by
  `updateExperimental`).

Where mihomo fetches remote content by itself (the allowlist surface)
**[verified]**:

- **Geodata on demand.**
  - Any `GEOIP`, `GEOSITE` or `IP-ASN` rule, or a DNS `fallback-filter.geoip`
    (on by default when `fallback` is set), calls `geodata.InitGeoIP/
    InitGeoSite/InitASN`.
  - Those functions download from `geox-url` when the file is missing **or fails
    verification** ([init.go#L71-L193](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/component/geodata/init.go#L71-L193),
    [geoip.go#L217](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/rules/common/geoip.go#L217),
    [config.go#L1570-L1577](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/config/config.go#L1570-L1577)).
  - Default URLs are `github.com/MetaCubeX/meta-rules-dat/releases/download/latest/…`
    ([config.go#L583-L587](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/config/config.go#L583-L587)).
- **Geodata auto-update.** If `geo-auto-update` is set, the host must call
  `updater.RegisterGeoUpdater()`, as `main.go` does. It is not automatic in
  `ApplyConfig`.
- **Proxy-providers and rule-providers** of `type: http`. They download in
  `loadProvider` during `ApplyConfig` and then refresh on their `interval`
  (`resource.NewHTTPVehicle`).
- **`external-ui`.** `AutoDownloadUI` fetches `external-ui-url` (default
  metacubexd zip) when the directory is empty
  ([update_ui.go#L51-L89](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/component/updater/update_ui.go#L51-L89),
  [config.go#L599](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/config/config.go#L599)).
- **Not data downloads, but outbound traffic:**
  - Default DNS servers (`114.114.114.114`, `223.5.5.5`, `doh.pub`, …) apply when
    the YAML omits them
    ([config.go#L520-L529](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/config/config.go#L520-L529)).
  - The `dns/system.go` `init()` hard-codes `114.114.114.114` / `8.8.8.8` as the
    system-resolver fallback.
  - NTP is off by default.
- **Core self-update** (`component/updater/update_core.go`) is reachable only
  from `hub/route`'s `/upgrade`. It is never linked when only `executor` is
  imported.

Other `init()` side effects of importing the needed packages **[verified]**:

- `statistic` starts a 1 s ticker goroutine.
- `log` configures logrus to stdout, and the observable goroutine starts.
- `dns/system.go` installs a system resolver.
- `config` wires `dns.ParseNameServer`.
- `adapter/outbound/tailscale.go` registers a tailscale hostinfo hook and calls
  `envknob.SetNoLogsNoSupport()`.
- `common/pool` replaces sing's allocator.
- `component/ca` embeds a CA bundle. The repo's copy dates from 2024-03; official
  CI overwrites it with the runner's bundle before building
  ([build.yml#L229-L233](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/.github/workflows/build.yml#L229-L233)).
  An embedder therefore ships the older embedded bundle plus the system pool.

`main.go`'s defensive `net.DefaultResolver` override and `automaxprocs` are in
package `main`, so an embedder does not get them.

## 3. Build tags, flags, Go version, `replace`

Official release build **[verified]**
([build.yml#L35](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/.github/workflows/build.yml#L35),
[#L161-L174](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/.github/workflows/build.yml#L161-L174),
[#L196-L204](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/.github/workflows/build.yml#L196-L204),
[#L245](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/.github/workflows/build.yml#L245),
[Makefile#L15-L17](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/Makefile#L15-L17)):

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local GOOS=linux GOARCH=amd64 GOAMD64=v3 \
go build -v -tags "with_gvisor" -trimpath \
  -ldflags "-extldflags --static \
            -X 'github.com/metacubex/mihomo/constant.Version=${VERSION}' \
            -X 'github.com/metacubex/mihomo/constant.BuildTime=${BUILDTIME}' \
            -w -s -buildid="
```

- The asset named `mihomo-linux-amd64-*` is **`GOAMD64=v3`**. There are also
  `-v1`, `-v2` and `-v3` variants, plus `-go120`/`-go123` builds for old
  kernels and OSes.
- The toolchain is `setup-go` with `go-version: '1.26'` from
  `github.com/MetaCubeX/go/releases` (MetaCubeX's patched Go). The official
  binary's build info says `go1.26.8`, `-tags=with_gvisor`, `GOAMD64=v3`.
- Embedders set `constant.Version` / `BuildTime` the same way. The path stays
  `github.com/metacubex/mihomo/constant`.

Optional tags in `constant/features` **[verified]**: `with_gvisor`,
`with_low_memory`, `no_fake_tcp`, `no_tailscale`, `no_zerotier`, `no_easytier`
and `cmfa` (the Android build).

What `with_gvisor` buys **[verified]**:

- Without it, sing-tun's `NewGVisor` and `NewMixed` return `gVisor is not
  included in this build`
  ([stack_gvisor_stub.go](https://github.com/MetaCubeX/sing-tun/blob/27e28c30de350cff262e32d9ed92b87215a0ff7c/stack_gvisor_stub.go),
  [stack.go#L41-L63](https://github.com/MetaCubeX/sing-tun/blob/27e28c30de350cff262e32d9ed92b87215a0ff7c/stack.go#L41-L63)).
- The **`tailscale` outbound** is compiled out
  ([tailscale.go#L1](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/adapter/outbound/tailscale.go#L1)).
- `system` and the new default **`mips`** stack (metacubex/mipstack) still work.
  The latest commit makes `Mips` the default TUN stack
  ([config.go#L544-L547](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/config/config.go#L544-L547),
  [tun.go#L8-L20](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/constant/tun.go#L8-L20)).
- So a no-gVisor build works with default configs, but **user configs that say
  `stack: gvisor` or `stack: mixed` would fail TUN start**, and tailscale nodes
  would fail to parse.
- Cost: 10.7 MB raw / 3.1 MB zstd-19 (section 5).

Go version **[verified]**:

- mihomo's `go.mod` says `go 1.20` with no `toolchain` line. The proxy reports
  `"GoVersion": "1.20"`.
- `go1.27.1` builds it unmodified. The scratch module's `go.mod` was written as
  `go 1.27.1`.

**Embedding gotcha 1: `replace` is not inherited [verified].** mihomo's `go.mod`
has exactly one `replace`:

```
// for https://github.com/golang/protobuf/issues/1704
replace google.golang.org/protobuf => github.com/metacubex/protobuf-go v0.0.0-20260306035419-7ceee0674686
```

- Go applies `replace` only in the main module.
- Without the line, the scratch build **still compiles and runs**, but it uses
  upstream protobuf v1.34.2. golang/protobuf#1704 is a `MethodByName("Methods")`
  call that disables linker method dead-code elimination on Go ≥ 1.26.
- Measured cost: **+10.3 MB** with `with_gvisor` (70.8 vs 60.5 MB) and +6.2 MB
  without it.
- The embedding `go.mod` must copy this line. CI should diff mihomo's `replace`
  block against ours on every bump.

**Embedding gotcha 2: GODEBUG defaults are not inherited [verified].** The
module's `go` line sets the binary's default `GODEBUG`.

- The official binary carries `DefaultGODEBUG=asynctimerchan=1,…,tls10server=1,
  tls3des=1,tlsrsakex=1,x509negativeserial=1,…` (Go 1.20 semantics).
- The embed binary built with `go 1.27.1` has the modern defaults.
- Adding `godebug default=go1.20` to the embedding `go.mod` reproduced exactly
  the `DefaultGODEBUG` of a full mihomo built with the same toolchain, at zero
  size cost (checked with `go version -m`).
- Not matching risks behaviour drift against old TLS/x509 servers.
  **[inferred]** The fork TLS stacks (`metacubex/tls`, `utls`) are unaffected,
  but `crypto/tls` / `crypto/x509` call sites are affected.

**Bump bot [verified/inferred]:**

- mihomo publishes proper semver tags (`v1.19.32`; the Go proxy lists 109
  versions) under the module path `github.com/metacubex/mihomo`, so
  Dependabot's `gomod` ecosystem and Renovate's `gomod` manager both track it
  normally. The non-semver tag `Prerelease-Alpha` is ignored.
- Neither bot will update **our** copied `replace` when mihomo changes theirs
  [inferred]. Add a CI step that fails if they differ.
- The sing-tun version is transitively pinned by mihomo; do not pin it
  separately.
- FlClash is the precedent for the alternative. It pins a **fork** via git
  submodule + `replace github.com/metacubex/mihomo => ./Clash.Meta`
  ([go.mod#L5](https://github.com/chen08209/FlClash/blob/4b59eca853778d4e7be3de26589252889c899bc5/core/go.mod#L5)).
  That is because it uses fork-only APIs absent upstream (`listener.StopListener`,
  `statistic.DefaultManager.NowTraffic`, `tunnel.ProvidersSnapshot`)
  ([hub.go#L99-L101](https://github.com/chen08209/FlClash/blob/4b59eca853778d4e7be3de26589252889c899bc5/core/hub.go#L99-L101),
  [hub.go#L271-L272](https://github.com/chen08209/FlClash/blob/4b59eca853778d4e7be3de26589252889c899bc5/core/hub.go#L271-L272)).
  Omihomo needs none of them.

## 4. resolvectl when hosted in-process

Builds on the
[2026-08-18 setcap note](2026-08-18-mihomo-setcap-parity.md), now re-checked at
sing-tun v0.4.27. **[verified]**

- `sing_tun.New` → `tun.New(options)` → `NativeTun.configure()` adds addresses,
  brings the link up, sets routes and rules, then **unconditionally** calls
  `t.setSearchDomainForSystemdResolved()`
  ([tun_linux.go#L49-L77](https://github.com/MetaCubeX/sing-tun/blob/27e28c30de350cff262e32d9ed92b87215a0ff7c/tun_linux.go#L49-L77),
  [#L303-L412](https://github.com/MetaCubeX/sing-tun/blob/27e28c30de350cff262e32d9ed92b87215a0ff7c/tun_linux.go#L303-L412)).
  The only exits before that call are the `EPERM` "unprivileged" early returns.
- That function returns early only if `EXP_DisableDNSHijack` is set, or
  `exec.LookPath("resolvectl")` fails, or there is no DNS server address.
  Otherwise, in a goroutine with errors ignored, it runs
  `resolvectl domain <tun> ~.`, `resolvectl default-route <tun> true` and
  `resolvectl dns <tun> <addr>`. `Close()` runs `resolvectl revert <tun>`
  synchronously
  ([tun_linux.go#L1038-L1077](https://github.com/MetaCubeX/sing-tun/blob/27e28c30de350cff262e32d9ed92b87215a0ff7c/tun_linux.go#L1038-L1077),
  [#L437-L444](https://github.com/MetaCubeX/sing-tun/blob/27e28c30de350cff262e32d9ed92b87215a0ff7c/tun_linux.go#L437-L444)).
  It is a child process (`exec`), not D-Bus from inside our process. `resolvectl`
  itself talks D-Bus to systemd-resolved.
- **What triggers it:** any Linux TUN start without `file-descriptor`.
  - mihomo never sets `DNSServers`, so the address is `Inet4Address[0].Next()`.
  - mihomo always sets `Inet4Address` to a /30 taken from `fake-ip-range`
    (default `198.18.0.1/30` → DNS `198.18.0.2`)
    ([config.go#L1686-L1704](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/config/config.go#L1686-L1704)).
  - So it fires regardless of `auto-route`, `strict-route` and `dns-hijack`.
- **Can it be disabled?** Not through mihomo.
  - sing-tun has `EXP_DisableDNSHijack`, commented "For library usages"
    ([tun.go#L117-L118](https://github.com/MetaCubeX/sing-tun/blob/27e28c30de350cff262e32d9ed92b87215a0ff7c/tun.go#L117-L118)).
  - mihomo's `tun.Options` literal does not set it
    ([server.go#L387-L422](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/listener/sing_tun/server.go#L387-L422)).
  - Practical switches **[inferred]**: make `resolvectl` unresolvable on the
    daemon's `PATH`, or `InaccessiblePaths=/usr/bin/resolvectl` in the unit. Both
    make `LookPath` fail silently.
  - Disabling it means systemd-resolved keeps using the uplink DNS. With
    `auto-route`, those queries still enter the TUN and are caught by
    `dns-hijack` (default `0.0.0.0:53`)
    ([config.go#L548](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/config/config.go#L548)).
- **Under a root system service [inferred, #36 owns it]:**
  - polkit is moot for root.
  - The sandbox must allow `fork`/`execve` of `/usr/bin/resolvectl`, so no
    `SystemCallFilter=~@process` and no `NoExecPaths=/`.
  - It needs `AF_UNIX` in `RestrictAddressFamilies` and reach to
    `/run/dbus/system_bus_socket`.
  - The child inherits the unit's sandbox.
  - Failures are silent (`_ =`), so a broken sandbox shows up only as "DNS not
    routed to TUN", never as an error.
- **Other exec paths:**
  - Auto-redirect tries the in-process nftables netlink backend first. It falls
    back to `exec` of `iptables`/`ip6tables` only if nftables is unavailable or
    `DISABLE_NFTABLES=1`
    ([redirect_linux.go#L45-L113](https://github.com/MetaCubeX/sing-tun/blob/27e28c30de350cff262e32d9ed92b87215a0ff7c/redirect_linux.go#L45-L113),
    [redirect_iptables.go#L269-L283](https://github.com/MetaCubeX/sing-tun/blob/27e28c30de350cff262e32d9ed92b87215a0ff7c/redirect_iptables.go#L269-L283)).
  - Auto-redirect also starts a redirect server on `0.0.0.0`/`::` on an
    ephemeral port.
  - An OpenWrt `fw4` hook runs only if an `inet fw4` table exists.
  - mihomo's own `iptables:` (tproxy) block execs `iptables`/`ip`. It is a no-op
    unless enabled
    ([tproxy_iptables.go#L26-L35](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/listener/tproxy/tproxy_iptables.go#L26-L35),
    [#L115-L126](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/listener/tproxy/tproxy_iptables.go#L115-L126)).
- A related #36 lever: `tun.file-descriptor` makes sing-tun skip `open` and
  `configure` entirely (no routes, rules or resolvectl). That is the
  Android-style handoff.

## 5. Binary size (measured)

Scratch module `/tmp/mihomo-embed-size` (outside the repo). `main.go` imports
`hub/executor`, `listener`, `tunnel`, `tunnel/statistic`,
`adapter/outboundgroup`, `component/profile/cachefile`, `log` and `constant`,
and calls each API from section 1. Commands, run with `go1.27.1 linux/amd64`:

```sh
go mod init example.com/embedsize
go get github.com/metacubex/mihomo@v1.19.32 && go mod tidy
go mod edit -replace google.golang.org/protobuf=github.com/metacubex/protobuf-go@v0.0.0-20260306035419-7ceee0674686
go mod tidy
CGO_ENABLED=0 GOAMD64=v3 go build [-tags with_gvisor] -trimpath -ldflags '-s -w -buildid=' -o out/<name> .
gzip -6 -c out/<name> | wc -c;  zstd -3 -T0 -c … | wc -c;  zstd -19 -T0 -c … | wc -c
```

| Binary (linux/amd64 v3, stripped) | Raw | gzip -6 | zstd -3 | zstd -19 |
|---|---:|---:|---:|---:|
| **Embed, with_gvisor, protobuf replace** | **60.49 MB** | 22.47 MB | 21.66 MB | 17.37 MB |
| Embed, no gvisor, replace | 49.83 MB | 18.50 MB | 17.78 MB | 14.30 MB |
| Embed, with_gvisor, **no replace** | 70.75 MB | 25.58 MB | 24.60 MB | 19.60 MB |
| Embed, no gvisor, no replace | 56.07 MB | 20.50 MB | 19.66 MB | 15.71 MB |
| Full mihomo `main`, same toolchain and flags | 61.16 MB | 22.75 MB | 22.02 MB | 17.59 MB |
| Official `mihomo-linux-amd64-v1.19.32` (go1.26.8) | 61.99 MB | 22.85 MB (asset `.gz` = 22,846,526 B) | 22.08 MB | 17.60 MB |

Notes:

- The official `mihomo-linux-amd64-v1.19.32.pkg.tar.zst` asset is 22,091,475 B.
- All builds succeeded. `godebug default=go1.20` did not change the size.
- The daemon's own code (socket server, JSON) will add little compared with
  this.

## 6. License

mihomo's license identity **[verified]**:

- `LICENSE` (674 lines, md5 `1ebbd3e3…`) is **byte-identical** to
  `https://www.gnu.org/licenses/gpl-3.0.txt` (diff empty). The copy in the
  `v1.19.32` module zip is the same.
- It contains only the license text. There is no copyright notice and no
  "either version 3 … or (at your option) any later version" grant. The
  "How to Apply These Terms" appendix is a template, not a grant.
- It has been in place since `e5284cf6` (2019, Dreamacro/clash, "License: use
  GPL 3.0").
- README: "This software is released under the GPL-3.0 license."
  ([README.md#L97-L101](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/README.md#L97-L101)).
- No `.go` file carries a license header or SPDX tag (grep for `GNU General
  Public`, `SPDX-License`, `any later version` found nothing).
- **Conflicting upstream signal:** MetaCubeX's own deb/rpm/pacman metadata says
  `--license GPL-3.0-or-later`
  ([.fpm_systemd#L4](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/.github/release/.fpm_systemd#L4)).
- GitHub's detector reports `GPL-3.0` for the `Meta` branch (`/license?ref=Meta`).
  GitHub's **repo-level** license shows **MIT**, but only because the repo's
  default branch is now `main`. `main` is an unrelated Python project ("pydantic
  model for Honkai: Star Rail… Mihomo API", no common history with `Meta`). It
  says nothing about the Go core.
- Downstream labels disagree too: Arch `extra/mihomo` uses `GPL3`, AUR
  `mihomo-bin` uses `GPL-3.0`, and AUR `mihomo-git` uses `MIT` (wrong).

GPLv3 §14 ([LICENSE#L563-L577](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/LICENSE#L563-L577)):

> If the Program specifies that a certain numbered version of the GNU General
> Public License "or any later version" applies to it, you have the option of
> following … any later version … If the Program does not specify a version
> number of the GNU General Public License, you may choose any version ever
> published by the Free Software Foundation.

How that applies:

- The "any version" fallback needs the Program to name **no** version. mihomo
  names version 3: the LICENSE is v3 and the README says "GPL-3.0". So the
  fallback does not apply.
- No "or any later version" statement appears anywhere in the source.
- So the literal reading is version 3 only. The SPDX list (3.29.0) marks the
  bare `GPL-3.0` id GitHub uses as a deprecated alias of "GNU General Public
  License v3.0 only".
- **Best-fit SPDX for mihomo: `GPL-3.0-only`.** The fpm metadata is the one
  contrary signal. It is packaging metadata, not a grant in the program, so it
  is noted but not decisive.
- This is not legal advice. The conservative choice is "only".

What GPLv3 requires of Omihomo when it statically links mihomo and ships binaries
(AUR `-bin` from a GitHub release) **[verified text; application inferred]**:

- **Whole combined work under GPLv3.** The daemon binary is one program, so
  Omihomo's own code must be GPLv3-compatible (#37 already decides this).
  Choosing `GPL-3.0-or-later` for Omihomo's own files would also be compatible.
  The combined binary is then effectively GPLv3.
- **Corresponding Source (§1, §6).** It means "all the source code needed to
  generate, install, and … run the object code and to modify the work, including
  scripts" ([LICENSE#L134-L149](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/LICENSE#L134-L149)).
  - Every linked Go module is part of the work, not a System Library.
  - §6(d) allows offering source "from a designated place", and source may sit
    on a different server, "provided you maintain clear directions… Regardless
    of what server hosts the Corresponding Source, you remain obligated to ensure
    that it is available for as long as needed"
    ([LICENSE#L275-L286](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/LICENSE#L275-L286)).
  - A public release tag with `go.mod`/`go.sum` plus build scripts gives
    directions to the dependency sources. It relies on proxy.golang.org and
    upstream repos staying available, which we do not control **[inferred]**.
  - mihomo itself avoids that dependency: every release attaches `vendor.tar.gz`
    (`go mod vendor`) and `toolchain.tar.gz`
    ([build.yml#L304-L308](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/.github/workflows/build.yml#L304-L308);
    the v1.19.32 assets are 17.6 MB and 67.7 MB).
  - Omihomo should do the same: attach `go mod vendor` output (or a source
    tarball that includes `vendor/`) to each release that ships a binary.
- **Notices (§4, §5).** Keep license and warranty notices intact
  ([LICENSE#L195-L220](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/LICENSE#L195-L220)),
  and install the GPLv3 text. For a CLI/daemon without an interactive UI,
  "Appropriate Legal Notices" are not mandatory, though a `--version`/about line
  is good practice.
- **§7 naming clause.** The README adds: "any downstream projects not affiliated
  with `MetaCubeX` shall not contain the word `mihomo` in their names" (added
  `ccc3f84d`, 2025-02-08).
  - §7(e) permits "Declining to grant rights under trademark law for use of some
    trade names"
    ([LICENSE#L379-L380](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/LICENSE#L379-L380)).
  - Other "further restrictions" may be removed by recipients (§7 ¶3, [L388-L392](https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/LICENSE#L388-L392)).
  - Whether this clause is enforceable is a legal question. As a plain upstream
    request it applies to the name **Omihomo** and the binary/package names.
  - **Flag for the maintainer.** This is outside #37's scope but blocks nothing
    technical.

Transitive licenses. These come from a module-level scan of all **126 modules
linked** into the with_gvisor embed binary (`go version -m`), checking each
module root in the module cache. `go-licenses report` was also run; it agreed
where it could classify, but missed root-only licenses in 64 packages.
**[verified]**

| License | Count | Notable |
|---|---:|---|
| BSD-3-Clause | 45 | golang.org/x/*, protobuf (+ `PATENTS`), miekg/dns, restls-client-go |
| MIT | 38 | quic-go fork, logrus, … |
| Apache-2.0 | 15 | **metacubex/gvisor**, nftables, netlink, klauspost/compress, go-iptables (**NOTICE**), metacubex/wazero (**NOTICE**), go.yaml.in/yaml/v3 (**NOTICE**) |
| GPL-3.0 | 14 | mihomo, **sing, sing-tun, sing-mux, sing-quic, sing-shadowsocks(2), sing-vmess, fswatch** (all *GPL-3.0-or-later* by notice), sing-wireguard, mieru, randv2, sina-ghaderi/poly1305 + rabaead (verbatim text) |
| MPL-2.0 | 4 | **mipstack** (the default TUN stack), zerotier-go (**NOTICE**), yamux (+BSD), hashicorp/golang-lru |
| BSD-2-Clause | 5 | |
| LGPL-3.0 | 1 | easytier-go |
| ISC / CC0 | 1 / 1 | maxminddb-golang / Yawning/aez |
| **GPL-2.0** | 1 | **rasky/go-lzo** (used by `transport/openvpn`) |
| none at root | 1 | metacubex/chacha. Its files say "governed by a license … in the LICENSE file" (aead/chacha, MIT), but the module ships no LICENSE |

Points for #37:

- **NOTICE files that must be redistributed** (Apache-2.0 §4(d) / attribution):
  `coreos/go-iptables`, `metacubex/wazero`, `go.yaml.in/yaml/v3`, and
  `metacubex/zerotier-go` (an MPL attribution note).
- MPL-2.0 files stay under MPL. Shipping them in a GPLv3 binary is allowed:
  MPL-2.0 §3.3 Larger Work + GPL compatibility **[inferred; MPL text not
  fetched]**. Keep their notices and point to their source.
- **go-lzo is labelled GPLv2.** Its README says it shares the license of the
  original LZO. The GPLv2 text in the module carries no "only/or later"
  statement, and the Go files have no headers. If it were GPL-2.0-only it would
  be incompatible with GPLv3. That is an **upstream mihomo** issue: official
  mihomo links it too. I did not verify upstream LZO's "or later" wording.
- Practical approach: generate a `THIRD_PARTY_NOTICES` file at release time
  (for example `go-licenses save`/`report` plus the module-root scan above) and
  install it with the package.

## 7. Precedent: FlClash `core/`

FlClash embeds mihomo in-process **[verified, read only]**:

- It pins its own mihomo fork as a submodule, branch `FlClash`, with a
  `replace` to `./Clash.Meta`.
- It calls `constant.SetHomeDir`, `executor.ParseWithBytes`, `hub.ApplyConfig`
  (the controller stays optional via `route.ReCreateServer`), its own
  `patchSelectGroup` with `ForceSet`, `listener.ReCreateTun` and
  `executor.Shutdown`
  ([common.go#L141-L184](https://github.com/chen08209/FlClash/blob/4b59eca853778d4e7be3de26589252889c899bc5/core/common.go#L141-L184),
  [#L381-L428](https://github.com/chen08209/FlClash/blob/4b59eca853778d4e7be3de26589252889c899bc5/core/common.go#L381-L428)).
- It wraps apply in its **own** mutex.
- It keeps the selected map in the host, not only in mihomo's cachefile.
- It disables `ntp.write-to-system` where the platform forbids it.
- When parsing fails it falls back to `config.ParseRawConfig(config.DefaultRawConfig())`
  so the listeners keep serving.

## Implications for #40 and #37

**#40: core package shape.** Keep every mihomo import inside `internal/core`.
Suggested surface:

```go
type Core interface {
    Apply(yaml []byte) error              // lock → reject iptables.enable → force Tun.Enable from state → ParseWithBytes → ApplyConfig(cfg, true) → check GetTunConf()
    SetTUN(on bool) error                 // lock → GetTunConf(); Enable=on → ReCreateTun → verify GetTunConf().Enable == on
    Groups() []Group                      // tunnel.Proxies() filtered by outboundgroup.ProxyGroup; Now(), Proxies(), Type()
    Proxies() []Proxy                     // tunnel.Proxies(), Providers(); DelayHistory/LastDelayForTestUrl
    Select(group, name string) error      // SelectAble.Set + cachefile.Cache().SetSelected
    DelayTest(ctx, name, url string, expected string) (uint16, error)            // C.Proxy.URLTest
    GroupDelayTest(ctx, group, url string, expected string) (map[string]uint16, error) // ProxyGroup.URLTest
    Connections() *statistic.Snapshot    // or a copied DTO; never leak mihomo types
    CloseConnection(id string); CloseAll()
    Traffic(ctx) <-chan TrafficSample    // own 1 s ticker over DefaultManager.Now()/Total()
    Logs(ctx, level) <-chan LogLine      // ONE internal log.Subscribe drained with drop-on-full, fanned out by us
    Close()                               // ReCreateTun(off) then executor.Shutdown() at process exit only
}
```

Rules for the implementation:

- Call `C.SetHomeDir(<state dir>)` once at startup, before any other mihomo
  call.
- Never import `hub` or `hub/route`. Do not call `updater.RegisterGeoUpdater`
  unless geo auto-update is a product feature.
- Serialize parse+apply under one core lock, because `config.Parse` mutates
  globals.
- Expect a traffic blackout while providers load on reload. Pre-fetching
  provider files in the daemon, which also suits the remote-content allowlist,
  shortens it.
- Allowlist or rewrite remote content before applying:
  - `geox-url` and pre-seeded geodata, so mihomo never downloads on its own;
  - `proxy-providers` / `rule-providers` of `type: http`;
  - strip `external-ui*` and `external-controller*`.
- Run the unit with `UMask=0077`, since `cache.db` is created 0666 minus umask.

`go.mod`:

```
require github.com/metacubex/mihomo v1.19.32
replace google.golang.org/protobuf => github.com/metacubex/protobuf-go v0.0.0-20260306035419-7ceee0674686
godebug default=go1.20   // match official mihomo runtime behaviour
```

Build and release:

- Build with `CGO_ENABLED=0 go build -tags with_gvisor -trimpath -ldflags "-s -w
  -buildid= -X github.com/metacubex/mihomo/constant.Version=v1.19.32"`.
- Keep `with_gvisor` so user configs with `stack: gvisor|mixed` and tailscale
  nodes work. It costs about 3 MB compressed.
- Use `GOAMD64=v1` for the AUR `-bin` unless the package targets v3 only.
- Dependabot `gomod` (or Renovate) on `github.com/metacubex/mihomo` works. Add a
  CI check that our `replace` block equals mihomo's at the bumped version.

resolvectl:

- Expect `resolvectl` child processes on every TUN up/down. #36 must allow
  `execve` of `/usr/bin/resolvectl` and D-Bus `AF_UNIX`, or deliberately hide
  `resolvectl`. Failures are silent.

**#37:**

- Use **`GPL-3.0-only`** to match mihomo's LICENSE + README. That is the
  literal §14 reading; upstream's fpm `or-later` metadata is the one contrary
  signal.
- Install `LICENSE` (GPLv3 text) and a generated `THIRD_PARTY_NOTICES`. It must
  include the four NOTICE files above, the MPL-2.0 notices, and the MIT text for
  `metacubex/chacha`.
- Attach `go mod vendor` source to every release that ships a binary.
- Separately, raise the README's "no `mihomo` in downstream names" clause with
  the maintainer, since the project is called Omihomo.

## Could not verify

- The binaries were not run as root, and no TUN was created. The resolvectl
  findings come from code reading, not observation.
- Whether upstream LZO (and therefore go-lzo) is GPL-2.0-or-later. The module
  contains only the GPLv2 text and a README line.
- How the bump bots handle the copied `replace` line. That they do not update it
  is inferred, not tested.
- MPL-2.0/GPLv3 compatibility is stated from general knowledge; the MPL text was
  not fetched.
- Whether the §7 naming clause is enforceable. That is a legal question.
- Scratch artifacts for reproduction: `/tmp/mihomo-embed-size` (module, `out/`
  binaries) and `/tmp/emb-research` (mihomo, sing-tun and FlClash clones,
  `modlic.tsv`, `modclass.tsv`, `licenses.csv`).
