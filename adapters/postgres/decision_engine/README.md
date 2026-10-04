# Decision-owner PostgreSQL adapter

`Store` owns an independent schema, migration ledger, typed Decision facts and
strict 1.1.0 fixed Command receipt ledger. Jobs reference real Decision rows.
The existing private PG core supplies only finite owner transactions, opaque
same-store tokens, Job/Claim and explicit pool mechanics. No demo business Input
is required. Old demo PG/SQLite migrations and behavior remain separate.

`Migrate` applies the numbered embedded files in one finite transaction under
the owner migration lock. Each applied version retains its SHA-256 checksum;
rerun verifies every checksum and does not rewrite a published migration.
Migration0001 defines the initial owner schema. Migration0002 adds no fabricated
past execution time or billing event. Stop and drain the old writer generation
before running it; mixed old/new writers are unsupported because an old writer
could overwrite new accounting observations.

For pre-accounting rows,0002 adds confirmed `rule_starts="0"`. Existing execution
indicators classify observations as incomplete, so old recorded bytes, steps
and cost remain lower bounds rather than invented totals. An old accepted row
without execution closes as `billing_basis_unsupported` with exact zero new
starts/fees. An old running/waiting row closes as `usage_unavailable` with its
recorded usage unchanged. Their original accepted receipt, input, component and
manifest binding remain fixed. Their existing Job is closed in the same owner
transaction. Old terminal status, Proposal and publication identities remain
unchanged. New calls using the old binding receive fixed `unsupported` before
creating new work; replay still returns its original accepted receipt.

Only new Decisions with the distinct accurate fixture-rule/2 component,
manifest and durable-rule-start billing basis execute the current rule. The
fixture source owner continues reading old absent billing metadata unchanged.
Conformance creates old accepted/running/completed facts with the frozen real
old writer, drains it, upgrades through this adapter, and observes the preserved
facts through the public current queries.

Test cleanup retains independent administrative handles. Only acknowledged
CREATE schemas registered in the fsynced ownership ledger may be removed;
names, timestamps and row contents do not establish deletion authority.
