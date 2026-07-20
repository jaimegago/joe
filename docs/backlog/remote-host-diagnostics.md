# Remote host diagnostics — OS-level stats of managed hosts as a future component type

Status: deferred
Priority: later

The `system_info` shared tool was removed (see `sysinfo-tool-removal` in
`docs/project/DECISIONS.md`). It was the one shared diagnostic that inspected the
host Joe itself runs on — disk, memory, load, OS — rather than probing outward from
Joe's network position. It had no target parameter and could only ever report on the
daemon's own machine, which contradicts Joe's capability story: Joe inspects the
managed systems it is pointed at, not the box it happens to be deployed on. So the
local-host read was dropped rather than kept as an unowned shared-tool retrofit.

The legitimate want it gestured at — *OS-level stats of a remote managed host* (disk,
memory, load, process state on a specific server an operator has registered) — is
real but belongs to Joe's component model, not the shared-tool tree. If pursued, it
should be a **future component type** (an SSH-reachable host, or a node-agent
endpoint) with an explicit target coordinate, credentials carried through the
credentials-as-references seam, and a Read/Mutate classification authored at compile
time like every other adapter — so the read is governed and provable in observation
mode, not a component-independent tool that only ever sees the daemon's own host.

Two constraints to carry into that design:

- **A shell transport is Mutate-capable by construction.** An SSH session or any
  general command channel can change the remote system; there is no way to prove a
  free-form shell command is read-only. A remote-host component would need a
  *severely constrained, read-only surface* — a fixed, curated set of stat reads
  (e.g. specific whitelisted commands or a structured node-agent API), never an
  arbitrary-command tool — for the read to be classifiable as `ActionRead` and pass
  the write floor honestly. This is the same reasoning that keeps Joe from ingesting
  unclassifiable external capability (cf. D-0067).
- **Kubernetes node stats already have a governed path.** Where the managed host is a
  Kubernetes node, node/pod resource stats are already reachable through the existing
  `kubernetes` component and its governed accessor — no new transport is needed for
  that case. The remote-host component type is only for hosts *outside* an orchestrated
  cluster.

No design of record, no ADR, no code. Parked here so the removal is a clean deletion
with the deferred idea recorded, not lost.
