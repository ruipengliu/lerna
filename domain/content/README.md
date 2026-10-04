# Content owner

`New` installs one exact logical owner, a durable Repository, immutable Objects,
finite queue/staging/lease/work limits, an admission-fixed publication budget and
retry ceiling, and an explicit worker identity. Missing configuration fails startup.
No domain configuration comes from environment variables.

`Put`, `Get`, `GetCommand` are authenticated 1.2.0 entries. The trusted subject
comes from the caller's host; it cannot be supplied through payload. Put fixes
accepted and the original publication responsibility in one PG transaction;
accepted does not mean published. `Step` resumes bounded original durable work,
with object I/O outside transactions, current policy gates before/after I/O, and
an I/O context bounded by the original Claim lease and publication deadline.
`Get` verifies complete objects before ranges and never repairs. Command progress
is historical publication, separate from current access.

The fixture policy is durable host-installed exact-ref/purpose/subject policy with
independent read, process, save, sync and disclose flags. It is not a production
Grant. Direct sources currently require this owner's published exact versions;
cross-owner sources fail closed. Inherited closure, physical cleanup and delayed
second-holder fencing are separate tickets. No partial Content profile is advertised.
Expired or forbidden records cannot become `gone` without physical cleanup.

`GetCommand10` and `GetCommand11` use the same current durable reader policy and
unchanged old codecs. Observations bridge only when lossless. A new accepted
receipt's mandatory ContentRef/retention cannot fit old codecs and remains
unavailable. Old write methods and schemas are unchanged.

Public real PG/object conformance is in `conformance/component/content_*_test.go`;
run `make test-integration` with the repository's documented dedicated PG DSN,
absolute owned-scope registry and a durable TMPDIR. These paths require actual
services and never skip when missing. Normal reopen is real native Close/Open;
mechanical I/O/Close injections are identified separately from native faults.
