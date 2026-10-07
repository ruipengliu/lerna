# Complete Module Interfaces and preserve reliable recovery

Status: ready-for-agent

## Problem Statement

Lerna has a working M1 implementation. Its main Modules have clear responsibilities. Task orchestration owns Task decisions. Sessions owns user input and Confirmation records. Grants owns authorization. Budget owns cost records. The Execution Manager owns Operation and Effect records. The Egress Gate controls access to external resources.

Maintainers cannot yet rely on the declared Interfaces to describe all requirements. Several Modules accept a Store Adapter and then require extra methods through unchecked type assertions. A minimal Adapter that satisfies the public ledger Store Interface compiles, but an accepted Operation handoff causes a panic. The production SQLite Adapter supplies the hidden methods and avoids this failure.

Maintainers must also know workflow details outside the Module that owns them. Startup and CLI recovery each maintain a processing sequence. Sessions coordinates Task creation, explicit Requirement acceptance, and initial input recording. Completion, cancellation, and non-success Task closure each repeat parts of the same delivery and send-closure protocols.

Target-specific evidence rules remain inside the Execution Manager. The design requires these rules to have a separate identity as trusted implementations. Adding or changing a target protocol currently requires changes to the core Implementation. The reusable executor conformance suite also requires the concrete production Harness, which limits where the same behavior checks can run.

These issues increase the cost of changing or replacing an Adapter. They also increase the risk that a reliability fix reaches only one workflow. The review did not establish a current production loss of responsibility, duplicate physical send, or incorrect Result.

## Solution

Make each Module Interface state its required dependencies. Put recovery sequence rules in one trusted host Module. Put initial Task creation rules inside Task orchestration. Give trusted evidence rules an injected Interface. Share delivery and send-closure mechanics inside their current owning Modules. Let the existing executor conformance scenarios run through narrow production command and query Interfaces.

Preserve the existing user behavior and reliability contracts. Original commands must return original receipts. Recovery must continue original responsibilities. Unknown Effects, late evidence, and billing records must retain their original identities. Fixed Result records must remain unchanged.

The work increases Depth by reducing the dependency and ordering knowledge that callers must learn. It increases Locality by giving each shared reliability rule one Implementation owner.

## User Stories

