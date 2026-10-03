import { DatabaseSync } from "node:sqlite";
import { chmodSync, existsSync, lstatSync } from "node:fs";
import { dirname } from "node:path";
import { createHash, randomUUID } from "node:crypto";
import { canonical, newID, parseStrict } from "@harness/sdk";
import { reject } from "./error";
import { object, type Document, type Config, type Principal } from "./types";
import manifest from "./contracts.gen.json";

// 每实例只持有一个连接；所有业务裁决使用不含 await 的短事务。
export class Store {
  readonly db: DatabaseSync;
  readonly id: string;
  private epoch: string = randomUUID();
  private priority = false;
  constructor(
    readonly config: Config,
    migrate = false,
    maintenance = false,
  ) {
    const directory = lstatSync(dirname(config.database));
    if (
      !directory.isDirectory() ||
      directory.isSymbolicLink() ||
      directory.uid !== process.getuid?.() ||
      directory.mode & 0o077
    )
      reject("forbidden", "private_database_directory_required");
    if (config.expected_database_id && !existsSync(config.database))
      reject("forbidden", "original_database_missing");
    if (existsSync(config.database)) {
      const file = lstatSync(config.database);
      if (
        !file.isFile() ||
        file.isSymbolicLink() ||
        file.uid !== process.getuid?.() ||
        file.mode & 0o077
      )
        reject("forbidden", "private_database_file_required");
    }
    if (!migrate && !existsSync(config.database))
      reject("invalid_state", "explicit_migration_required");
    this.db = new DatabaseSync(config.database);
    this.db.exec(
      "PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=1000; PRAGMA trusted_schema=OFF",
    );
    chmodSync(config.database, 0o600);
    const frozen = canonical({
      system: config.system,
      tenant_id: config.tenant_id,
      owner_id: config.owner_id,
      memory: config.memory ?? null,
      model_profile_ref: config.model_profile_ref ?? null,
      answer_schema_ref: config.answer_schema_ref ?? null,
      managed_root: config.managed_root ?? null,
      binding_ref: config.binding_ref ?? null,
      install_lock_ref: config.install_lock_ref ?? null,
      keys: config.keys ?? [],
      core_digest: manifest.core_digest,
      methods_digest: `sha256:${createHash("sha256").update(canonical(manifest.methods[config.system])).digest("hex")}`,
      ...(config.foreign_sources?.length
        ? { foreign_sources: config.foreign_sources, source_methods: manifest.source_methods }
        : {}),
    });
    if (migrate) {
      this.db.exec(
        "BEGIN IMMEDIATE; CREATE TABLE IF NOT EXISTS metadata(key TEXT PRIMARY KEY,value TEXT NOT NULL); CREATE TABLE IF NOT EXISTS records(namespace TEXT NOT NULL,id TEXT NOT NULL,revision INTEGER NOT NULL,body TEXT NOT NULL,PRIMARY KEY(namespace,id)); CREATE TABLE IF NOT EXISTS history(namespace TEXT NOT NULL,id TEXT NOT NULL,revision INTEGER NOT NULL,body TEXT NOT NULL,PRIMARY KEY(namespace,id,revision)); CREATE TABLE IF NOT EXISTS content_bytes(key TEXT PRIMARY KEY,body BLOB NOT NULL); COMMIT;",
      );
      this.tx(() => {
        const saved = this.meta("identity");
        const identity = canonical([config.system, config.tenant_id, config.owner_id]);
        if (saved && saved !== identity) reject("idempotency_conflict", "database_owner_changed");
        this.meta("identity", identity);
        if (this.meta("configuration") && this.meta("configuration") !== frozen)
          reject("idempotency_conflict", "owner_configuration_changed");
        this.meta("configuration", frozen);
        this.meta("format", "alternate-ts-local/1");
        if (!this.meta("database_id")) this.meta("database_id", newID("database"));
        for (const p of config.credentials) {
          if (!this.get("credentials", p.subject_id)) this.put("credentials", p.subject_id, p);
        }
        for (const use of config.uses ?? []) {
          const id = String(use.use_id);
          if (this.get("uses", id) && canonical(this.get("uses", id)) !== canonical(use))
            reject("idempotency_conflict", "original_use_changed");
          if (!this.get("uses", id)) this.put("uses", id, use);
        }
      }, false);
    }
    if (
      this.meta("format") !== "alternate-ts-local/1" ||
      this.meta("identity") !== canonical([config.system, config.tenant_id, config.owner_id])
    )
      reject("invalid_state", "explicit_migration_required");
    if (this.meta("configuration") !== frozen)
      reject("idempotency_conflict", "owner_configuration_changed");
    this.id = this.meta("database_id");
    if (!/^database_[0-9a-f]{32}$/.test(this.id))
      reject("invalid_state", "explicit_database_identity_migration_required");
    if (config.expected_database_id && config.expected_database_id !== this.id)
      reject("forbidden", "original_database_identity_mismatch");
    if (!migrate) {
      if (maintenance) this.epoch = this.meta("instance_epoch");
      else this.tx(() => this.meta("instance_epoch", this.epoch), false);
    }
  }
  assertCurrent(): void {
    if (this.meta("instance_epoch") !== this.epoch) reject("forbidden", "owner_instance_fenced");
  }
  now(): number {
    return Math.max(Date.now(), Number(this.meta("clock") || 0));
  }
  tx<T>(fn: () => T, fenced = true, priority = false): T {
    const originalPriority = this.priority;
    this.priority = priority;
    this.db.exec("BEGIN IMMEDIATE");
    try {
      if (fenced) this.assertCurrent();
      this.meta("clock", String(this.now()));
      const result = fn();
      try {
        this.db.exec("COMMIT");
      } catch (error) {
        if (!this.db.isTransaction)
          reject("effect_unknown", "commit_outcome_unknown_query_original");
        throw error;
      }
      return result;
    } catch (error) {
      if (this.db.isTransaction) this.db.exec("ROLLBACK");
      throw error;
    } finally {
      this.priority = originalPriority;
    }
  }
  meta(key: string, value?: string): string {
    if (value !== undefined)
      this.db
        .prepare(
          "INSERT INTO metadata VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value",
        )
        .run(key, value);
    const row = this.db.prepare("SELECT value FROM metadata WHERE key=?").get(key);
    return row ? String(row.value) : "";
  }
  get<T = Document>(namespace: string, id: string, revision = 0): T | undefined {
    const row = revision
      ? this.db
          .prepare("SELECT body FROM history WHERE namespace=? AND id=? AND revision=?")
          .get(namespace, id, revision)
      : this.db.prepare("SELECT body FROM records WHERE namespace=? AND id=?").get(namespace, id);
    return row ? (parseStrict(String(row.body)) as T) : undefined;
  }
  require<T = Document>(namespace: string, id: string, revision = 0): T {
    const value = this.get<T>(namespace, id, revision);
    if (!value) reject("not_found", "original_object_not_found");
    return value;
  }
  put(namespace: string, id: string, value: unknown): void {
    const body = canonical(value);
    parseStrict(body);
    const row = this.db
      .prepare("SELECT revision FROM records WHERE namespace=? AND id=?")
      .get(namespace, id);
    const revision = row ? Number(row.revision) + 1 : 1;
    const priority = this.priority || ["credentials", "denials"].includes(namespace);
    if (
      !row &&
      Number(this.db.prepare("SELECT count(*) AS n FROM records").get()?.n) >=
        (priority ? 1000 : 900)
    )
      reject("overloaded", "durable_record_capacity");
    if (
      Number(this.db.prepare("SELECT count(*) AS n FROM history").get()?.n) >=
      (priority ? 10000 : 9000)
    )
      reject("overloaded", "durable_history_capacity");
    this.db
      .prepare(
        "INSERT INTO records VALUES(?,?,?,?) ON CONFLICT(namespace,id) DO UPDATE SET revision=excluded.revision,body=excluded.body",
      )
      .run(namespace, id, revision, body);
    this.db.prepare("INSERT INTO history VALUES(?,?,?,?)").run(namespace, id, revision, body);
  }
  list(namespace: string, after = "", limit = 200): Document[] {
    if (!Number.isInteger(limit) || limit < 1 || limit > 1000)
      reject("invalid_request", "invalid_scan_bound");
    return this.db
      .prepare("SELECT body FROM records WHERE namespace=? AND id>? ORDER BY id LIMIT ?")
      .all(namespace, after, limit)
      .map((row) => object(parseStrict(String(row.body))));
  }
  credential(subject: string): Principal {
    return this.require<Principal>("credentials", subject);
  }
  close(): void {
    this.db.close();
  }
}
