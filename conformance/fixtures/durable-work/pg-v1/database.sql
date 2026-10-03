--
-- PostgreSQL database dump
--

\restrict lernav1fixture

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
-- Name: lerna_test_000000000000000000000001; Type: SCHEMA; Schema: -; Owner: -
--

CREATE SCHEMA lerna_test_000000000000000000000001;


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: command_receipts; Type: TABLE; Schema: lerna_test_000000000000000000000001; Owner: -
--

CREATE TABLE lerna_test_000000000000000000000001.command_receipts (
    tenant_id text NOT NULL,
    owner_id text NOT NULL,
    command_id text NOT NULL,
    digest text NOT NULL,
    metadata bytea NOT NULL,
    receipt bytea NOT NULL
);


--
-- Name: durable_inputs; Type: TABLE; Schema: lerna_test_000000000000000000000001; Owner: -
--

CREATE TABLE lerna_test_000000000000000000000001.durable_inputs (
    tenant_id text NOT NULL,
    owner_id text NOT NULL,
    object_id text NOT NULL,
    revision bigint NOT NULL,
    text_value bytea NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT durable_inputs_revision_check CHECK ((revision > 0))
);


--
-- Name: jobs; Type: TABLE; Schema: lerna_test_000000000000000000000001; Owner: -
--

CREATE TABLE lerna_test_000000000000000000000001.jobs (
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
    CONSTRAINT jobs_check CHECK (((completed_revision >= 0) AND (completed_revision <= work_revision))),
    CONSTRAINT jobs_state_check CHECK ((state = 'ready'::text)),
    CONSTRAINT jobs_work_revision_check CHECK ((work_revision > 0))
);


--
-- Name: schema_migrations; Type: TABLE; Schema: lerna_test_000000000000000000000001; Owner: -
--

CREATE TABLE lerna_test_000000000000000000000001.schema_migrations (
    version bigint NOT NULL,
    checksum text NOT NULL
);


--
-- Data for Name: command_receipts; Type: TABLE DATA; Schema: lerna_test_000000000000000000000001; Owner: -
--

COPY lerna_test_000000000000000000000001.command_receipts (tenant_id, owner_id, command_id, digest, metadata, receipt) FROM stdin;
fixture-tenant	fixture-owner	applied-original	sha256:ef12ea033c4b41739d4b8981c9d448f9cb20842b1cf6f5ba3faf389612aca0b7	\\x7b22636f6e74726163745f76657273696f6e223a22686f73742d64757261626c652d776f726b2d31222c2270726f66696c65223a22686f7374222c226d6574686f64223a2264757261626c655f776f726b2e7265636f7264222c22746172676574223a7b2274656e616e745f6964223a22666978747572652d74656e616e74222c226f776e65725f6964223a22666978747572652d6f776e6572222c226b696e64223a2264757261626c655f776f726b222c226964223a2276312d696e707574227d2c227375626a6563745f62696e64696e67223a7b2274656e616e745f6964223a22666978747572652d74656e616e74222c227375626a6563745f6964223a22666978747572652d777269746572222c2264656c65676174696f6e5f636861696e223a5b5d7d2c226163636570745f6265666f7265223a22323039392d30312d30315430303a30303a30302e3030303030305a227d	\\x7b22636f6d6d616e645f726566223a7b22636f6d6d616e645f6964223a226170706c6965642d6f726967696e616c222c226f776e6572223a7b226f776e65725f6964223a22666978747572652d6f776e6572222c2274656e616e745f6964223a22666978747572652d74656e616e74227d7d2c226e6578745f616374696f6e223a2271756572795f6f726967696e616c222c226f626a6563745f726566223a7b226964223a2276312d696e707574222c226b696e64223a2264757261626c655f776f726b222c226f776e65725f6964223a22666978747572652d6f776e6572222c227265766973696f6e223a2231222c2274656e616e745f6964223a22666978747572652d74656e616e74227d2c227265766973696f6e223a2231222c227374617465223a226170706c696564227d
fixture-tenant	fixture-owner	expired-original	sha256:af87147779f2d50e8fb9f5d6ea70e0184948e44128c79351be0d1f0c25ffe15e	\\x7b22636f6e74726163745f76657273696f6e223a22686f73742d64757261626c652d776f726b2d31222c2270726f66696c65223a22686f7374222c226d6574686f64223a2264757261626c655f776f726b2e7265636f7264222c22746172676574223a7b2274656e616e745f6964223a22666978747572652d74656e616e74222c226f776e65725f6964223a22666978747572652d6f776e6572222c226b696e64223a2264757261626c655f776f726b222c226964223a22657870697265642d696e707574227d2c227375626a6563745f62696e64696e67223a7b2274656e616e745f6964223a22666978747572652d74656e616e74222c227375626a6563745f6964223a22666978747572652d777269746572222c2264656c65676174696f6e5f636861696e223a5b5d7d2c226163636570745f6265666f7265223a22323030302d30312d30315430303a30303a30302e3030303030305a227d	\\x7b22636f6d6d616e645f726566223a7b22636f6d6d616e645f6964223a22657870697265642d6f726967696e616c222c226f776e6572223a7b226f776e65725f6964223a22666978747572652d6f776e6572222c2274656e616e745f6964223a22666978747572652d74656e616e74227d7d2c226e6578745f616374696f6e223a227265736f6c76655f72656a656374696f6e222c22726561736f6e223a2265787069726564222c227374617465223a2272656a6563746564227d
\.