1. As a storage Adapter author, I want the declared Store Interface to include every required method, so that compilation identifies missing capabilities.
2. As a Module caller, I want required dependencies to be checked before production use, so that a normal command does not discover a missing dependency through a panic.
3. As a host maintainer, I want production assembly to report incomplete configuration before business recovery starts, so that partial assembly cannot advance saved responsibilities.
4. As a test author, I want explicit dependencies for a narrow scenario, so that I can provide a valid test Adapter without reading every Implementation method.
5. As a Module caller, I want an explicitly disabled capability to return its documented error, so that missing configuration cannot appear to be successful work.
6. As a host maintainer, I want startup and manual recovery to use one recovery entry point, so that I can find their processing rules in one place.
7. As a user, I want startup to check supported saved versions before recovery advances work, so that unsupported history remains available for review.
8. As a user, I want recovery to query the original command receipt, so that a lost receipt does not create a replacement Operation.
9. As a user, I want recovery to wait for an existing claim to expire when required, so that an old worker cannot lose its responsibility to an early takeover.
10. As a user, I want manual recovery to retain its current authority limits, so that a recovery command does not grant new model or execution permissions.
11. As a user, I want future Reconciliation work to keep its saved due time, so that recovery does not perform a query before that time.
12. As a user, I want a transient recovery failure to preserve the original responsibility, so that I can retry recovery after the dependency becomes available.
13. As a user, I want startup to resume only saved and enabled ReasonerDriver records, so that opening the host does not configure new Tasks.
14. As a user, I want cancellation to stop new model and target work, so that recovery cannot continue the cancelled Task through a new request.
15. As a Session caller, I want one creation Interface in the existing transaction, so that I do not need to order internal Task updates.
16. As a user, I want explicit Requirements and their input record to agree, so that the Task has one recorded basis for admission.
17. As a user, I want a goal without explicit Requirements to keep its current draft and waiting state, so that the refactor does not invent completion conditions.
18. As a user, I want the original SubmitGoal receipt phases to remain unchanged, so that existing command replay retains its meaning.
19. As a trusted rule maintainer, I want target-specific evidence rules behind an explicit Interface, so that I can verify a rule without changing core Effect projection.
20. As a user, I want saved evidence to use its original supported rule version, so that an upgrade does not change the meaning of earlier evidence.
21. As a user, I want unsupported rule versions to stop recovery before business work starts, so that the system does not interpret unknown history with a newer rule.
22. As a user, I want weak, invalid, or mismatched evidence to preserve an unknown Effect, so that the system does not claim an outcome it cannot prove.
23. As a user, I want a terminal query response to be checked against the original Operation, so that completion of the query does not imply completion of the original action.
24. As a user, I want file evidence to retain all required publication and barrier checks, so that a matching read alone cannot establish successful publication.
25. As an execution Adapter author, I want the trust rules to remain under host control, so that replacing an ordinary Adapter cannot replace terminal evidence rules.
26. As a reliability maintainer, I want one internal send-closure algorithm, so that a fix applies to completion, cancellation, and non-success Task closure.
27. As a user, I want each closure workflow to retain its own authorization basis and evidence record, so that shared mechanics do not combine different decisions.
28. As a user, I want closure to inspect all original sends, so that closing a new unsent attempt cannot erase an older unknown Effect.
29. As a user, I want an accurate no-send proof from the original execution endpoint, so that a control receipt is not treated as proof that an action did not occur.
30. As a user, I want closure, the start gate, and physical I/O to use the same ordering point, so that a late worker cannot send after closure has won.
31. As a user, I want a closure that arrives before its original handoff to remain effective, so that the late handoff cannot enable execution.
32. As a user, I want late observations and bills to remain attached to their original sends, so that closure does not remove execution or settlement responsibility.
33. As a user, I want Budget to release only reservations covered by an exact immutable proof, so that cancellation or Task closure cannot erase possible costs.
34. As a user, I want delivery recovery to complete work only after the source saves the recipient receipt, so that responsibility survives a lost acknowledgment.
35. As a user, I want non-success Task closure to preserve execution and settlement followups, so that unresolved work remains visible after the Task closes.
36. As a user, I want a fixed Result to keep the same stored bytes after late evidence and recovery, so that the Task conclusion remains an immutable record.
37. As a conformance test author, I want the existing executor scenarios to use narrow command and query Interfaces, so that another permitted implementation can run the same assertions.
38. As a conformance test author, I want independent target request, effect, and billing counts, so that core records alone cannot prove that external work occurred once.
39. As a reliability maintainer, I want crash and receipt-loss tests at real commit positions, so that recovery is checked against persisted facts.
40. As a maintainer, I want the design documents and complete checks to describe the final structure, so that the next change starts from an accurate contract.

## Implementation Decisions

1. **Keep the current fact owners.** Retain Task orchestration, Sessions, Grants, Budget, Execution Manager, Content governance, Trace, Durable work, and Egress Gate. Preserve transaction domains and the one-writer rule. This is a structural refactor under the existing reliability contracts.

2. **Make dependencies explicit.** Audit each Module constructor, Store, Work, and Decisions Interface, and assembly method against all supported command, query, and recovery behavior. Include named and anonymous asserted Interfaces, even when an assertion is checked and returns an error. Compose the required sub-Interfaces or inject separate typed dependencies. Store required dependencies in typed fields. Required capabilities must not be discovered through type assertions during a business call. A type assertion remains appropriate only for an explicitly optional capability with a defined fallback or failure. Include Session delivery and questions, content registration and derivation, observations and file resources, trace sources and metrics, and all budget, Task, execution, closure, and history paths in this audit.

3. **Complete assembly before use.** Preserve two-stage wiring where Modules have real reciprocal dependencies. Add an explicit completion or validation step for production assembly. A constructor must reject missing dependencies that it can check immediately. Assembly completion must reject the remaining missing links before compatibility checks, business recovery, or external I/O. Missing required dependencies, including typed nil values, produce an explicit assembly error. Distinguish intentional capability absence from invalid configuration. Do not introduce a forwarding layer for each stored record.

4. **Centralize trusted host recovery.** Introduce one host recovery Module with a bounded entry point and explicit startup and manual-progress modes. Inject narrow command and query Interfaces. Keep the phase list and ordering private and fixed. The host Module coordinates existing owners; each owner still decides and writes its facts. Assembly connects this Module, and the CLI delegates its recovery request to it.

5. **Preserve recovery mode differences.** Startup retains its current bounded waiting behavior and all saved-version checks before business recovery. Manual progress retains its current supported work and caller authority. Preserve fixed producer identities, including the separate authority used for enabled ReasonerDriver recovery. Do not relabel an ordinary caller as a trusted producer. Shared entry points do not imply identical phase lists or identities.

