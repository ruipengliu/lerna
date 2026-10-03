# Internal durable work mechanisms

`Admit` coordinates one original Command key, digest comparison, finite new
admission deadline and fixed receipt using the consumer's injected short Tx.
The consumer decides business preconditions and writes its facts/Job through
that Tx. runtime never interprets the demo `text` or claims Task success.

`Tx` is opaque: adapters must validate their exact database binding, owner and
active transaction lifetime. `CommandStore` preserves the original receipt and
minimum immutable metadata; `JobStore.Trigger` adds the current responsibility
without replacing its Job identity. `Clock` supplies one trusted owner time
source, with PG defaulting to database time and SQLite to trusted device UTC time.

Only a confirmed COMMIT returns a received receipt. `ErrCommitUnknown` produces
`commit_unknown` with the original reference and the public
`query_or_retransmit_original` action. V1 implements admission only; Claim,
worker and scheduling remain later tickets. PostgreSQL and file SQLite implement
the same v1 admission ports; adapter details live in their READMEs.
