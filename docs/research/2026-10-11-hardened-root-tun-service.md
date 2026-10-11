# Ticket #36: Minimum privileges and sandbox for a root TUN service

Research date: 2026-10-11

## Verdict

These conclusions come from source at pinned commits: mihomo v1.19.32,
metacubex/sing-tun v0.4.27, systemd v261.2, Linux v7.2 and dbus-broker v37.
The machine runs the same versions. No privileged test was run.

- **TUN, auto-route, nftables auto-redirect and routing marks need only
  `CAP_NET_ADMIN`.** The project should also grant `CAP_NET_RAW`. It costs
  little: sing-tun tries raw ICMP sockets first. Without the capability it
  falls back to unprivileged ICMP sockets, which this machine allows through
  `ping_group_range`. `CAP_NET_BIND_SERVICE` is not needed. Omihomo listens
  on 7890 and 7899, DNS hijack is an nftables DNAT, and sing-tun's own
  listeners use port 0.
- **The resolved check uses only the caller's euid, and capabilities play no
  part.** The claim "systemd 261 ignores capabilities" is correct. The
  capability shortcut was removed in v256, and on AF_UNIX D-Bus it never
  worked. Any uid-0 caller passes before polkit is asked, whatever its
  bounding set. sing-tun runs `/usr/bin/resolvectl` as a child process, so
  the sandbox must allow it to exec. It needs `AF_UNIX`, `AF_NETLINK` and
  the system bus socket.
- **Looking up processes owned by any non-root account needs two more
  capabilities: `CAP_SYS_PTRACE` and `CAP_DAC_READ_SEARCH`.** This includes
  the owner's own processes. Without both, mihomo's lookup fails, and mihomo
  also discards the uid it got from sock_diag.
- **With `CAP_SYS_PTRACE`, the filesystem sandbox stops being a security
  boundary.** A uid-0 process holding `CAP_SYS_PTRACE` can follow
  `/proc/1/root`. That magic link leads to the host's own writable mounts,
  where uid 0 owns `/etc` without needing any DAC capability. Seccomp cannot
  block this. `ProtectSystem=` and `ProtectHome=` then guard against bugs,
  not against a compromised daemon. The project has to choose:
  1. accept this;
  2. turn process lookup off by default;
  3. move lookup into a helper, which needs a mihomo patch because 1.19.32
     has no hook for non-Android platforms.
- **Settings that conflict:**
  - `PrivateNetwork=`, `NetworkNamespacePath=`
  - `PrivateUsers=`, `PrivatePIDs=`
  - `PrivateDevices=` used as-is
  - `IPAddressDeny=`, `RestrictNetworkInterfaces=`
  - a non-root `User=` or `DynamicUser=`

  Settings that need care:
  - `MemoryDenyWriteExecute=` breaks the EasyTier outbound's JIT. Build
    with the `no_easytier` tag.
  - `ProtectClock=` breaks `ntp.write-to-system`.
  - `ProcSubset=pid` and `ProtectProc=` only matter without process lookup.
  - `SocketBindDeny=`.
- The recommended unit below scores **2.8 "OK"** under
  `systemd-analyze security --offline=yes`. Without the two lookup
  capabilities it scores 2.4. The score does not model the `/proc/1/root`
  escape.

## 1. Capabilities

A uid-0 service gets the whole bounding set as permitted and effective
capabilities. When root execs a program, the file sets count as all ones, so
the new permitted set is the bounding set. For this service,
`CapabilityBoundingSet=` is therefore the capability list.
`AmbientCapabilities=` adds nothing ([capabilities(7)]). The same rule
applies to children it execs (`resolvectl`, and `iptables` if the fallback
is used): they get the bounding set too, and `NoNewPrivileges=` does not
change this, because nothing is gained over what the parent already has.

### TUN, auto-route, nftables, marks: `CAP_NET_ADMIN`

All of this is verified in source:

- sing-tun opens `/dev/net/tun` and calls `TUNSETIFF`
  ([sing-tun tun_linux.go L263-L289][st-tun-open]). Creating a new device
  checks `CAP_NET_ADMIN` ([tun.c L2763][k-tun-iff]). The node is `0666
  root:root` here, so uid 0 needs no DAC override to open it.
- Links, addresses, routes and policy rules go over rtnetlink
  ([st-route]). Any rtnetlink message that is not a GET needs
  `CAP_NET_ADMIN` ([rtnetlink.c L7003][k-rtnl]).
- **Silent failure:** if `LinkSetUp` returns `EPERM`, sing-tun treats the
  process as unprivileged and returns success. No routes or rules are set,
  and `resolvectl` is never called ([st-unpriv]). A missing capability
  therefore looks like a TUN that came up but carries nothing.
- auto-redirect uses the in-process nftables backend first
  ([st-redirect]). nfnetlink rejects every message from a sender without
  `CAP_NET_ADMIN` ([nfnetlink.c L659][k-nfnl]).
- With auto-redirect on, mihomo sets the default routing mark to the output
  mark ([mihomo server.go L530][mh-mark]). Every outbound socket then gets
  `SO_MARK` ([dialer.go L130][mh-dialer], [mark_linux.go][mh-sockopt]).
  `SO_MARK` needs `CAP_NET_RAW` or `CAP_NET_ADMIN`
  ([sock.c L1526-L1533][k-somark]).
