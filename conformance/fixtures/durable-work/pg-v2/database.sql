--
-- PostgreSQL database dump
--

\restrict lernav2fixture

-- Dumped from database version 18.6 (Debian 18.6-1.pgdg12+2)
-- Dumped by pg_dump version 18.6 (Debian 18.6-1.pgdg12+2)

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: lerna_test_c5e0e440f4b2382a7f6e24c8; Type: SCHEMA; Schema: -; Owner: -
--

CREATE SCHEMA lerna_test_c5e0e440f4b2382a7f6e24c8;


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: command_receipts; Type: TABLE; Schema: lerna_test_c5e0e440f4b2382a7f6e24c8; Owner: -
--

CREATE TABLE lerna_test_c5e0e440f4b2382a7f6e24c8.command_receipts (
    tenant_id text NOT NULL,
    owner_id text NOT NULL,
    command_id text NOT NULL,
    digest text NOT NULL,
    metadata bytea NOT NULL,
    receipt bytea NOT NULL
);


--
-- Name: durable_inputs; Type: TABLE; Schema: lerna_test_c5e0e440f4b2382a7f6e24c8; Owner: -
--

CREATE TABLE lerna_test_c5e0e440f4b2382a7f6e24c8.durable_inputs (
    tenant_id text NOT NULL,
    owner_id text NOT NULL,
    object_id text NOT NULL,
    revision bigint NOT NULL,
    text_value bytea NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    projected_revision bigint,
    text_digest text,
    CONSTRAINT durable_inputs_projection CHECK ((((projected_revision IS NULL) AND (text_digest IS NULL)) OR ((projected_revision IS NOT NULL) AND (text_digest IS NOT NULL) AND (projected_revision > 0) AND (projected_revision <= revision) AND (text_digest ~ '^sha256:[0-9a-f]{64}$'::text)))),
    CONSTRAINT durable_inputs_revision_check CHECK ((revision > 0))
);


--
-- Name: jobs; Type: TABLE; Schema: lerna_test_c5e0e440f4b2382a7f6e24c8; Owner: -
--

CREATE TABLE lerna_test_c5e0e440f4b2382a7f6e24c8.jobs (
    tenant_id text NOT NULL,
    owner_id text NOT NULL,
    job_id text NOT NULL,
    object_kind text NOT NULL,
    object_id text NOT NULL,
    phase text NOT NULL,
    work_revision bigint NOT NULL,
    completed_revision bigint DEFAULT 0 NOT NULL,
    state text NOT NULL,
    due_at timestamp with time zone NOT NULL,
    lease_epoch bigint DEFAULT 0 NOT NULL,
    claimed_revision bigint,
    worker_id text,
    lease_until timestamp with time zone,
    scan_at timestamp with time zone GENERATED ALWAYS AS (
CASE
    WHEN (state = 'leased'::text) THEN GREATEST(due_at, lease_until)
    ELSE due_at
END) STORED,
    CONSTRAINT jobs_check CHECK (((completed_revision >= 0) AND (completed_revision <= work_revision))),
    CONSTRAINT jobs_claim_binding CHECK ((((state = 'leased'::text) AND (lease_epoch > 0) AND (claimed_revision IS NOT NULL) AND (claimed_revision > completed_revision) AND (claimed_revision <= work_revision) AND (worker_id IS NOT NULL) AND ((length(worker_id) >= 1) AND (length(worker_id) <= 128)) AND (lease_until IS NOT NULL)) OR ((state <> 'leased'::text) AND (claimed_revision IS NULL) AND (worker_id IS NULL) AND (lease_until IS NULL)))),
    CONSTRAINT jobs_lease_epoch_check CHECK ((lease_epoch >= 0)),
    CONSTRAINT jobs_progress_state CHECK ((((state = 'done'::text) AND (completed_revision = work_revision)) OR ((state <> 'done'::text) AND (completed_revision < work_revision)))),
    CONSTRAINT jobs_state_check CHECK ((state = ANY (ARRAY['ready'::text, 'leased'::text, 'done'::text]))),
    CONSTRAINT jobs_work_revision_check CHECK ((work_revision > 0))
);


--
-- Name: schema_migrations; Type: TABLE; Schema: lerna_test_c5e0e440f4b2382a7f6e24c8; Owner: -
--

CREATE TABLE lerna_test_c5e0e440f4b2382a7f6e24c8.schema_migrations (
    version bigint NOT NULL,
    checksum text NOT NULL
);