6. **Preserve complete recovery work.** After compatibility checks, preserve startup order: goals, original handoffs, content registration and observations, execution reports and interpretation, Grant revocation, cancellation, completion, Operation progress, Reconciliation, Operation progress again, Task-closure delivery, non-success Task-closing decisions, Budget closure, execution followups, settlement followups, enabled ReasonerDrivers, and trace recovery. Preserve the separate manual-progress sequence and supported phases. Completion recovery includes rechecking the verification and its unique continuation request; it is not only closure delivery. Cancellation acknowledgments do not create a final Result. Task-closure delivery and final non-success Result creation remain separate owner decisions. Due times, claim epochs, leases, timeout behavior, and current error behavior remain authoritative. Waiting does not hold a transaction lock. Recovery has no new autonomous loop and does not register arbitrary handlers.

7. **Deepen initial Task creation.** Give Task orchestration one transactional creation Interface that accepts the trusted initial goal, Session input, command identity, and optional explicit Requirements. It creates the Task, accepts applicable Requirements, and records the initial input in the existing adjudication transaction. It returns the created Task reference and the information needed for Session routing. Sessions retains message order and association writes. Preserve initial input sequence, Requirement source, bound input version, revision behavior, and processed or draft state. Validate permanent rejection conditions before writing in the legacy goal-decision path, as its current transaction contract requires. Keep legacy asynchronous SubmitGoal and synchronous input delivery receipt semantics distinct. An input waiting on a dependency creates no Task until its original delivery becomes eligible.

8. **Inject trusted evidence rules.** The Execution Manager declares the internal rule Interface. Trusted Adapters implement the existing simulator, file, API, model, and query evidence rules. The Interface accepts the original supported version, fixed descriptor, attempt, and original observation, with governed body material when interpretation requires it. It returns verified evidence facts and any existing wait information. The Execution Manager still performs conflict projection, decides Effect and settlement state, and writes the resulting records. Ordinary execution Adapters cannot register terminal rules.

9. **Retain historical rule identity.** Rule extraction preserves supported versions, complete declarations, bindings, and rule identifiers. Saved interpretations and receipts are replayed rather than recalculated. Compatibility checks use pure version and binding checks; they do not compile requests, read governed body content, resolve credentials, renew keys, or send requests. Unknown original versions stop startup before any business responsibility advances. Expired original keys remain readable as history. Current credential absence retains its current distinction from unsupported saved history.

10. **Share closure mechanics inside the owners.** Within Task orchestration, share the claim, original-receipt query, delivery, recipient validation, source acknowledgment, and bounded recovery mechanics. Within the Execution Manager, share send-history inspection, REGISTERED-send closure, revision updates, dispatch sealing, and execution-work updates. Private helpers return the proof material needed by each typed workflow. Keep completion, cancellation, and non-success Task closure commands, authority checks, ranges, intents, seals, event types, and followups distinct. Any consolidation of Durable work helpers retains the fixed permitted Job types and their owner-specific completion conditions.

11. **Preserve closure proof and accounting.** The original endpoint closure still uses the same cross-process critical section as P4, P5, and physical I/O. SEALED does not establish a no-send proof by itself. Inspect all current and historical sends. Missing execution records are not an empty history. Retain permanent markers for closures that precede Operation acceptance. Shared closure mechanics do not release Budget reservations, return consumed Grant uses, or erase billing sources. Budget remains the only owner of exact proof-based reservation release and cost settlement.

12. **Preserve atomic records and cross-domain receipts.** Operation updates, typed seals, Job changes, followups, and structural source events remain in their existing owner transactions. A required source-event failure rolls back the owner transaction. Cross-domain delivery retains intent, recipient acceptance, and source receipt commits. Source work completes only after the recipient receipt is saved. Trace remains diagnostic and does not decide business completion.

13. **Generalize the existing executor fixture.** Replace the concrete Harness requirement in the reusable suite with narrow Interfaces for invocation, Operation query, original observation query, and billing-source query. Production assembly supplies these Interfaces. Fixture setup, independent target observation, and fault injection remain fixture responsibilities. Reuse the existing scenarios and assertions. This work does not implement a second backend or a remote core.