- Binding to an interface uses `SO_BINDTOIFINDEX`
  ([sing bind_linux.go L28][sing-bind]). It needs `CAP_NET_RAW` only to
  rebind a socket that is already bound ([sock.c L642][k-bindidx]).
- mihomo's lookup finds sockets with sock_diag (`NETLINK_INET_DIAG`), which
  needs no capability. `CAP_NET_ADMIN` only unlocks some extra bytecode in
  the query ([inet_diag.c L742][k-diag]).

### ICMP: `CAP_NET_RAW`, optional

`ConnectDestination` on Linux first tries a privileged connection (raw
socket). If that fails with `os.ErrPermission`, it retries unprivileged
([ping/destination.go L57-L65][st-ping]). The unprivileged path uses
`SOCK_DGRAM` ICMP sockets ([ping/socket_unix.go][st-ping-sock]). Raw
sockets need `CAP_NET_RAW` ([af_inet.c L321][k-raw]). Datagram ICMP sockets
need the caller's gid inside `net.ipv4.ping_group_range`. systemd's
`50-default.conf` sets that to `0 2147483647`
([sysctl.d L45][sd-sysctl]), and this machine has that value. Keep
`CAP_NET_RAW`: it is cheap, it matches the earlier ticket #2 research, and
it does not depend on the sysctl.

### Privileged ports: `CAP_NET_BIND_SERVICE`, not needed

sing-tun's own listeners all use port 0:

- the redirect server ([redirect_server.go L43][st-rs]);
- the system stack listeners on the TUN address.

DNS hijack is an nftables DNAT, not a socket bound to port 53 (as ticket #2
already found). Add this capability only if the override ever allows a
`dns.listen` or listener port below 1024.

### Process lookup: `CAP_SYS_PTRACE` and `CAP_DAC_READ_SEARCH`

How mihomo looks up a process, verified in source:

1. sock_diag returns the socket's uid and inode.
2. mihomo walks `/proc`, skipping any pid whose `/proc/<pid>` owner differs
   from that uid.
3. It runs `ReadDir("/proc/<pid>/fd")` and `Readlink` on each fd, looking
   for `socket:[inode]`.
4. It runs `Readlink("/proc/<pid>/exe")`
   ([process_linux.go L65-L231][mh-proc]).

The `/proc/net/{tcp,udp}` fallback (L67-L78) only runs on Android.

What the kernel checks:

- **Listing `/proc/<pid>/fd`.** The directory is mode `0500`
  ([base.c L3284][k-fd-dir]). It is owned by the task's euid, or by root if
  the task is not dumpable ([task_dump_owner][k-dump-owner]).
  `proc_fd_permission` is `generic_permission` plus a same-thread-group
  exception ([fd.c L326-L343][k-fdperm]). So root with fsuid 0 needs
  `CAP_DAC_READ_SEARCH` (or `CAP_DAC_OVERRIDE`) to read a directory owned by
  uid 1000 ([namei.c L533-L543][k-genperm]).
- **Reading `fd/N` and `exe` links.** Both use
  `proc_pid_link_inode_operations` ([fd.c L211-L214][k-fdlink],
  [base.c L1853][k-linkops]). That calls `call_proc_get_link` →
  `ptrace_may_access(PTRACE_MODE_READ_FSCREDS)`
  ([base.c L1776-L1797][k-getlink]). `__ptrace_may_access` allows the call
  only if:
  - the caller's fsuid and fsgid match all three of the target's uids and
    gids; or
  - the caller has `CAP_SYS_PTRACE`.

  The commoncap hook then also requires either that the target's permitted
  set is a subset of the caller's effective set, or `CAP_SYS_PTRACE`
  ([ptrace.c L345-L353][k-ptrace], [commoncap.c L164-L186][k-cap-ptrace]).
  Yama's `ptrace_scope=1` only restricts `ATTACH` mode, not `READ`.

So a bounded root service without `CAP_SYS_PTRACE` cannot resolve any
ordinary user's process. It cannot resolve full-capability root processes
either. If the `fd` walk fails, `findProcessName` returns an error. On
non-Android builds `tunnel.go` then drops the uid along with the path
([tunnel.go L351-L366][mh-tunnel]), so `UID` rules stop working as well.
Both capabilities are required. Mihomo needs nothing else here: it reads no
`cmdline` or `status`.

### What `CAP_SYS_PTRACE` costs

This section comes from source and was not tested live.

`/proc/<pid>/root` and `/proc/<pid>/cwd` are the same kind of ptrace-gated
magic link ([base.c L3315-L3316][k-mem-root]). On open,
[`nd_jump_link`][k-jump] continues the path walk from the target's own
`struct path`. For PID 1 that is the host's root, on the host's mounts. The
service's read-only bind mounts and hidden `/home` do not apply there. uid 0
owns `/etc/shadow`, the unit files and so on, so it can write them without
any DAC capability.

`/proc/1/mem` is also writable:

- the file is mode `0600` and owned by root;
- `mem_open` checks `PTRACE_MODE_ATTACH` ([base.c L840-L856][k-mem]),
  which `CAP_SYS_PTRACE` satisfies, and so does Yama scope 1.

seccomp sees only `openat` and `write`, so it cannot block either path.

Partial mitigations:

- The recommended filter blocks the ptrace system calls: `ptrace` and
  `pidfd_getfd` are not in `@system-service`, and `process_vm_writev` is
  removed explicitly.
- `ReadOnlyPaths=/proc` would make `/proc/<pid>/mem` writes fail with
  `EROFS`, because opening for write checks the mount
  ([open.c L861-L908][k-open]). It does not stop `/proc/1/root`.

Nothing in systemd 261 closes the `/proc/<pid>/root` path. With lookup
enabled, a compromised daemon should be treated as root. That is still no
worse than today's setuid-root core.

A helper split would give the daemon only `CAP_NET_*`. A small lookup
service would hold `CAP_SYS_PTRACE` with `PrivateNetwork=yes` and `AF_UNIX`
only. mihomo 1.19.32 calls `process.FindProcessName` directly; its only hook
(`DefaultPackageNameResolver`) runs only after that call succeeds, or on
CMFA builds ([process.go][mh-procgo]). So the split needs a small mihomo
patch.

### Root without DAC capabilities

- Files are fine as long as root owns them. Keep `/var/lib/omihomo`
  root-owned (`StateDirectory=` with mode `0700`). A user-owned state
  directory, as in today's design, would not be writable without
  `CAP_DAC_OVERRIDE`.
- `/dev/net/tun` (`0666`), the D-Bus socket (`0666`), `/proc/<pid>` and
  `/proc/self` need no DAC capability.
- The widget socket: a root-owned socket created with umask `0077` is
  `0600 root`. The owner cannot connect to that, and the bounding set
  above has no `CAP_CHOWN`. Use socket activation instead.
  `omihomo.socket` with `SocketUser=<owner>` and `SocketMode=0600` makes
  PID 1 create the node, and `DirectoryMode=` creates the parent directory
  ([systemd.socket(5)][sd-socket]). The other option is to add
  `CAP_CHOWN` and remove `@chown` from the deny line.

## 2. The resolved uid-0 check (systemd 261.2)

**What sing-tun runs.** In `configure()`, sing-tun looks up `resolvectl` on
`PATH`. It then runs `domain <if> ~.`, `default-route <if> true` and
`dns <if> <addr…>` in a goroutine, and `revert <if>` on `Close()`. It
ignores every error ([tun_linux.go L1038-L1075][st-resolvectl], called at
[L406][st-resolvectl-call] and [L441][st-close]). These are external
processes, so the following are required:

- exec of `/usr/bin/resolvectl` and its libraries in `/usr/lib`;
- a `PATH` that contains `/usr/bin`, which systemd's default `PATH`
  already does;
- `AF_UNIX` for `sd_bus_open_system` ([resolvectl.c L138-L151][sd-rctl-bus]);
- `AF_NETLINK`, because `ifname_mangle` resolves the interface name over
  rtnetlink ([resolvectl.c L155][sd-rctl-ifname], [L2661-L2697][sd-rctl-dns]).

Children inherit the unit's seccomp filter, address-family filter and mount
namespace.

**How resolved authorizes the call.** All of this is verified in source.

- `resolvectl` calls `SetLinkDNS`, `SetLinkDomains`, `SetLinkDefaultRoute`
  and `RevertLink` on resolve1's Manager ([L2816][sd-rctl-dr],
  [L3152][sd-rctl-revert]). The Manager dispatches each to the Link
  handler ([resolved-bus.c L1761-L1782][sd-rbus]).
- Each Link handler calls `bus_verify_polkit_async(message,
  "org.freedesktop.resolve1.set-…")`
  ([resolved-link-bus.c L226-L230][sd-rlink], and similarly at L361, L441
  and L742).
- `bus_verify_polkit_async` is `bus_verify_polkit_async_full` with
  `good_user = UID_INVALID` and no flags ([bus-polkit.h L20-L22][sd-pkh]).
  The function takes no capability argument.
- Unless `POLKIT_ALWAYS_QUERY` is set, it first calls
  `sd_bus_query_sender_privilege(call, /* capability= */ -1)` and returns
  "authorized" if that succeeds, before building any polkit request
  ([bus-polkit.c L537-L583][sd-pk]).
- With `capability < 0`, `sd_bus_query_sender_privilege` asks only for UID
  and EUID. It authorizes when the sender's euid equals the daemon's uid,
  or when the sender is root and the daemon is not
  ([bus-convenience.c L675-L742][sd-priv]). resolved runs as
  `User=systemd-resolve` ([unit L53][sd-resolved-unit]), so the "sender is
  root" branch applies. If networkd owns the link, resolvectl retries
  against networkd, which uses the same helper.
- The sender's EUID comes from the bus: `GetConnectionCredentials` →
  `UnixUserID` → `SD_BUS_CREDS_EUID`
  ([bus-control.c L562-L570][sd-busctl]). dbus-broker fills `UnixUserID`
  from `SO_PEERCRED` taken at connect time ([peer.c L269-L273][dbb-peer],
  [driver.c L1386-L1390][dbb-driver]), and the kernel reports the
  connecting task's euid there ([sock.c L1703-L1713][k-ucred]).