--
-- Data for Name: command_receipts; Type: TABLE DATA; Schema: lerna_test_c5e0e440f4b2382a7f6e24c8; Owner: -
--

COPY lerna_test_c5e0e440f4b2382a7f6e24c8.command_receipts (tenant_id, owner_id, command_id, digest, metadata, receipt) FROM stdin;
tenant-one	owner-one	v2-original	sha256:d272b19334ecab93e35012d81ffcad7bc8c155dbda005d8d1459730e72d3f503	\\x7b22636f6e74726163745f76657273696f6e223a22686f73742d64757261626c652d776f726b2d31222c2270726f66696c65223a22686f7374222c226d6574686f64223a2264757261626c655f776f726b2e7265636f7264222c22746172676574223a7b2274656e616e745f6964223a2274656e616e742d6f6e65222c226f776e65725f6964223a226f776e65722d6f6e65222c226b696e64223a2264757261626c655f776f726b222c226964223a22696e707574227d2c227375626a6563745f62696e64696e67223a7b2274656e616e745f6964223a2274656e616e742d6f6e65222c227375626a6563745f6964223a22616c696365222c2264656c65676174696f6e5f636861696e223a5b5d7d2c226163636570745f6265666f7265223a22323130312d30312d30315430303a30303a30302e3030303030305a227d	\\x7b22636f6d6d616e645f726566223a7b22636f6d6d616e645f6964223a2276322d6f726967696e616c222c226f776e6572223a7b226f776e65725f6964223a226f776e65722d6f6e65222c2274656e616e745f6964223a2274656e616e742d6f6e65227d7d2c226e6578745f616374696f6e223a2271756572795f6f726967696e616c222c226f626a6563745f726566223a7b226964223a22696e707574222c226b696e64223a2264757261626c655f776f726b222c226f776e65725f6964223a226f776e65722d6f6e65222c227265766973696f6e223a2231222c2274656e616e745f6964223a2274656e616e742d6f6e65227d2c227265766973696f6e223a2231222c227374617465223a226170706c696564227d
tenant-one	owner-one	v2-new	sha256:8b2b407b05e0b8ea3caa3081ec306abece3f41e615ae05aada0276ebf4c4c8cb	\\x7b22636f6e74726163745f76657273696f6e223a22686f73742d64757261626c652d776f726b2d31222c2270726f66696c65223a22686f7374222c226d6574686f64223a2264757261626c655f776f726b2e7265636f7264222c22746172676574223a7b2274656e616e745f6964223a2274656e616e742d6f6e65222c226f776e65725f6964223a226f776e65722d6f6e65222c226b696e64223a2264757261626c655f776f726b222c226964223a22696e707574227d2c227375626a6563745f62696e64696e67223a7b2274656e616e745f6964223a2274656e616e742d6f6e65222c227375626a6563745f6964223a22616c696365222c2264656c65676174696f6e5f636861696e223a5b5d7d2c226163636570745f6265666f7265223a22323130312d30312d30315430303a30303a30302e3030303030305a222c2265787065637465645f7265766973696f6e223a2231227d	\\x7b22636f6d6d616e645f726566223a7b22636f6d6d616e645f6964223a2276322d6e6577222c226f776e6572223a7b226f776e65725f6964223a226f776e65722d6f6e65222c2274656e616e745f6964223a2274656e616e742d6f6e65227d7d2c226e6578745f616374696f6e223a2271756572795f6f726967696e616c222c226f626a6563745f726566223a7b226964223a22696e707574222c226b696e64223a2264757261626c655f776f726b222c226f776e65725f6964223a226f776e65722d6f6e65222c227265766973696f6e223a2232222c2274656e616e745f6964223a2274656e616e742d6f6e65227d2c227265766973696f6e223a2232222c227374617465223a226170706c696564227d
\.


--
-- Data for Name: durable_inputs; Type: TABLE DATA; Schema: lerna_test_c5e0e440f4b2382a7f6e24c8; Owner: -
--

COPY lerna_test_c5e0e440f4b2382a7f6e24c8.durable_inputs (tenant_id, owner_id, object_id, revision, text_value, created_at, updated_at, projected_revision, text_digest) FROM stdin;
tenant-one	owner-one	input	2	\\x68656c6c6f	2026-10-03 20:00:00+00	2026-10-03 20:00:00+00	\N	\N
\.