14. **Keep public and persisted contracts stable.** Preserve command identities, receipt phases, stable error and recovery semantics, supported schemas, persisted object identities, format markers, and fixed Result bytes. No new protocol or database migration is planned. The refactor must retain the current supported history. If a proposed implementation needs a semantic or persisted-format change, it requires a separate specification and an upstream decision before that change.

15. **Update design before implementation.** Describe the completed dependency Interfaces, host recovery ownership, Task creation Interface, and trusted rule assembly in the affected design documents. Follow the existing Module dependency rules. Keep core dependencies on public contracts and each Module's declared internal Interfaces. These decisions implement existing ADR constraints; they do not supersede those ADRs.

## Testing Decisions

- **Primary Seam:** Use the existing production host commands and public queries. Include the compiled CLI for startup and manual recovery behavior. Verify persisted Task, Session, Operation, receipt, seal, input, Budget, and Result records through their current owner queries. Independent targets provide physical request, effect, and billing counts. Good tests assert these outcomes and survive internal rearrangement.
- **Narrow exceptions:** Dependency completeness needs compile-time Adapter checks and constructor or assembly-failure checks. Check the production Store and each transaction-domain Work Adapter against its consuming Interfaces. A replacement Adapter that implements the declared required Interface must exercise the relevant normal command without a hidden-interface panic. An incomplete Adapter must fail the construction or configuration type check. Cover missing links, nil and typed nil dependencies, and complete two-stage assembly. Missing required configuration must fail before business recovery or external I/O. Optional capability absence must produce its defined behavior. These checks do not require a new external test Seam.
- **Module coverage:** Cover Task orchestration, Sessions, Execution Manager, Budget, Content governance, Trace, Durable work, Egress Gate, trusted rule Adapters, and host recovery where their Interfaces change. Keep existing Grant and content-use checks active in end-to-end scenarios.
- **Baseline:** Before code changes, record observable outputs and independent target counts for representative existing workflows. Use current conformance scenarios as prior art. Do not use the refactored Implementation itself as the expected-output oracle.
- **Initial Task creation:** Test goals with and without explicit Requirements through Session commands. Verify one Task, one initial input sequence, exact association, Requirement source, bound input version, receipt phase, and processed or draft status. Cover original SubmitGoal replay, synchronous input replay, and a failure inside the shared transaction with no partial Task or Session state.
- **Recovery modes:** Run production startup and manual progress with pending original responsibilities. Verify phase-dependent results, mode-specific identities, natural claim expiry, retained future due times, and cancellation. A transient dependency failure must preserve the original work. Startup compatibility rejection must leave another READY Job unclaimed and leave external target counts unchanged.
- **Reasoner recovery:** Reuse configured-driver, original-position replay, UNKNOWN, cancellation, and rejected-completion continuation scenarios. Verify that recovery creates no replacement model position, Proposal request, admission, or physical send. Only saved enabled drivers may resume, through the existing bounded Task owner entry point.
- **Trusted rules:** Reuse public API, file, simulator, model, and independent-query scenarios. Assert original rule identity, Effect outcome, late-effect state, conflict projection, and wait behavior. Include wrong accounts or keys, mismatched subjects, weak negative evidence, invalid or redacted bodies, non-terminal responses, and file-barrier failures. Query completion must not establish the original Operation's terminal state without its required evidence.
- **Supported history:** Include unprepared Operations, closed UNKNOWN Operations, previous sends, independent queries, expired keys, and missing current credentials. Include completed, disabled, and superseded driver revisions. Verify compatibility without compiling, body reads, credential resolution, or target calls. Unsupported rule or implementation versions must fail before any owner advances business work. Compare original receipts, billing sources, Task-closing views, and fixed Result bytes. Unknown format, identity, or saved-contract rejection retains the original file group as required by its existing qualification contract. Unknown implementation rejection in a recognized database format must preserve business facts; this does not add a promise that WAL or SHM bytes stay unchanged.
- **Closure windows:** For completion, cancellation, and non-success Task closure, cover before P4, after P4 but before P5, and after P5. Include late original handoff, stale workers, missing execution history, and an older possible send with a newer REGISTERED send. Assert typed seals, exact closed-send references, original Effect, late-effect state, all billing sources, and any required execution and settlement followups.
- **Commit and receipt loss:** Reuse fault tests for failure before commit, failure after commit, and lost receipts at original intent, recipient acceptance, and source acknowledgment. Prove each saved position through public queries. Recovery and replay must retain the same responsibility and must not increase original business-send counts. Independently authorized query counts are checked separately.
- **Accounting and diagnostics:** Verify that closure-based release of an unsent reservation requires exact immutable no-send evidence for that reservation. For P5 sends, Budget continues to settle costs and release any unused reservation under the existing final-billing-evidence rules. Late bills and observations do not change fixed Result bytes. Owner source-event failure must roll back the corresponding business write. Trace collection and its backlog do not alter business decisions.
- **Conformance fixture:** Run the existing executor scenarios through the new narrow fixture Interfaces. Keep production Adapter coverage and independent target counts. Add one lightweight Interface Adapter around the existing production commands to show that the fixture no longer requires concrete Harness fields. This wrapper is test plumbing, not a new business Implementation.
- **Test maintenance:** Move or replace tests that assert obsolete internal arrangement. Keep distinct public fault scenarios and their evidence. Do not add another copy of a full conformance suite for the private helpers. Every added behavioral test carries the applicable project rule annotation.
- **Completion criteria:** All scenarios above pass without changed user behavior, hidden required dependency assertions, lost responsibilities, or new physical sends. The complete project check passes, including normal and fault race tests, dependency lint, protocol compatibility, generated-code checks, and rule checks. Record the actual tested source revision and command results after implementation.

