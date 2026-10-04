# Ticket 01 read-gate adoption

2026-10-04. Root explicitly adopted Astra's read-only decision against fixed
`bde5aaa827532552885bfe3faabb35dbbf2326e6`. This resolves eligibility for already
opened Get/GetCommand in AC1/6/7 and ADR0006/0007, without making later full
source closure/physical cleanup/SIGKILL a hidden prerequisite.

1. Read and disclose are separate current decisions. Keep the first read gate,
   then real whole-object I/O outside any transaction. Before returning any body,
   a second original-owner short transaction rechecks read/disclose, exact still
   published version and current policy/record time bounds. The earlier read bound
   cannot be widened. Revocation committed before this final gate refuses;
   revocation after its commit does not retract an already authorized response.
   Query creates no Job/ref, repairs no bytes and changes no publication history.
2. Get's initial admission gate samples trusted time after every actual blocking
   policy/version lock and checks this request's AcceptBefore before every allowed
   observation, including not_found/preparing/failed/metadata mismatch. GetCommand
   rechecks the current durable reader row and trusted Now after blocking facts,
   before found/not_found observations. Current reader expiry must deny independently
   from the envelope. Existing legacy codecs/bridges remain frozen/lossless.
3. AcceptBefore admits this read; it is not a completion deadline. An initially
   admitted object I/O may cross it while finite context/current policy/retention
   still permit disclosure. Final disclosure gate therefore does not reapply the
   envelope cutoff. Caller context and current earlier bounds still apply.

The Astra report was source review, not actual fault evidence. Actual public
red/green results and normal controls are added to ticket-01-evidence.md only after
native execution. The finite object pause is explicitly mechanical; actual bytes
are read and independently verified through the real adapter. Lock waiting uses
real PG row locks and exact blocker observation, never private business oracles.

Root subsequently fully read and adopted Astra's direct-read/association/replay
follow-up against clean intermediate `92d27992f37f0a425621bc75c6d5b4a073fa06a6`.
The implemented narrow matrix is:

| Action | Target | Every already registered direct source |
| --- | --- | --- |
| Get, before I/O and before body disclosure | read + disclose | read + disclose, exact published ref, current retention |
| New Put, including another Command for an existing declaration | save | read + process + save, exact published ref, current retention |
| Original Command digest replay | current original reader and bound subject | no new original Put authorization |

Get uses the current caller and this read's purpose, preserves its earlier stricter
bound and never recursively loads source.Sources or downloads all source bodies.
Process/save/sync permissions do not substitute for or add to the read/disclose
matrix. Lock waits are followed by trusted Now even for absent/mismatched targets.

A new association intersects current policy/source caps with the original record's
CurrentRetainUntil and EffectiveRetainUntil. An actual tightening is saved
monotonically in that same Tx with one revision change. The original accepted
receipt/effective cap/publication deadline/Job/history are retained. An association
charges no new staging capacity; a still eligible failed version may be associated
without applying its old publication deadline. Original replay checks reader after
its real Command lock, retains fixed receipt priority, and ignores old Put admission
cutoff and current save/source permission. New admission checks reader then fresh
Now after every later blocking fact/capacity operation.

The original independent intermediate findings remain immutable in
[ticket-01-spec-initial.md](ticket-01-spec-initial.md) and
[ticket-01-standards-initial.md](ticket-01-standards-initial.md). Their findings and
Astra decisions are requirements/source review, not executed fault evidence.