--
-- Data for Name: jobs; Type: TABLE DATA; Schema: lerna_test_c5e0e440f4b2382a7f6e24c8; Owner: -
--

COPY lerna_test_c5e0e440f4b2382a7f6e24c8.jobs (tenant_id, owner_id, job_id, object_kind, object_id, phase, work_revision, completed_revision, state, due_at, lease_epoch, claimed_revision, worker_id, lease_until) FROM stdin;
tenant-one	owner-one	job-afee007508d4f54e0dc0793934d5671b	durable_work	input	project	2	0	leased	2026-10-03 20:00:00+00	1	1	v2-old	2026-10-03 20:00:01+00
\.


--
-- Data for Name: schema_migrations; Type: TABLE DATA; Schema: lerna_test_c5e0e440f4b2382a7f6e24c8; Owner: -
--

COPY lerna_test_c5e0e440f4b2382a7f6e24c8.schema_migrations (version, checksum) FROM stdin;
1	sha256:f8d04d373b039a425b4f6d0a7b7dd4410971c00faf91cdba3f68a9204579127e
2	sha256:cdb7dea9f55ee8ac9201a943cecf8096108b48bc208409e1372bbf17295b2297
\.


--
-- Name: command_receipts command_receipts_pkey; Type: CONSTRAINT; Schema: lerna_test_c5e0e440f4b2382a7f6e24c8; Owner: -
--

ALTER TABLE ONLY lerna_test_c5e0e440f4b2382a7f6e24c8.command_receipts
    ADD CONSTRAINT command_receipts_pkey PRIMARY KEY (tenant_id, owner_id, command_id);


--
-- Name: durable_inputs durable_inputs_pkey; Type: CONSTRAINT; Schema: lerna_test_c5e0e440f4b2382a7f6e24c8; Owner: -
--

ALTER TABLE ONLY lerna_test_c5e0e440f4b2382a7f6e24c8.durable_inputs
    ADD CONSTRAINT durable_inputs_pkey PRIMARY KEY (tenant_id, owner_id, object_id);


--
-- Name: jobs jobs_pkey; Type: CONSTRAINT; Schema: lerna_test_c5e0e440f4b2382a7f6e24c8; Owner: -
--

ALTER TABLE ONLY lerna_test_c5e0e440f4b2382a7f6e24c8.jobs
    ADD CONSTRAINT jobs_pkey PRIMARY KEY (tenant_id, owner_id, job_id);


--
-- Name: jobs jobs_tenant_id_owner_id_object_kind_object_id_phase_key; Type: CONSTRAINT; Schema: lerna_test_c5e0e440f4b2382a7f6e24c8; Owner: -
--

ALTER TABLE ONLY lerna_test_c5e0e440f4b2382a7f6e24c8.jobs
    ADD CONSTRAINT jobs_tenant_id_owner_id_object_kind_object_id_phase_key UNIQUE (tenant_id, owner_id, object_kind, object_id, phase);


--
-- Name: schema_migrations schema_migrations_pkey; Type: CONSTRAINT; Schema: lerna_test_c5e0e440f4b2382a7f6e24c8; Owner: -
--

ALTER TABLE ONLY lerna_test_c5e0e440f4b2382a7f6e24c8.schema_migrations
    ADD CONSTRAINT schema_migrations_pkey PRIMARY KEY (version);


--
-- Name: jobs_scan; Type: INDEX; Schema: lerna_test_c5e0e440f4b2382a7f6e24c8; Owner: -
--

CREATE INDEX jobs_scan ON lerna_test_c5e0e440f4b2382a7f6e24c8.jobs USING btree (tenant_id, owner_id, scan_at, job_id) WHERE ((state <> 'done'::text) AND (lease_epoch < '9223372036854775807'::bigint));


--
-- Name: jobs jobs_tenant_id_owner_id_object_id_fkey; Type: FK CONSTRAINT; Schema: lerna_test_c5e0e440f4b2382a7f6e24c8; Owner: -
--

ALTER TABLE ONLY lerna_test_c5e0e440f4b2382a7f6e24c8.jobs
    ADD CONSTRAINT jobs_tenant_id_owner_id_object_id_fkey FOREIGN KEY (tenant_id, owner_id, object_id) REFERENCES lerna_test_c5e0e440f4b2382a7f6e24c8.durable_inputs(tenant_id, owner_id, object_id);


--
-- PostgreSQL database dump complete
--

\unrestrict lernav2fixture