**History.** systemd commit [7b36fb9f][sd-commit] ("drop auth-by-cap dbus
feature", first released in v256) removed the capability parameter. Its
message says capability authentication "never actually" worked on AF_UNIX
transports, because peer capabilities cannot be read without races.

**What this means:**

- Capabilities and the bounding set play no part. A root service with only
  `CAP_NET_ADMIN`, or no capabilities at all, passes as long as its euid
  is 0.
- `NoNewPrivileges=` does not matter: `resolvectl` is not setuid, and it
  inherits euid 0.
- `ProtectSystem=strict` leaves `/run` read-only, but connecting to
  `/run/dbus/system_bus_socket` still works. resolved's own unit combines
  `ProtectSystem=strict` with being on the bus ([unit L39-L42][sd-resolved-unit]).
- `PrivateNetwork=` would hide only abstract sockets; the bus socket is on
  the filesystem. It is ruled out anyway because TUN needs the host netns.
- The D-Bus policy lets anyone send to `org.freedesktop.resolve1`
  (`/usr/share/dbus-1/system.d/org.freedesktop.resolve1.conf`).
- polkit is never consulted, so the four `auth_admin_keep` actions do not
  matter.

## 3. Sandbox options (systemd 261.2)

Cites are to [systemd.exec(5)][sd-exec] and
[systemd.resource-control(5)][sd-rc] at the v261.2 commit.

### Compatible: use these

| Option | Why it is safe here |
| --- | --- |
| `ProtectSystem=strict` | No writes outside the state directory. Important because uid 0 owns `/etc`. Bus connect still works. |
| `ProtectHome=yes` | Nothing reads `/home`. `readlink /proc/<pid>/exe` still returns host paths, because `d_path` ignores mounts it cannot reach ([d_path.c L265-L295][k-dpath]). |
| `PrivateTmp=yes` | No `/tmp` use found. |
| `DevicePolicy=closed`, `DeviceAllow=/dev/net/tun rw` | The eBPF device filter allows only the tun node. systemd's own `systemd-nsresourced.service` does the same ([unit L28-L29][sd-nsres]). |
| `ProtectKernelTunables=yes` | No `/proc/sys` or `/sys` writes in the compiled Linux dependency graph; only reads (see method below). |
| `ProtectKernelModules=yes` | Only blocks explicit loads. Kernel-triggered autoload (tun `char-major-10-200`, nfnetlink subsystems) still happens per the man page. `tun` and `nf_tables` are already loaded here. |
| `ProtectKernelLogs=yes`, `ProtectControlGroups=yes`, `ProtectHostname=yes`, `PrivateIPC=yes` | Unused facilities. |
| `ProtectClock=yes` | Only `ntp.write-to-system` calls `settimeofday` ([ntp time_unix.go][mh-ntp]). Keeping the clock in the override is already the policy, and this enforces it. |
| `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK` | Netlink (route, inet_diag, netfilter), bus, widget socket, traffic. No `AF_PACKET` or other families in the compiled code. |
| `RestrictNamespaces=yes` | Namespace calls in dependencies (`setns` in mdlayher/socket and vishvananda/netns) run only when a netns is configured. |
| `LockPersonality=`, `RestrictRealtime=`, `RestrictSUIDSGID=`, `SystemCallArchitectures=native` | No use. |
| `NoNewPrivileges=yes` | No setuid or file-capability exec is needed. |
| `SystemCallFilter=@system-service`, then `~@privileged process_vm_readv process_vm_writev`; `SystemCallErrorNumber=EPERM` | `@system-service` already leaves out `@debug` (`ptrace`, `pidfd_getfd`) and `open_by_handle_at` ([seccomp-util.c L480-L491, L751-L786, L922-L987][sd-seccomp]). That matters because `CAP_DAC_READ_SEARCH` plus `open_by_handle_at` would escape the mount namespace. `process_vm_*` come in through `@ipc` (L628-L629), so they must be removed explicitly. `EPERM` makes stray calls fail instead of killing a Go process with `SIGSYS`. |
| `UMask=0077`, `StateDirectory=omihomo`, `StateDirectoryMode=0700` | Private, root-owned state. |
| `KeyringMode=` | The default for system services is already `private`. |

### Needs care

| Option | Issue |
| --- | --- |
| `MemoryDenyWriteExecute=yes` | Go itself is fine. The EasyTier outbound runs on wazero, whose default engine JITs with `mprotect(PROT_EXEC)` ([wazero mmap_unix.go L13][wz-mmap]). MDWE blocks that. Build with `-tags no_easytier`, which removes wazero from the build (checked with `go list -deps`). |
| `ProcSubset=pid` | Hides `/proc/sys` and `/proc/net`. Lookup's `/proc/net` fallback is Android-only. Go's `somaxconn` read falls back, `net.Interface.MulticastAddrs` breaks, and the mieru outbound reads `/proc/sys/net/ipv4/tcp_congestion_control`. Optional; test live. |
| `ProtectProc=invisible` / `ptraceable` | hidepid uses `ptrace_may_access` ([base.c L726-L743][k-hidepid]). With `CAP_SYS_PTRACE` it is a no-op; without it, nothing is lost. Useful only in the no-lookup profile. |
| `NoExecPaths=/` with `ExecPaths=/usr/bin/resolvectl /usr/lib <daemon>` | Would also stop the iptables fallback and shells from running. Libraries are mapped from `/usr/lib`. Optional; untested. |
| `ReadOnlyPaths=/proc` | Partly reduces the `CAP_SYS_PTRACE` risk (see §1). Untested together with the `Protect*` settings that mount over `/proc`. |
| `SocketBindDeny=` / `SocketBindAllow=` | sing-tun binds port-0 listeners, and users configure listener ports. A list is possible but fragile. Skip. |
| `ReadWritePaths=` | Not needed: `StateDirectory=` (and `RuntimeDirectory=` if used) are already exempt. |
| `CapabilityBoundingSet=` | See §1. `AmbientCapabilities=` has no effect for uid 0. |
| `SecureBits=` | Leave unset. `noroot` would remove root's implicit capabilities on exec. |
| `RemoveIPC=` | No effect for root ([man][sd-exec-removeipc]). |
| auto-redirect's iptables fallback, mihomo `iptables:` (tproxy) | Both exec `iptables`, `ip` and `sysctl` ([redirect_linux.go L80-L111][st-redirect], [executor.go L468-L474][mh-exec]). Under a root unit the children would get `CAP_NET_ADMIN`, unlike in the setcap design. Writes to `sysctl` and `/run` lock files would fail under this sandbox. Require nftables and keep `iptables.enable` in the override. |

### Conflicts

| Option | Why it breaks the workload |
| --- | --- |
| `PrivateNetwork=`, `NetworkNamespacePath=`, `JoinsNamespaceOf=` (netns) | The TUN, routes, rules, nftables and sock_diag would all live in a private netns. The host's traffic would never reach them. |
| `PrivateUsers=` (any mode) | Capabilities apply only inside the unit's user namespace. `ns_capable(init_user_ns, CAP_NET_ADMIN)` fails for the host netns ([man][sd-exec-privusers]). |
| `PrivatePIDs=yes` | `/proc` shows only the unit's own pids, so lookup breaks. |
| `PrivateDevices=yes` | The private `/dev` gets only null, zero, full, random, urandom, tty, ptmx, pts, shm, mqueue and hugepages. There is no `/dev/net/tun` ([namespace.c L1312-L1407][sd-privdev]). Adding the node back with `BindPaths=` is unverified. |
| `IPAddressDeny=` / `IPAddressAllow=` lists | An eBPF filter on the unit's own sockets. All proxied and DIRECT traffic starts from the daemon. |
| `RestrictNetworkInterfaces=` | Same reason: traffic leaves through every uplink. |
| `User=` (non-root), `DynamicUser=` | The resolved euid check fails (decided in #30). |
| `ProtectProc=invisible` / `ProcSubset=pid` without `CAP_SYS_PTRACE` | Not a conflict in itself; lookup is already broken in that case. |

Method for the dependency claims: I ran `go list -deps` for `GOOS=linux`
at mihomo v1.19.32. Then I grepped the Go files that would be compiled for
`/proc` and `/sys` paths, for `exec.Command`, and for namespace, bpf,
`ptrace`, `PROT_EXEC` and `settimeofday` calls. A grep shows what is in the
code, not what runs at runtime.

## 4. Recommended unit

This assumes process lookup stays on. The daemon path is a placeholder. The
widget socket comes from a separate `omihomo.socket` unit (§1).

```ini
[Unit]
Description=Omihomo proxy daemon (embedded mihomo)
# Subscription downloads want a network; resolvectl wants resolved.
# Ordering only.
Wants=network-online.target
After=network-online.target systemd-resolved.service

[Service]
Type=exec
ExecStart=/usr/lib/omihomo/omihomod
Restart=on-failure
# No User=: euid 0 is what systemd-resolved authorizes (§2).

# TUN, rtnetlink, nfnetlink, SO_MARK
CapabilityBoundingSet=CAP_NET_ADMIN
# Raw ICMP; unprivileged ICMP fallback exists
CapabilityBoundingSet=CAP_NET_RAW
# Process lookup: fd/exe links, other users' fd directories.
# Drop both if find-process-mode is off.
CapabilityBoundingSet=CAP_SYS_PTRACE CAP_DAC_READ_SEARCH
NoNewPrivileges=yes

# Root-owned private state; root needs no DAC capabilities here.
StateDirectory=omihomo
StateDirectoryMode=0700
UMask=0077

# Read-only OS; uid 0 still owns /etc, so this matters.
ProtectSystem=strict
# exe paths still resolve (d_path)
ProtectHome=yes
PrivateTmp=yes
# Only device the service opens
DevicePolicy=closed
DeviceAllow=/dev/net/tun rw
ProtectKernelTunables=yes
# Kernel-triggered tun/nf_tables autoload unaffected
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
# Also enforces ntp.write-to-system off
ProtectClock=yes
ProtectHostname=yes
PrivateIPC=yes

# Bus, widget, resolvectl, netlink, traffic
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK
RestrictNamespaces=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
LockPersonality=yes
# Requires the daemon to be built with -tags no_easytier
MemoryDenyWriteExecute=yes
SystemCallArchitectures=native
SystemCallFilter=@system-service
# ptrace and pidfd_getfd are already outside @system-service.
SystemCallFilter=~@privileged process_vm_readv process_vm_writev
SystemCallErrorNumber=EPERM

[Install]
WantedBy=multi-user.target
```

The no-lookup profile:

- set `CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW`;
- add `ProtectProc=invisible`, and optionally `ProcSubset=pid`;
- force `find-process-mode: off` in the merge, so mihomo does not attempt a
  lookup that fails on every connection.

## 5. Verify live during implementation

Run these as root once the daemon exists. They mutate networking, so they
were not run here.

1. **Capabilities as granted.** Run `systemd-analyze security
   omihomo.service`. Then read `CapEff` from `grep Cap
   /proc/$(systemctl show -p MainPID --value omihomo)/status` and decode it
   with `capsh --decode=<hex>`. Expect exactly the bounding set above in
   CapPrm and CapEff.
2. **TUN fully configured.** Check `ip -d link show omihomo`,
   `ip rule | grep -c lookup` and `nft list tables`, which should show
   sing-tun's table when auto-redirect is on. Routes and rules must exist:
   the silent "unprivileged" path in sing-tun hides a missing
   `CAP_NET_ADMIN`.
3. **resolved without polkit.** `resolvectl status omihomo` should show the
   DNS server, `~.` and `DefaultRoute: yes`. `journalctl -u polkit -S -5min`
   should show no `resolve1` checks. Then stop the service and confirm
   `resolvectl status omihomo` is gone, which shows the `revert` ran.
4. **Routing marks.** Run `ss -tne state established` and look for
   `fwmark:0x…` on the daemon's outbound sockets when auto-redirect is on.
5. **ICMP.** `ping -c1 1.1.1.1` through TUN should work with and without
   `CAP_NET_RAW` (drop-in), since `ping_group_range` covers the fallback.
6. **Process lookup.**
   - Make a connection as the owner and one as another account (e.g.
     `sudo -u nobody curl …`). Both must show a process path in the flow
     view.
   - Repeat with a drop-in removing `CAP_SYS_PTRACE`: both should go blank.
     Then try removing `CAP_DAC_READ_SEARCH` the same way.
   - Run one program from `/home` to confirm `ProtectHome=` does not hide its
     exe path.
7. **The escape exists; documents the trade-off.**

   ```sh
   sudo systemd-run --wait --pipe -p ProtectSystem=strict \
     -p ProtectHome=yes \
     -p CapabilityBoundingSet='CAP_SYS_PTRACE CAP_DAC_READ_SEARCH' \
     sh -c 'ls /proc/1/root/home && touch /proc/1/root/etc/omihomo-t36'
   ```

   Expect success, then remove `/etc/omihomo-t36`. Without
   `CAP_SYS_PTRACE` the same command should fail with `EACCES`. Also try
   adding `-p ReadOnlyPaths=/proc` and writing `/proc/1/mem` to see what
   that mitigation covers.
8. **Seccomp, address families and MDWE.** Temporarily add
   `SystemCallLog=~@system-service`. Exercise TUN on/off, a subscription
   update and the widget socket, then check the journal for blocked calls
   and `EAFNOSUPPORT`. Load an EasyTier outbound in a build without
   `no_easytier` to confirm MDWE is the reason it fails.
9. **Optional settings.** Try `ProcSubset=pid`, `NoExecPaths=`/`ExecPaths=`
   and `PrivateDevices=yes` with `BindPaths=/dev/net/tun`, each one at a
   time with steps 2–6.

## Open items and uncertainty

- **Product decision:** keep lookup with root-equivalent exposure, default
  it off, or patch mihomo for a lookup helper. Everything else in this
  ticket is settled.
- These are verified in source only, not live:
  - the `/proc/1/root` and `/proc/1/mem` escape;
  - that `ReadOnlyPaths=/proc` blocks the `mem` path.
- These are inferences from grep or from systemd precedent:
  - `ProcSubset=pid`, the `NoExecPaths=` allow-list and `RestrictNamespaces=`
    being harmless;
  - the address-family set being complete;
  - `PrivateDevices=` with `BindPaths=` being unsupported.
- `ProtectKernelModules=` relies on the man page's autoload note and on both
  modules already being loaded here. It was not tested on a machine where
  `tun` is not yet loaded.
- The iptables fallback under this sandbox was not analysed beyond "it
  execs external tools". Treat nftables as required, as ADR-0006 already
  does via `prepare`.

## Sources

Pinned commits:

- mihomo `v1.19.32` = `88dcbf7f`
- sing-tun `v0.4.27` = `27e28c30`
- sing `v0.5.8` = `1dd5bcee`
- systemd `v261.2` = `4925d9f0`
- Linux `v7.2` = `8d3ae592`
- dbus-broker `v37` = `e5b413f5`

The machine runs systemd 261.2-1-arch, linux 7.2.x, mihomo-bin 1.19.32 and
dbus-broker 37.

[capabilities(7)]: https://man.archlinux.org/man/capabilities.7.en#Capabilities_and_execution_of_programs_by_root
[mh-proc]: https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/component/process/process_linux.go#L65-L231
[mh-procgo]: https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/component/process/process.go#L21-L38
[mh-tunnel]: https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/tunnel/tunnel.go#L351-L378
[mh-mark]: https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/listener/sing_tun/server.go#L530
[mh-dialer]: https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/component/dialer/dialer.go#L130
[mh-sockopt]: https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/common/sockopt/mark_linux.go#L6
[mh-exec]: https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/hub/executor/executor.go#L468-L474
[mh-ntp]: https://github.com/MetaCubeX/mihomo/blob/88dcbf7f1614a67c3b36b848ee3592dfa92ada36/ntp/ntp/time_unix.go#L13
[st-tun-open]: https://github.com/MetaCubeX/sing-tun/blob/27e28c30de350cff262e32d9ed92b87215a0ff7c/tun_linux.go#L263-L289
[st-unpriv]: https://github.com/MetaCubeX/sing-tun/blob/27e28c30de350cff262e32d9ed92b87215a0ff7c/tun_linux.go#L360-L364
[st-resolvectl-call]: https://github.com/MetaCubeX/sing-tun/blob/27e28c30de350cff262e32d9ed92b87215a0ff7c/tun_linux.go#L406
[st-close]: https://github.com/MetaCubeX/sing-tun/blob/27e28c30de350cff262e32d9ed92b87215a0ff7c/tun_linux.go#L437-L443
[st-route]: https://github.com/MetaCubeX/sing-tun/blob/27e28c30de350cff262e32d9ed92b87215a0ff7c/tun_linux.go#L921-L943
[st-resolvectl]: https://github.com/MetaCubeX/sing-tun/blob/27e28c30de350cff262e32d9ed92b87215a0ff7c/tun_linux.go#L1038-L1075
[st-redirect]: https://github.com/MetaCubeX/sing-tun/blob/27e28c30de350cff262e32d9ed92b87215a0ff7c/redirect_linux.go#L80-L111
[st-rs]: https://github.com/MetaCubeX/sing-tun/blob/27e28c30de350cff262e32d9ed92b87215a0ff7c/redirect_server.go#L43
[st-ping]: https://github.com/MetaCubeX/sing-tun/blob/27e28c30de350cff262e32d9ed92b87215a0ff7c/ping/destination.go#L57-L65
[st-ping-sock]: https://github.com/MetaCubeX/sing-tun/blob/27e28c30de350cff262e32d9ed92b87215a0ff7c/ping/socket_unix.go#L20-L40
[sing-bind]: https://github.com/MetaCubeX/sing/blob/1dd5bcee2b112766b8f030eb51f3db2f245996ed/common/control/bind_linux.go#L28
[wz-mmap]: https://github.com/metacubex/wazero/blob/9ae6bdcf2a7d/internal/platform/mmap_unix.go#L13
[sd-pkh]: https://github.com/systemd/systemd/blob/4925d9f07fc697efccd98a93046ff535b8832445/src/shared/bus-polkit.h#L20-L22
[sd-pk]: https://github.com/systemd/systemd/blob/4925d9f07fc697efccd98a93046ff535b8832445/src/shared/bus-polkit.c#L537-L583
[sd-priv]: https://github.com/systemd/systemd/blob/4925d9f07fc697efccd98a93046ff535b8832445/src/libsystemd/sd-bus/bus-convenience.c#L675-L742
[sd-busctl]: https://github.com/systemd/systemd/blob/4925d9f07fc697efccd98a93046ff535b8832445/src/libsystemd/sd-bus/bus-control.c#L562-L570
[sd-rlink]: https://github.com/systemd/systemd/blob/4925d9f07fc697efccd98a93046ff535b8832445/src/resolve/resolved-link-bus.c#L226-L230
[sd-rbus]: https://github.com/systemd/systemd/blob/4925d9f07fc697efccd98a93046ff535b8832445/src/resolve/resolved-bus.c#L1761-L1782
[sd-rctl-bus]: https://github.com/systemd/systemd/blob/4925d9f07fc697efccd98a93046ff535b8832445/src/resolve/resolvectl.c#L138-L151
[sd-rctl-ifname]: https://github.com/systemd/systemd/blob/4925d9f07fc697efccd98a93046ff535b8832445/src/resolve/resolvectl.c#L155
[sd-rctl-dns]: https://github.com/systemd/systemd/blob/4925d9f07fc697efccd98a93046ff535b8832445/src/resolve/resolvectl.c#L2661-L2697
[sd-rctl-dr]: https://github.com/systemd/systemd/blob/4925d9f07fc697efccd98a93046ff535b8832445/src/resolve/resolvectl.c#L2816
[sd-rctl-revert]: https://github.com/systemd/systemd/blob/4925d9f07fc697efccd98a93046ff535b8832445/src/resolve/resolvectl.c#L3152
[sd-resolved-unit]: https://github.com/systemd/systemd/blob/4925d9f07fc697efccd98a93046ff535b8832445/units/systemd-resolved.service.in#L39-L53
[sd-nsres]: https://github.com/systemd/systemd/blob/4925d9f07fc697efccd98a93046ff535b8832445/units/systemd-nsresourced.service.in#L28-L29
[sd-seccomp]: https://github.com/systemd/systemd/blob/4925d9f07fc697efccd98a93046ff535b8832445/src/shared/seccomp-util.c#L480-L987
[sd-privdev]: https://github.com/systemd/systemd/blob/4925d9f07fc697efccd98a93046ff535b8832445/src/core/namespace.c#L1312-L1407
[sd-sysctl]: https://github.com/systemd/systemd/blob/4925d9f07fc697efccd98a93046ff535b8832445/sysctl.d/50-default.conf#L45
[sd-exec]: https://github.com/systemd/systemd/blob/4925d9f07fc697efccd98a93046ff535b8832445/man/systemd.exec.xml
[sd-exec-privusers]: https://github.com/systemd/systemd/blob/4925d9f07fc697efccd98a93046ff535b8832445/man/systemd.exec.xml#L2246-L2300
[sd-exec-removeipc]: https://github.com/systemd/systemd/blob/4925d9f07fc697efccd98a93046ff535b8832445/man/systemd.exec.xml#L2821-L2832
[sd-rc]: https://github.com/systemd/systemd/blob/4925d9f07fc697efccd98a93046ff535b8832445/man/systemd.resource-control.xml#L1276-L1360
[sd-socket]: https://github.com/systemd/systemd/blob/4925d9f07fc697efccd98a93046ff535b8832445/man/systemd.socket.xml#L379-L405
[sd-commit]: https://github.com/systemd/systemd/commit/7b36fb9f96fd5c1f63b9f0f9e75194e3e4dd6a8d
[dbb-peer]: https://github.com/bus1/dbus-broker/blob/e5b413f573b2c158bff88370944d571379977b7e/src/bus/peer.c#L269-L273
[dbb-driver]: https://github.com/bus1/dbus-broker/blob/e5b413f573b2c158bff88370944d571379977b7e/src/bus/driver.c#L1386-L1390
[k-tun-iff]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/drivers/net/tun.c#L2763
[k-rtnl]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/core/rtnetlink.c#L7003-L7004
[k-nfnl]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/netfilter/nfnetlink.c#L659
[k-somark]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/core/sock.c#L1526-L1533
[k-bindidx]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/core/sock.c#L642
[k-ucred]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/core/sock.c#L1703-L1713
[k-diag]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/ipv4/inet_diag.c#L742
[k-raw]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/net/ipv4/af_inet.c#L321-L323
[k-fd-dir]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/fs/proc/base.c#L3284
[k-dump-owner]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/fs/proc/base.c#L1862-L1906
[k-fdperm]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/fs/proc/fd.c#L326-L343
[k-fdlink]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/fs/proc/fd.c#L211-L214
[k-linkops]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/fs/proc/base.c#L1853-L1857
[k-getlink]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/fs/proc/base.c#L1776-L1797
[k-hidepid]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/fs/proc/base.c#L726-L743
[k-mem]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/fs/proc/base.c#L840-L856
[k-mem-root]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/fs/proc/base.c#L3314-L3317
[k-genperm]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/fs/namei.c#L521-L564
[k-jump]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/fs/namei.c#L1168-L1194
[k-ptrace]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/kernel/ptrace.c#L305-L372
[k-cap-ptrace]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/security/commoncap.c#L164-L186
[k-open]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/fs/open.c#L861-L912
[k-dpath]: https://github.com/torvalds/linux/blob/8d3ae59288f1e7d58d76558a6ee96d533bc5019f/fs/d_path.c#L265-L295

- capabilities(7), root exec semantics: [capabilities(7)]
- systemd v261.2: [systemd.exec(5)][sd-exec],
  [systemd.resource-control(5)][sd-rc], [systemd.socket(5)][sd-socket],
  [bus-polkit.c][sd-pk], [bus-convenience.c][sd-priv],
  [resolved-link-bus.c][sd-rlink], [resolvectl.c][sd-rctl-bus],
  [seccomp-util.c][sd-seccomp], [namespace.c][sd-privdev],
  [commit 7b36fb9f][sd-commit]
- mihomo v1.19.32: [process_linux.go][mh-proc], [tunnel.go][mh-tunnel],
  [server.go][mh-mark], [executor.go][mh-exec]
- sing-tun v0.4.27: [tun_linux.go][st-resolvectl],
  [redirect_linux.go][st-redirect], [ping][st-ping]
- Linux v7.2: [fs/proc/base.c][k-getlink], [fs/proc/fd.c][k-fdperm],
  [kernel/ptrace.c][k-ptrace], [security/commoncap.c][k-cap-ptrace],
  [net/core/sock.c][k-somark], [drivers/net/tun.c][k-tun-iff]
- dbus-broker v37: [peer.c][dbb-peer], [driver.c][dbb-driver]
- Read-only local checks:
  - `systemctl --version`;
  - `systemctl cat systemd-resolved`;
  - `/dev/net/tun` mode;
  - `lsmod`;
  - `ping_group_range`;
  - Yama `ptrace_scope=1`;
  - `systemd-analyze security --offline=yes` on a draft of the unit above.
