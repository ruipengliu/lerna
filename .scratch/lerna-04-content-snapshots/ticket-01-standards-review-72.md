Standards — fixed delivery follow-up

BASE 64c6872c8ed56ddd66dd51c98dc430046fc9f47d → DELIVERY 72da5ee820a69695553ff208e10a8ba31804d0b8; tested SOURCE 46d6ca26e4c2a4e9cf6db95e890b641f6bf2aa97. Actual full scope: six commits/88 paths; source scope: five/81. Qualified 56 unchanged initial objects by mode/type/blob; reviewed every source delta across 22 paths and all ten delivery documentation/audit paths. The archived Spec report was checked only for provenance/structure/links, with finding content isolated. Generated/schema/101-fixture coverage carries from the complete initial review; 79 frozen objects remain equal. Metadata: /tmp/lerna-04-ticket-01-standards-final-scope.json.

Original five hard P2 findings CLOSED in this source: Get performs a current post-I/O observation; initial Get/Command observations recheck trusted admission after blocking facts, including absence; local Open joins native causes; World registration immediately owns first Close and retains setup errors, while local partial setup closes acquired FDs; all five tools aggregate original/native/ACK/cleanup failures through scope.finish. The original FD/ACK Duplicated Code smell CLOSED through one concrete conformance-ownership module.

Current hard violations: 1; worst P2.

- P2 — domain/content/service.go:338–377, shared observe: “if policy == nil” accepts each target read/disclose policy without binding policy.Ref to the stored record.Ref. CheckPolicy selects stable version identity, so an authorized policy declaring a different hash/media type/length can authorize bytes from the existing declaration at both gates. “record.Ref != ref” checks only the caller. This breaches domain/content/README.md’s “exact-ref/purpose/subject policy” and AGENTS.md:125’s actual-entry authorization rule. Bind both target policies to the existing record; preserve caller metadata-mismatch integrity semantics. A subsequent unreviewed fix does not close this fixed-pin finding.

Current judgement smells: 1; worst P3.

- P3 — possible Duplicated Code: “const nativeCauses = [result.error, ...result.cleanupErrors, …]” plus AggregateError repeats at test-generator.mjs:167–178,221–232,341–352 and test-generator-v1_1.mjs:58–69. Fowler suggests one shared qualification shape. Root explicitly adopted KEEP after the narrow architecture decision: all four preserve identical native facts and complex ownership is already centralized. This remains optional maintenance judgement, not a required acceptance gate.

All 12 Fowler heuristics, repository overrides and tooling exclusions retained. No tests/build/DB/native execution. Recorded corrected race40.887/full check greens do not relabel historical failures. Eight AC remain unchecked pending root acceptance; later source/merge/CI are not covered.

Archival provenance added by the fixer, without changing the above verdict/pin:
[ticket-01-standards-review-72-scope.json](ticket-01-standards-review-72-scope.json)
is the exact original scope metadata, retaining all mode/type/blob qualification.
The /tmp pointer is historical, not the only retained object. This report is for
candidate72 and cannot be used as a verdict on source14 or later qualification.
