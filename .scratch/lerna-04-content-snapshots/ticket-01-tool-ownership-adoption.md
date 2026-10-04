# Ticket 01 tooling ownership adoption

Root fully read and adopted Astra's Strong narrow extraction against clean
intermediate `92d27992f37f0a425621bc75c6d5b4a073fa06a6`, while the same sole fixer
was repairing five existing entry points' cause/cleanup differences.

`scripts/conformance-ownership.mjs` is the only private conformance ownership
module for the three version-specific contract scripts and two generator scripts.
It owns exact scope dev/inode, scope/parent Sync, complete fsynced ledger ACK,
actual original pid/role bindings, sticky unknown, cause aggregation and final
delete eligibility. Initialization failures carry the original scope. Producer
roles may inherit a group; only detached compiler roles require pid=group.

The original boundedBuild/requireBuild/startRunner still own process creation,
finite stop, actual Wait, pipe closure and group absence. Consumers capture the
original compiler pid from onStart because requireBuild success returns void.
They supply actual owner observations only after native closure. The module neither
spawns nor kills entities nor scans/guesses ownership. Missing exit observation,
ACK failure or first FD Close unknown retains the original exact scope, even if a
different entity or later ACK succeeds. Exit-observation errors are recorded for
finish rather than replacing a primary native error or preventing other cleanup.

Each consumer settles every held runner, then the module aggregates primary,
all native cleanup errors and its own ledger/stat/remove causes. Expected generator
schema refusal remains a business status/stderr assertion; native result.error and
cleanupErrors are combined before those assertions. A physical deletion followed
by failed external removed ACK reports removed=true and keeps both causes.

Mechanical interface tests use actual tracked descriptors and the original bounded
child Wait. Their independent fixture creates/fsyncs/records its exact root before
injecting a diagnostic. The fixture may remove that root only after independent
actual descriptor/child closure and unchanged identity, without clearing the
module's unknown or calling finish again. An absent module registry environment
uses its normal internal ledger; the external removed-ACK test explicitly owns a
second acknowledged fixture. Local session controls may separately retain the
fixture ACK ledger for exact audit. These are injected diagnostics, not native
failure evidence. The suite is included in Makefile's existing Node test entry.

No public registry, process manager, callback DSL, product policy, version codec,
SQL migration or archived receipt was added or changed by this extraction.
