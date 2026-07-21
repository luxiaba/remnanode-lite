# Remnawave Node 2.8.0 Behavioral Contract Baseline

[Back to developer documentation](README.md) · [Architecture](../architecture.md)

This document records the project's compatibility baseline for official Node `2.8.0`. When that contract changes, add a new version document or describe the migration explicitly. Do not silently replace the pinned source recorded here.

## Evidence Boundary

This document and `internal/contract` jointly define the project's compatibility target. The sole official code baseline is:

- Repository: `https://github.com/remnawave/node.git`
- Version: `2.8.0`
- Commit: `596f015a5c8f876dc9a9d61b6cb78d35bd8e379b`
- Panel version used for integration acceptance: `2.8.1` (independent of the project version)

Route methods come from the four official controllers. Request and response shapes come from the Zod schemas under `libs/contract/commands`; application errors come from `libs/contract/constants/errors` and `HttpExceptionFilter`. `internal/contract/official-source-manifest.json` records the SHA-256 of every evidence blob and the 26 method/path/controller-decorator entries extracted from the source.

The extractor reads raw Git objects at the pinned commit, with replace refs disabled, and never trusts the index, worktree, or `HEAD`. It parses `ROOT`, `REST_API`, controller and route constants, and NestJS HTTP decorators. It also walks the Git tree to find controllers and modules, confirms that `main.ts` bootstraps with `NestFactory.create(AppModule)`, checks static imports and supported metadata, binds each route decorator to one exported controller class, and verifies that a reachable module registers every controller. The two internal-controller paths must exactly match the exclusions on the same `setGlobalPrefix` call.

Unsupported syntax fails closed, including conditional expressions, spreads, unknown dynamic modules, and composite, aliased, or qualified decorators. Supporting new syntax requires a reviewed parser change. The two independently derived official route inventories must both match the local Go contract exactly.

The extractor is deliberately narrow: it does not translate arbitrary TypeScript or Zod into Go schemas. Request and response constraints remain manually reviewed contract data. Manifest digests expose changes in the official evidence, while schema boundary tests and `contract-probe` verify the implementation. A contract upgrade still requires review of the official diff; successful extraction alone does not prove complete semantic equivalence.