--
-- Data for Name: durable_inputs; Type: TABLE DATA; Schema: lerna_test_000000000000000000000001; Owner: -
--

COPY lerna_test_000000000000000000000001.durable_inputs (tenant_id, owner_id, object_id, revision, text_value, created_at, updated_at) FROM stdin;
fixture-tenant	fixture-owner	v1-input	1	\\x504720763120706f727461626c6520696e70757420f09f8c8d	2026-10-03 18:25:35.081773+00	2026-10-03 18:25:35.081773+00
\.


--
-- Data for Name: jobs; Type: TABLE DATA; Schema: lerna_test_000000000000000000000001; Owner: -
--

COPY lerna_test_000000000000000000000001.jobs (tenant_id, owner_id, job_id, object_kind, object_id, phase, work_revision, completed_revision, state, due_at) FROM stdin;
fixture-tenant	fixture-owner	job-6fecd7b3c21f82c899e8961852b58c4d	durable_work	v1-input	project	1	0	ready	2026-10-03 18:25:35.081773+00
\.


--
-- Data for Name: schema_migrations; Type: TABLE DATA; Schema: lerna_test_000000000000000000000001; Owner: -
--

COPY lerna_test_000000000000000000000001.schema_migrations (version, checksum) FROM stdin;
1	sha256:f8d04d373b039a425b4f6d0a7b7dd4410971c00faf91cdba3f68a9204579127e
\.


--
-- Name: command_receipts command_receipts_pkey; Type: CONSTRAINT; Schema: lerna_test_000000000000000000000001; Owner: -
--

ALTER TABLE ONLY lerna_test_000000000000000000000001.command_receipts
    ADD CONSTRAINT command_receipts_pkey PRIMARY KEY (tenant_id, owner_id, command_id);


--
-- Name: durable_inputs durable_inputs_pkey; Type: CONSTRAINT; Schema: lerna_test_000000000000000000000001; Owner: -
--

ALTER TABLE ONLY lerna_test_000000000000000000000001.durable_inputs
    ADD CONSTRAINT durable_inputs_pkey PRIMARY KEY (tenant_id, owner_id, object_id);


--
-- Name: jobs jobs_pkey; Type: CONSTRAINT; Schema: lerna_test_000000000000000000000001; Owner: -
--

ALTER TABLE ONLY lerna_test_000000000000000000000001.jobs
    ADD CONSTRAINT jobs_pkey PRIMARY KEY (tenant_id, owner_id, job_id);


--
-- Name: jobs jobs_tenant_id_owner_id_object_kind_object_id_phase_key; Type: CONSTRAINT; Schema: lerna_test_000000000000000000000001; Owner: -
--

ALTER TABLE ONLY lerna_test_000000000000000000000001.jobs
    ADD CONSTRAINT jobs_tenant_id_owner_id_object_kind_object_id_phase_key UNIQUE (tenant_id, owner_id, object_kind, object_id, phase);


--
-- Name: schema_migrations schema_migrations_pkey; Type: CONSTRAINT; Schema: lerna_test_000000000000000000000001; Owner: -
--

ALTER TABLE ONLY lerna_test_000000000000000000000001.schema_migrations
    ADD CONSTRAINT schema_migrations_pkey PRIMARY KEY (version);


--
-- Name: jobs jobs_tenant_id_owner_id_object_id_fkey; Type: FK CONSTRAINT; Schema: lerna_test_000000000000000000000001; Owner: -
--

ALTER TABLE ONLY lerna_test_000000000000000000000001.jobs
    ADD CONSTRAINT jobs_tenant_id_owner_id_object_id_fkey FOREIGN KEY (tenant_id, owner_id, object_id) REFERENCES lerna_test_000000000000000000000001.durable_inputs(tenant_id, owner_id, object_id);


--
-- PostgreSQL database dump complete
--

\unrestrict lernav1fixture