## Out of Scope

- New user commands, autonomous Reasoner loops, target protocols, model suppliers, storage backends, remote transports, or third-party plugin registration.
- Changes to admission, authorization, Confirmation consumption, retry eligibility, Effect projection, settlement criteria, billing interpretation, or the authority of fact owners.
- Combining transaction domains or replacing persistent cross-domain handoffs with in-memory calls.
- Combining completion, cancellation, and non-success closure into one business decision or one persisted evidence type.
- Automatic migration, backup rollback, deletion of unsupported history, key renewal, or rewriting fixed Result records.
- General workflow engines, arbitrary handler registries, table-access facades, or a Repository wrapper for each record.
- Trace query performance work, default Reasoner prompt changes, or broader Module decomposition based only on method or dependency counts.
- A second core Implementation or backend qualification. The fixture change only removes the existing concrete-Harness restriction.

## Further Notes

The repository issue tracker stores this specification as a local Markdown issue. The specification is ready for implementation planning; it does not report that the refactor is complete. Implementation starts with the affected design updates and failing behavior checks, followed by small changes at the existing Seams.

The user confirmed the test Seams: use existing production commands and queries for behavior acceptance, with only the necessary compile-time and construction checks for dependency completeness. No new public testing Interface is required. The narrow executor fixture wraps the same existing commands and queries.

Review evidence is limited to the current design and code. The original Module review ran selected core and conformance tests and reproduced the storage Interface panic. Those checks establish the problem and the baseline. They do not validate the new structure or replace the complete implementation checks.

Use the [project glossary](../../GLOSSARY.md), whose authoritative terms are in [Project goals, section 3](../../docs/architecture/project-goals.md#3-核心术语). Preserve the [layering rules](../../docs/architecture/layers.md), [development rules](../../docs/development.md), and [design-document conventions](../../docs/architecture/conventions.md).

Relevant accepted decisions:

- [ADR 0003: Replacement classes and assembly](../../docs/adr/0003-replacement-classes-and-assembly.md) and [ADR 0004: Language and stack](../../docs/adr/0004-language-and-stack.md).
- [ADR 0005: Billing source identity](../../docs/adr/0005-billing-source-identity.md) and [ADR 0006: Source handoff and trace mapping](../../docs/adr/0006-trace-source-handoff-local-mapping.md).
- [ADR 0007: Managed file publication](../../docs/adr/0007-managed-file-publication.md) and [ADR 0008: Fixed API identity and credentials](../../docs/adr/0008-fixed-api-identity-and-platform-credentials.md).
- [ADR 0009: Cancellation closure](../../docs/adr/0009-cancellation-closure.md) and [ADR 0010: Non-success Task closure](../../docs/adr/0010-nonsuccess-task-closing.md).
- [ADR 0011: Format compatibility and stopped recovery](../../docs/adr/0011-m1-format-and-stopped-recovery.md) and [ADR 0012: Bounded ReasonerDriver](../../docs/adr/0012-bounded-reasoner-driver.md).

Use the existing [executor trust requirements](../../docs/architecture/ports/executor/README.md#4-关键流程) and [conformance plan](../../docs/architecture/verification/conformance.md) as design sources. Keep the completed M1 specification and its acceptance records as historical evidence.