The plugin `config` schema is not defined in the Node repository. It comes from `@remnawave/node-plugins@0.4.5`, pinned by the official `package.json` and `package-lock.json`. The manual audit used npm tarball `node-plugins-0.4.5.tgz`, SHA-1 `3bfc3988278790ec40a93d6e6169f893c31bf62d`, and SHA-512 integrity `sha512-r9Lce/l/kHQATNhWbcutApFSJ5hH/Yu6Kv0+/qjpUDIEa1+DFb54Q8IwuvqWzxxbGkG9oO0cAeN4busBzz0a5Q==`. Go plugin validation follows `build/backend/models/node-plugins.schema.js` from that archive. Current source-evidence tests do not download or verify this external tarball; see [External Plugin Schema Evidence](testing.md#external-plugin-schema-evidence) for the review procedure.

## Common Semantics

- The external API uses mutual TLS; the official minimum is TLS 1.3.
- Every `/node` route uses an RS256 Bearer JWT. On authentication failure, the official implementation destroys the socket and returns no HTTP body.
- The official implementation likewise destroys the socket for an unknown route.
- Endpoints with request DTOs validate with Zod. Unknown object fields are stripped rather than rejected.
- DTO validation failure returns HTTP 400 with `statusCode=400`, `message="Validation failed"`, and `errors=[...]`.
- Every success response is HTTP 200 with a top-level `{ "response": ... }` envelope.
- Known application errors include `timestamp`, `path`, `message`, and `errorCode`; the relevant errors currently come from A001-A017.
- Unmapped Nest exceptions use the generic `statusCode`, `message`, and optional `error` response.
- This project may enforce smaller request limits for resource protection, but every deviation must be explicit and observable and must reject input before side effects occur.

## Go API Boundary Implementation

All 20 endpoints with request DTOs pass through `internal/nodeapi`. The decoder accepts exactly one JSON document, preserves Zod's unknown-field stripping, and produces a uniform validation response for missing fields, wrong types, union discriminants, UUIDs and IPs, enums, nullable/default behavior, and `minItems`.

The six official routes without a request DTO accept an empty body. If such a request declares `application/json` and carries content, that content must still be one valid object or array. Malformed JSON is rejected before side effects instead of being ignored.

`internal/httpserver` owns post-authentication capacity control, decoding, validation, cross-Xray/plugin lifecycle coordination, command mapping, and response envelopes. Statistics, user-handler, and plugin services no longer receive `http.ResponseWriter`, `*http.Request`, or decode JSON themselves. Xray configuration is decoded once into a map and passed directly to Manager without an intermediate `RawMessage` and second unmarshal.

Transport tests inject counting spies for providers, the connection dropper, plugin service, and Xray Manager. They require zero calls for every invalid request. Valid requests pass through the real dispatcher and are then checked against an independent official response schema.

## Go Xray Lifecycle Implementation

Lifecycle behavior follows the official `src/modules/xray-core/xray.service.ts`, `xray-process.service.ts`, `xray.module.ts`, and application shutdown hooks. At Node startup, the cached Xray state is offline; no old Panel configuration is restored from disk. `healthcheck` reads only cached state. Panel stop owns both core shutdown and plugin cleanup. The Go implementation confirms that core has stopped before resetting plugins, so a failed stop cannot remove filtering early. Application shutdown follows the same order.

Manager uses one explicit lifecycle state instead of multiple booleans that could form illegal combinations:

| Current state | Event | Next state | Commit semantics |
| --- | --- | --- | --- |
| `stopped` | Accept start | `starting` | Retain only pending configuration for the new process to fetch |
| `running` | Accept a start that requires restart | `starting` | Reap the old process before publishing new pending configuration |
| `starting` | gRPC ready and process still alive | `running` | Atomically commit active configuration, hashes, and inbound tags |
| `starting` | Cancellation, timeout, spawn, or process failure | `stopped` / `stopping` | Return to stopped after successful cleanup; if termination fails, retain process ownership and stopping state for a later stop retry |
| `starting` | Stop | `stopping` | Invalidate the operation epoch, cancel start, and transfer process ownership to stop |
| `running` | Stop or natural exit | `stopping` / `stopped` | Reap the process and clear configuration and hash state |

Manager's internal lifecycle mutations are protected by the operation mutex; state publication and ownership transfer occur under the manager mutex. The HTTP coordinator grants shared leases to start and has two independent handler slots that bound simultaneously retained configurations. A second concurrent start can therefore enter Manager and immediately receive the official-compatible `Request already in progress` response.

Stop, plugin sync/recreate, user mutations, and reset-capable statistics use exclusive leases. A waiting exclusive operation prevents a later start from overtaking it. Every accepted lifecycle operation gets a monotonically increasing `operationEpoch`, so an old start cannot overwrite a later stop. Every actual rw-core process separately owns a unique `process epoch + abstract socket` identity.

Exactly one `Wait` goroutine reaps each successfully spawned child. On Linux, rw-core starts in a dedicated process group with a parent-death signal. Normal-stop SIGINT, timeout escalation to SIGKILL, and fallback cleanup after a natural leader exit all target the entire process group. The parent-death signal protects only the group leader directly. If Node or its supervisor is itself force-killed, the project does not promise automatic cleanup of every descendant; operational recovery is to restart the service or host, or recreate the container.

Process-level tests cover pending-to-active commit boundaries, concurrent starts, start/stop interleavings, cancellation, startup timeout, exits before and after readiness, concurrent and repeated stop, and SIGINT-to-SIGKILL escalation. Linux tests add dedicated-process-group, whole-group signaling, and descendant-cleanup coverage. Route tests cover shared start admission, exclusive stop/plugin waits, canceled waits, the Panel-stop `Stop -> ResetPlugins` order, and retention of plugin snapshots and nft rules when Stop fails.

## Go Plugin and nftables Implementation

Plugin behavior follows the official `plugin.service.ts`, `nft.service.ts`, `plugin-state.service.ts`, torrent-blocker state and webhook handlers, and `@remnawave/node-plugins@0.4.5`. Each change first builds an immutable plan from validated configuration, resolving shared lists, ASN data, the connection-drop whitelist, effective torrent state, and firewall rules in one pass.

Enable and update operations apply the firewall before coordinating Xray. Disable, cleanup, and destructive recreate coordinate Xray before resetting the firewall. A snapshot is committed only after both sides succeed. Failure never publishes a mismatched new state, and rollback-capable paths replay the previous firewall plan so the same Panel request can be retried safely.

Initialize, sync, reset, block, unblock, and recreate are serialized through a capacity-one, context-cancelable operation gate. The HTTP application layer uses a shared-start/exclusive-mutation lifecycle coordinator with fixed lock order `Xray lifecycle lease -> Plugin operation gate -> Manager`, preventing plugin snapshots from changing while core reads startup configuration. Any future internal entry point that bypasses HTTP must reuse this coordinator.

Disabling torrent blocking without `includeRuleTags` hot-removes `RW_TB_OUTBOUND_BLOCK` without stopping an online core. A healthy `recreate-tables` rebuilds only nftables; core stops only when recovery from a degraded firewall makes torrent blocking effective again.

Webhook admission waits for space in a bounded queue of 64 entries, not directly on the operation gate. That wait shares the internal request's 30-second deadline. One worker later acquires the gate and applies nft/report side effects. If capacity does not recover, the request is canceled, or the service closes, the endpoint returns `503 + Retry-After`; it never reports an unadmitted event as successful. Collect drains reports atomically while holding the State lock.

nftables initialization is separate from Go object construction. With no `CAP_NET_ADMIN`, or after nft initialization failure, valid plugin configuration is still accepted, but ingress, egress, and torrent enforcement remain unavailable and torrent state is not injected into Xray incorrectly. Reset replaces only the plugin snapshot and preserves torrent reports not yet collected by Panel. Recreate replays the current committed filtering plan rather than creating empty tables.

Close first sets an irreversible mutation-admission fence and stops the webhook worker. Previously admitted mutations may finish; every new mutation is rejected. Gate wait, nft-table deletion, and worker join share the caller's deadline and are additionally capped by the service at 15 seconds. Cleanup failure retains the committed snapshot and permits only a later Close retry; normal business operations never reopen.

The nft backend replaces private IPv4/IPv6 tables and filtering elements in one atomic `nft -f` transaction, batches block operations, and removes addresses from both torrent and ingress dual-stack sets on unblock. Process exit stops rw-core before deleting `remnanode` and `remnanode6`; listener failure enters the same cleanup path. The Linux network-namespace integration test exercises initialization, two plan replacements, dual-stack block, repeated block/unblock, recreate, and close. This gate has passed on Linux arm64 kernel 6.8 with nftables; the amd64 test binary has also been cross-compiled and is included in CI.

## Go User, Connection, and Statistics Implementation

Every user mutation passes through a cancelable serial gate. Before reading inbound or IP state, each add/remove acquires a lease bound to the current rw-core `process epoch + abstract socket`. Handler RPCs, connection cleanup, and the local inbound-hash commit all run under that lease. `Start` and `Stop` wait for its release, so one mutation cannot cross rw-core processes.

If cleanup fails, the operation does not continue by adding a replacement account for that user. Any failure in a batch returns `success=false` and the first specific error. Separate remote RPCs cannot form a true distributed transaction, so already successful earlier operations are not represented as rolled back. The local state nevertheless never advances ahead of rw-core, and the same Panel request remains safe to retry.

Connection dropping normalizes and deduplicates IP addresses, skips the whitelist, and rejects invalid, unspecified, loopback, link-local, multicast, IPv4 broadcast, and local-interface addresses. Each batch enumerates IPv4 and IPv6 sockets separately through `NETLINK_SOCK_DIAG` and verifies every `SOCK_DESTROY` acknowledgement. Only `ENOENT` is treated as idempotent success. Missing `CAP_NET_ADMIN`, failed user-IP lookup, or any failed destruction returns a real `success=false`. CI creates real TCP connections in an isolated Linux network namespace and verifies their closure; this gate has passed on Linux arm64 kernel 6.8.

`get-users-ip-list` prefers rw-core's single `GetUsersStats` extension RPC. After an older core returns `UNIMPLEMENTED`, the capability result is cached and the implementation falls back to at most eight fixed workers instead of one goroutine per online user. Every Handler/Stats unary RPC shares a default deadline of at most five seconds; health probing uses three seconds. An earlier caller deadline and cancellation remain effective. Legacy batch queries use one total budget rather than renewing the timeout for each user.

## Go Resource-Budget Implementation

The resource design keeps the official HTTP contract while placing explicit limits on retained and temporary data. The decoded Xray tree and canonical JSON exist only during startup. Once rw-core is ready, Manager retains only hashes, inbound tags, and runtime state. Torrent reports use a bounded 1,024-entry ring. zstd input, window size, decoder concurrency, request bodies, and gRPC responses are all bounded.

The complete Xray Go module was replaced with a minimal protobuf wire client calibrated against the official generated types. Five account types, Handler requests, Stats messages, and deterministic golden-wire tests jointly pin compatibility.

With `LOW_MEMORY=1`, the public `/node` server defaults to a 16 MiB request-body limit and the Go runtime receives a 180 MiB managed-memory soft limit. Explicit `BODY_LIMIT_MB` accepts `1..1024`; invalid, negative, or overflowing values fail process startup instead of falling back silently. The internal Unix webhook retains its independent 8 KiB fixed limit. Debian and Alpine installers enable low-memory mode automatically when whole-machine memory is no more than 512 MiB.

Production init reads only `/etc/remnanode/node.env`; it does not fall back to a service-writable working directory. The configuration must be a regular, non-symlink file of at most 1 MiB, 4,096 lines, and 256 assignments. Configuration and Secret files are checked and bounded-read through the same descriptor opened with `O_NOFOLLOW|O_NONBLOCK`. systemd and OpenRC do not export the complete configuration environment; `GOMEMLIMIT` and version overrides are validated and applied by the same Go parser.

The real-rw-core `v26.6.27` gate at 1 CPU, 448 MiB, and no swap covers a 1k-user start, unchanged sync, 50k-user restart, hot add/remove, and statistics RPCs. Its dated M6 engineering cgroup peak was 143.9 MiB. This baseline predates the frozen `v2.8.0` candidate and is not current-candidate runtime evidence. Reproduction conditions and per-stage measurements are in [`resource-budget.md`](resource-budget.md).

## Go Transport, System, and Supply-Chain Implementation

The public server requires TLS 1.3 or later and disables Go's automatic HTTP/2 negotiation to preserve the official connection-handling model. An invalid JWT, unknown route, or wrong HTTP method closes the underlying connection instead of returning an enumerable 401/404/405 body. Request headers are limited to 64 KiB. Real TLS client tests cover normal connection reuse and connection closure after authentication or unknown-request failures.

systemd and OpenRC run under the dedicated `remnanode:remnanode` account. Configuration is `root:remnanode 0640`; state and log directories are `remnanode:remnanode 0750`. The service receives only `CAP_NET_ADMIN` and `CAP_NET_BIND_SERVICE`. systemd also narrows the bounding set to those capabilities and enables `NoNewPrivileges`, read-only system paths, namespace/syscall/address-family restrictions, `448 MiB` memory, zero swap, 1 CPU, and 256 tasks. Alpine 3.22 measurements for `supervise-daemon` showed `CapInh/Prm/Eff/Amb=0x1400` and `NoNewPrivs=1`; an `nft` child launched by the service could create the private table.

Project assets live under `/usr/local/lib/remnanode` and `/usr/local/share/remnanode`; the project does not take ownership of generic Xray paths. Release archives, rw-core zips, custom cores, and ASN data must pass SHA-256, structure, and version checks before installation. The audited digest for pinned rw-core `v26.6.27` cannot be overridden.

An upgrade backs up the binary, service definition, support assets, `node.env`, and optional rw-core assets. If the refreshed service or port check fails, every item is restored. Fault injection with bad service definitions on Ubuntu/systemd and Alpine/OpenRC verified digest and runtime-state restoration. Full-uninstall tests also verified that unrelated same-named processes remain running and generic Xray files remain untouched.

The dated M7 systemd/OpenRC and bad-service rollback observations above are engineering baselines. They were not produced from the frozen `v2.8.0` candidate and do not count as its runtime acceptance evidence.

Whole-process shutdown shares one 25-second application budget rather than restarting a timeout for each component. HTTPS and Unix intake, log rotation, and background version probing receive cancellation first. rw-core gets up to five seconds for SIGINT plus five seconds for SIGKILL. Only after core is confirmed stopped may plugins use the remaining budget to remove private nft tables. A transient core or plugin cleanup error is retried once within the same deadline.

Public `xray/stop` also serializes start and stop and resets plugins only after confirmed core termination. A failed Stop preserves rules and the snapshot. systemd supplies a 30-second TERM grace period; OpenRC supplies a `TERM/30/KILL/5` outer fallback. Deadline or cleanup failure returns an aggregate error and must not be logged as a successful graceful exit.

## Route Inventory

The table summarizes only core constraints. Executable schemas in `internal/contract/official_schemas.go` define complete type, nullable, enum, UUID, IP, date, and array-length constraints.

| Method | Path | Request core | Response core | Primary side effect or error |
| --- | --- | --- | --- | --- |
| POST | `/node/xray/start` | `internals.hashes`, `xrayConfig`; `forceRestart` defaults false | `isStarted`, nullable `version/error`, node and system information | Start or replace rw-core and replace configuration/hash state; failure still returns HTTP 200, `isStarted=false`, and nullable `error`. RN-001 is an official not-ready log diagnostic, not a response field |
| GET | `/node/xray/stop` | No body | `isStopped` | Stop rw-core and clean up plugin state and rules |
| GET | `/node/xray/healthcheck` | No body | `isAlive`, cached status, nullable Xray version, Node version | Read cached and process state only |
| POST | `/node/stats/get-user-online-status` | `username` | `isOnline` | Query online status; SDK errors degrade to false |
| POST | `/node/stats/get-users-stats` | `reset` | `users[]` traffic | `reset=true` clears counters; A011 |
| GET | `/node/stats/get-system-stats` | No body | Nullable `xrayInfo`, plugin and system statistics | Query rw-core and host; A010 |
| POST | `/node/stats/get-inbound-stats` | `tag`, `reset` | Inbound traffic | May clear counters; A012 |
| POST | `/node/stats/get-outbound-stats` | `tag`, `reset` | Outbound traffic | May clear counters; A013 |
| POST | `/node/stats/get-all-outbounds-stats` | `reset` | `outbounds[]` | May clear counters; A016 |
| POST | `/node/stats/get-all-inbounds-stats` | `reset` | `inbounds[]` | May clear counters; A015 |
| POST | `/node/stats/get-combined-stats` | `reset` | `inbounds[]`, `outbounds[]` | May clear counters; A017 |
| POST | `/node/stats/get-user-ip-list` | `userId` | `ips[]` with ISO date-time | Query and reset one user's IP statistics |
| GET | `/node/stats/get-users-ip-list` | No body | `users[].ips[]` | Query known-user IP statistics |
| POST | `/node/handler/add-user` | `data[]` union, `hashData.vlessUuid` | `success`, nullable `error` | Add a user and update inbound hash |
| POST | `/node/handler/remove-user` | `username`, UUID hash | `success`, nullable `error` | Read IPs, then remove every related inbound user/hash; drop connections only after all removals succeed |
| POST | `/node/handler/get-inbound-users-count` | `tag` | `count` | Query rw-core; A014 |
| POST | `/node/handler/get-inbound-users` | `tag` | `users[]` | Query rw-core; A014 |
| POST | `/node/handler/add-users` | `affectedInboundTags[]`, `users[]` | `success`, nullable `error` | Add users in a batch and replace affected hashes |
| POST | `/node/handler/remove-users` | `users[]`, each with userId/UUID | `success`, nullable `error` | Read IPs and remove related inbound users/hashes per user; batch-drop connections only for successful removals |
| POST | `/node/handler/drop-users-connections` | Non-empty `userIds[]` | `success` | Query IPs, then terminate host connections |
| POST | `/node/handler/drop-ips` | Non-empty `ips[]` | `success` | Terminate host connections; official schema does not require each element to be a valid IP |
| POST | `/node/plugin/sync` | Nullable `plugin`; non-null includes config/UUID/name | `accepted` | Replace or clear plugin state while coordinating nftables and rw-core |
| POST | `/node/plugin/torrent-blocker/collect` | No body | Complete `reports[]` | Atomically take and clear the report queue |
| POST | `/node/plugin/nftables/block-ips` | `ips[]` of valid IP plus numeric timeout | `accepted` | Add timed blocks and drop connections |
| POST | `/node/plugin/nftables/unblock-ips` | Array of valid IPs | `accepted` | Remove blocks from plugin tables |
| POST | `/node/plugin/nftables/recreate-tables` | No body | `accepted` | Rebuild and repopulate plugin nftables tables |

## Request Unions

`data[]` for `handler/add-user` accepts only these discriminants:

- `trojan`: tag, username, password
- `vless`: tag, username, uuid, flow; flow must be `xtls-rprx-vision` or an empty string
- `shadowsocks`: tag, username, password, cipherType, ivCheck
- `shadowsocks22`: tag, username, password
- `hysteria`: tag, username, password

`inboundData[]` for `handler/add-users` uses the same five types; VLESS additionally requires flow. Every `userData` must include userId, hashUuid, vlessUuid, trojanPassword, and ssPassword.

## Current Known Differences

The previously recorded TLS/socket and system supply-chain differences are closed. There is currently no known static P1/P2 difference in the `/node` contract.

The release-blocking runtime profile for `v2.8.0` is `docker-production-smoke-v2`. The exact candidate image digest must run through the production Compose template on a recorded native `x86_64`/`amd64` host, report the expected version, connect to a real Panel 2.8.1, carry real proxy traffic, record host capacity plus cgroup memory and PID observations, and remain healthy with zero OOM kills and zero restarts. Host size is not an admission limit; the container must still have exactly 448 MiB memory, no additional container swap, 1 CPU, and 256 PIDs.

The `whole-host-512mib-runtime`, `arm64-production-runtime`, `native-systemd-install`, `native-openrc-install`, repeated 50,000-user load, 24-hour soak, and fault/rollback profiles are deferred and non-blocking. The acceptance manifest and release risks must show that they were not run, rather than reporting them as passed.

Like the official deployment, Docker Compose uses host networking and `NET_ADMIN`, while retaining the capability to bind low ports. Go Manager directly owns the rw-core lifecycle, so the official two-process s6 runtime structure does not need to be copied. systemd and OpenRC remain equivalent native deployment entry points.

Both maintained production Compose templates use `remnanode-lite` for the
service, container, and hostname. They interpolate the same explicit runtime
mapping from `.env`, apply production defaults, and reject a missing or empty
`SECRET_KEY` before container creation; `.env` is not injected wholesale.

Runtime `dump-config` is an accepted deferred difference. Manager retains the complete canonical configuration only while rw-core starts, releases it after readiness, and then has `CurrentConfigJSON` return `{}`. This is a memory tradeoff for 512 MiB nodes and does not change the `/node` or rw-core startup contract. Restoring the diagnostic later requires a bounded design; a second large resident configuration copy is not acceptable.

## Local Verification

Run the normal executable contract tests with:

```bash
go test ./internal/contract
```

This always compares the checked-in machine-extracted manifest with local `OfficialRoutes`, so manual method/path drift fails even without an official source repository.

Verify the pinned official source evidence as well with:

```bash
go run ./cmd/contract-source-check \
  -source /tmp/remnawave-node-official-2.8.0-codex
```

To update a pinned contract, confirm `OfficialNodeCommit` and the evidence-directory inventory, then pass `-write` explicitly to regenerate the manifest. Review the manifest diff and rerun the normal contract tests. The checkout may be dirty or point `HEAD` elsewhere because the pinned commit object is the only input; verification fails if the repository does not contain that object.

Run the isolated firewall test on a Linux acceptance host with root, unshare, and nft:

```bash
sudo env "PATH=$PATH" REMNANODE_NFT_INTEGRATION=1 \
  go test ./internal/plugin -run '^TestNFTManagerInNetworkNamespace$' -count=1 -v
```

The tests establish that all 26 machine-extracted official methods and paths match both the local contract and the real dispatcher. They cover every valid request fixture, missing fields, wrong types, extra fields, unknown union discriminants, UUID/IP/`minItems` constraints, complete success schemas from real Go handlers, and the uniform official error schema.

## Black-Box Differential Entry Point

List routes and their default safety class:

```bash
go run ./cmd/contract-probe -list
```

Prepare a Panel client certificate signed by the same CA and use the first target as the official baseline:

```bash
export REMNANODE_CONTRACT_CA=/secure/ca.pem
export REMNANODE_CONTRACT_CERT=/secure/panel-client-cert.pem
export REMNANODE_CONTRACT_KEY=/secure/panel-client-key.pem

go run ./cmd/contract-probe \
  -token-file /secure/panel.jwt \
  -target official=https://127.0.0.1:2222 \
  -target candidate=https://127.0.0.1:3222
```

If the certificate contains only DNS names while a target uses an IP address, also pass `-server-name <certificate-name>`. The probe has no option to bypass certificate verification.

By default it performs only 11 non-destructive requests: health checks, statistics with `reset=false`, and read-only inbound-user queries. It compares status, response category, application error code, schema, and `SemanticSHA256` after removing dynamic fields. It records raw body size and SHA-256 for audit purposes, but does not use either to decide semantic equality and does not compare machine metrics, traffic values, or timing. Reports contain neither JWTs nor raw response bodies.

Start/stop, user mutations, connection dropping, IP-statistics reset, report draining, and nftables operations require both explicit `-routes` and `-allow-mutating` and must run only in an isolated acceptance environment.
