# Ticket 01 actual API handoff

This is the implemented first-ticket surface, subject to root's final two-axis
acceptance. It does not advertise the whole Content profile or complete slice04.
The adopted wire decision is [contract-shape-decision.md](contract-shape-decision.md).

## Entrypoints and ownership

- `contract/v1_2`, `contract/gen/go/v1_2` and SDK `@lerna/contract/v1_2` are isolated
  exact 1.2.0 entries. `DecodePut`, `DecodeGet`, `DecodeCommand` check typed methods;
  `DecodeContentResponse`/`EncodeContentResponse` bind full ref and original range;
  `DecodeCommandResponse`/`EncodeCommandResponse` bind original CommandRef/receipt.
  `DeclaredMethods` includes only put/get/command.get; supported methods are empty.
- `domain/content.New(Config)` requires explicit owner, Repository, Objects,
  finite Limits/PublishBudget/retry ceiling/worker. `Put`, `Get`, `GetCommand` take
  finite context, raw closed JSON, and a separately trusted SubjectBinding.
  `Step` resumes one bounded original durable publication synchronously.
- `adapters/postgres/content.Open(pg.Config)` owns an independent Content schema,
  `Migrate`, `MigrationVersions`, sticky `Close`. Its consumers' ports are declared
  in `domain/content/ports.go`; existing Core/Tx/Job/Claim mechanisms are reused.
- `adapters/objectstore/local.Open(absoluteRoot)` owns every native os.Root,
  directory and child file. Always retain a nonnil partial holder accompanying an
  Open error. `CloseContext` bounds pre-drain waiting; a timeout keeps invocation
  ownership, while first native Close errors remain sticky. Physical Sync/Close
  remains owned until real return. Successful Close is required before deleting root.

## Fixed facts versus current eligibility

Command identity and canonical business digest are separate from Content's
`(tenant, owner, content_id, version)` identity. The latter has the explicitly
closed `lerna-content-version-1` canonical tuple and opaque cv-hash Job identity,
with separately persisted tuple/reference fields validated at the storage boundary.
Original receipt wins on identical replay regardless of changed trace/connection.
A new Command may associate with the same exact declaration; altered bytes/hash/
length/media/source set/purpose/original requested retention refuses version_conflict.
A source-order change changes Command digest but not the canonical declaration set.

Accepted is fixed historical evidence with original ContentRef and first effective
retain_until; it does not claim published bytes. The record separately preserves
original requested retention, first effective retention and monotonic current cap.
Publication budget/deadline/retry ceiling are fixed at admission and never reset by
reopen. Actual I/O is bounded by original Claim.LeaseUntil, current policy and the
original deadline. Current tightening is saved before transient Defer; later policy
widening cannot extend an observed cap. Expiry/revocation cannot imply physical gone.

1.2 Command progress observes preparing/published/failed publication history only;
current Content get independently checks read and disclose, then verifies the entire
object before range slicing. Missing/damaged bytes return unavailable with exact
reason and never repair or choose latest. The explicit `GetCommand10`/`GetCommand11`
bridges reuse durable current reader policy and untouched old closed codecs. New
accepted and unrepresentable new error reasons stay unavailable; only lossless facts
bridge. Frozen old APIs/schema/SQL/archive remain byte-for-byte unchanged.

## Fixture policy and finite responsibility

`InstallFixturePolicy(ctx, FixturePolicy, expectedRevision)` and
`InstallFixtureCommandReader` are trusted host fixture-management seams, absent
from all client payloads. They are not production Grant APIs. Independent read,
process, save, sync and disclose flags default to false. Put requires save;
direct exact published sources require their own read/process/save permissions;
Get requires read and disclose. Unopened paths remain denied.

Current direct-source gates execute at admission and before/after native publication.
Source/target retention caps bound first accepted. Cross-tenant refuses, cross-owner
source support is unopened. Full inherited closure, revocation propagation, source
processing ports, ContextCompiler/Decision integration, physical cleanup/fences,
second independent holder and SIGKILL recovery belong to subsequent tickets. Do not
consume the old fixture Seed as evidence of Content-backed material.

Preparing count and retained staging bytes have separate limits. Failed staging
still consumes bytes until later actual cleanup. Records retain original staging,
attempt name/epoch and cleanup responsibility on failures; this is not a claim that
all residual holders are physically absent. Local Put uses bounded temp write,
independent hash/length verification, file Sync, no-clobber link, directory Sync and
independent final readback before published adoption. Existing wrong bytes stay
unchanged and never publish; matching existing bytes may be adopted.

## Acceptance map

1. Closed 1.2 source/types/codecs/shared goldens: `contract/schema/1.2.0`,
   `contract/v1_2`, `sdk/typescript/src/v1_2`, shared `conformance/fixtures/1.2.0`.
2. Same-Tx staged declaration/fixed receipt/Job and real reopen:
   `TestContentCorrectBytesDurablyAccepted`.
3. Native bounded primitives and no-clobber:
   `TestContentNoClobberExistingBytesMustMatchBeforePublication` plus local lifetime tests.
4. Independent two identities/fixed original replay:
   `TestContentCommandReplayAndExactVersionIdentities`, independent digest/identity goldens.
5. Canonical/decoded/wire/source limits, exact decimals, empty/binary/range:
   101 shared wire fixtures, `TestContentDecodedByteBoundHasRealNormalPublication`,
   `TestContentEmptyBytesBinaryAndFiniteRanges`, response binding tests in Go/TS.
6. Current distinct policy/trusted subject/config/finite responsibility:
   `TestContentExplicitPolicyAndTrustedPrincipalFailClosed`,
   `TestContentDirectSourcesRequireSeparateReadProcessSaveAndRetention`, wait/retry tests.
7. Exact public facts/object damage/no query repair/accurate legacy readers:
   `TestContentPublishedDamageAndMissingNeverRepairOnQuery`, old-reader test, real reopen.
8. Fresh/repeat/checksum plus protected affected old paths/lifetime:
   `TestContentMigrationEmptyRepeatAndChecksum`, old historical PG/SQLite upgrade
   normal/refusal/retry paths, protected archive checksums and exact native scope audit.

Actual command results, red/green distinctions and resource proof are recorded in
[ticket-01-evidence.md](ticket-01-evidence.md). No box is checked before root accepts.

## Current gates after independent intermediate review

Original Put replay rechecks its original reader after Command lock and retains
historical receipt priority even past original admission/content retention. A new
association applies current target-save and exact direct-source read/process/save,
monotonically intersects the old current/effective cap, and allocates no new Job
or staging body. A valid historical failed version remains associable.

Get checks the durable direct Sources with current caller/purpose read/disclose,
exact published metadata and current caps before native I/O and again before
body disclosure. Process/save/sync remain independent. Both observations preserve
the earlier bound; all initial blocking reads end in fresh trusted admission time.
AcceptBefore admits the read rather than imposing an I/O completion cutoff.
GetCommand rechecks reader/time after blocking facts, preserving history semantics.
These gates do not implement the later inherited closure or physical cleanup.
