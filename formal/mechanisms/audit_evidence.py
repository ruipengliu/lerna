#!/usr/bin/env python3
"""Audit delivered evidence, source coverage and document paths separately from proofs."""
from collections import Counter
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import re
from urllib.parse import unquote

ROOT = Path(__file__).resolve().parent
REPO = ROOT.parents[1]


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    issues = []
    final_dir = ROOT / "evidence/final"
    manifest = json.loads((final_dir / "manifest.json").read_text())
    if not manifest.get("finished_utc") or not manifest.get("all_expectations_matched"):
        issues.append("Final run has not completed with all expectations matched")
    baseline = json.loads((ROOT / "evidence/architecture-baseline.json").read_text())["files"]
    reviewed = json.loads((ROOT / "evidence/architecture-changes-reviewed.json").read_text())["reviews"]
    original = json.loads((REPO / "formal/handoff/evidence/checked/manifest.json").read_text())
    for label, sources in (("final", manifest["sources"]), ("architecture", baseline),
                           ("reused-handoff", original["sources"])):
        for name, expected in sources.items():
            path = REPO / name
            if not path.is_file() or sha(path) != expected:
                revision = reviewed.get(name) if label == "architecture" else None
                if not (revision and path.is_file() and revision["baseline_sha256"] == expected
                        and revision["reviewed_sha256"] == sha(path)
                        and sha(REPO / revision["baseline_snapshot"]) == expected
                        and sha(REPO / revision["reviewed_snapshot"]) == sha(path)):
                    issues.append(f"{label}: changed or missing source {name}")
    for result in original["checks"]:
        path = REPO / "formal/handoff/evidence/checked" / f"{result['name']}.log"
        if not path.is_file() or sha(path) != result["log_sha256"]:
            issues.append(f"Changed reused handoff log: {result['name']}")

    registries = []
    for registry in sorted(ROOT.glob("*/checks.json")):
        group = registry.parent
        checks = json.loads(registry.read_text())["checks"]
        registries.extend({**x, "group": group.name} for x in checks)
        for suffix, field in (("cfg", "config"), ("tla", "model"), ("lean", "source")):
            registered = {x[field] for x in checks if field in x}
            present = {x.name for x in group.glob(f"*.{suffix}")}
            if registered != present:
                issues.append(f"{group.name}: registration mismatch for {suffix}: {registered ^ present}")
    ids = Counter(x["id"] for x in registries)
    if any(n != 1 for n in ids.values()):
        issues.append("Duplicate check IDs")
    if {(x["group"], x["id"]) for x in registries} != {(x["group"], x["id"]) for x in manifest["checks"]}:
        issues.append("Final run does not match registered checks")

    axiom_lines = []
    for result in manifest["checks"]:
        path = final_dir / f"{result['id']}.log"
        if sha(path) != result["log_sha256"]:
            issues.append(f"Changed log: {path.name}")
        if not result["matched_expectation"]:
            issues.append(f"Unexpected result: {result['id']}")
        if result["kind"] == "tlc" and result["expected_exit"] == 0 and result.get("remaining") != 0:
            issues.append(f"Incomplete positive search: {result['id']}")
        if result["kind"] == "lean":
            output = path.read_text()
            axiom_lines.extend(x for x in output.splitlines() if "depends on axioms:" in x or "does not depend on any axioms" in x)
            if any(word in output for word in ("sorryAx", "Lean.ofReduceBool", "error:", "warning:")):
                issues.append(f"Unacceptable Lean diagnostic: {result['id']}")
            for match in re.findall(r"depends on axioms:\s*\[([^]]*)\]", output):
                if set(filter(None, map(str.strip, match.split(",")))) - {"propext", "Classical.choice", "Quot.sound"}:
                    issues.append(f"Unexpected axiom: {result['id']}: {match}")

    lean_files = sorted(ROOT.glob("*/*.lean"))
    for source in lean_files:
        text = re.sub(r"/-.*?-/", "", source.read_text(), flags=re.S)
        text = re.sub(r"--[^\n]*", "", text)
        if re.search(r"\b(sorry|admit|native_decide)\b|^\s*(axiom|constant)\b", text, re.M):
            issues.append(f"Proof placeholder or custom axiom: {source.relative_to(REPO)}")

    documents = [REPO / "docs/research/all-mechanisms-verification-results.md",
                 REPO / "docs/research/all-mechanisms-coverage.md",
                 REPO / "docs/research/handoff-verification-results.md", ROOT / "README.md"]
    documents += sorted(ROOT.glob("*/README.md"))
    links = 0
    for document in documents:
        for target in re.findall(r"\[[^\]]*\]\(([^)]+)\)", document.read_text()):
            target = target.strip("<>")
            if "://" in target or target.startswith(("#", "mailto:")):
                continue
            path = (document.parent / unquote(target.split("#", 1)[0])).resolve()
            links += 1
            if not path.exists():
                issues.append(f"Missing linked path in {document.relative_to(REPO)}: {target}")
        if "<!-- VERIFIED_STATISTICS -->" in document.read_text():
            issues.append(f"Unfilled statistics in {document.name}")

    coverage = json.loads((ROOT / "evidence/coverage.json").read_text())
    issues.extend(coverage["issues"])
    protocol = json.loads((ROOT / "evidence/protocol-static.json").read_text())
    if protocol["exit_code"] != 0 or not protocol["output_matches_saved_log"]:
        issues.append("Protocol static validation failed")
    if sha(ROOT / "evidence/protocol-static.log") != protocol["log_sha256"]:
        issues.append("Protocol log changed")
    for name, expected in protocol["inputs"].items():
        if sha(REPO / name) != expected:
            issues.append(f"Protocol input changed: {name}")
    overlay = json.loads((ROOT / "cross-group-mappings.json").read_text())
    for name, expected in overlay["source_sha256"].items():
        if sha(REPO / name) != expected:
            issues.append(f"Changed cross-group input: {name}")
    for mapping in overlay["mappings"]:
        for reused in mapping["formal_mappings"]:
            if not (REPO / reused["model"]).is_file():
                issues.append(f"Missing cross-group model: {reused['model']}")
            if not reused["checkIDs"]:
                continue  # Static protocol evidence has no TLC/Lean registration ID.
            evidence = json.loads((REPO / reused["evidence_manifest"]).read_text())
            checks = {x.get("id", x.get("name")): x for x in evidence["checks"]}
            for check_id in reused["checkIDs"]:
                check = checks.get(check_id)
                if check is None or not check["matched_expectation"]:
                    issues.append(f"Missing or failed reused check: {mapping['mechanism_id']} / {check_id}")
    renderer = ROOT / "evidence/verification-map.png"
    if not renderer.is_file():
        issues.append("Missing rendered verification diagram")
    result = {
        "checked_utc": datetime.now(timezone.utc).isoformat(),
        "scope": "static evidence and link-path audit; does not prove implementation correctness",
        "registered_checks": len(registries), "final_run_sources": len(manifest["sources"]),
        "architecture_baseline_files": len(baseline), "reused_handoff_sources": len(original["sources"]),
        "architecture_changes_reviewed": len(reviewed), "protocol_inputs_hashed": len(protocol["inputs"]),
        "positive_tlc_completed": sum(x["kind"] == "tlc" and x["expected_exit"] == 0 for x in manifest["checks"]),
        "lean_files": len(lean_files), "printed_axiom_audits": len(axiom_lines),
        "local_link_paths_checked": links, "acceptance_rows": coverage["expected_case_count"],
        "diagram": {"path": str(renderer.relative_to(REPO)), "sha256": sha(renderer),
                    "renderer": "mmdc 11.12.0 using installed Google Chrome",
                    "inspection": "rendered and visually inspected separately; no clipped labels or edges"},
        "supplemental_inputs": {str(p.relative_to(REPO)): sha(p) for p in
                                (ROOT / "build_coverage.py", Path(__file__), ROOT / "evidence/coverage.json",
                                 ROOT / "cross-group-mappings.json", ROOT / "evidence/architecture-changes-reviewed.json",
                                 ROOT / "evidence/protocol-static.json") if p.exists()},
        "issues": issues, "passed": not issues,
    }
    (ROOT / "evidence/static-checks.json").write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps(result, ensure_ascii=False, indent=2))
    raise SystemExit(bool(issues))


if __name__ == "__main__":
    main()
