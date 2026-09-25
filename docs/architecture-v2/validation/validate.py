#!/usr/bin/env python3
"""Validate documentation fixtures; this is not a task-runtime implementation."""
import json
from pathlib import Path

from jsonschema import Draft202012Validator, FormatChecker

ROOT = Path(__file__).resolve().parents[1]
SCHEMA = json.loads((ROOT / "schemas/task-outcome.schema.json").read_text())
Draft202012Validator.check_schema(SCHEMA)
VALIDATOR = Draft202012Validator(SCHEMA, format_checker=FormatChecker())


def errors(snapshot):
    structural = [error.message for error in VALIDATOR.iter_errors(snapshot)]
    if structural:
        return structural
    if snapshot["task"]["status"] != "succeeded":
        return []
    task, result = snapshot["task"], snapshot["result"]
    failures = []
    if result["task_id"] != task["task_id"]:
        failures.append("result belongs to another task")
    if result["goal_revision"] != task["goal_revision"]:
        failures.append("result belongs to another goal revision")
    conditions = result["condition_results"]
    indexed = {item["requirement_id"]: item for item in conditions}
    if len(indexed) != len(conditions):
        failures.append("duplicate requirement result")
    required = set(task["required_requirement_ids"])
    if not required <= indexed.keys():
        failures.append("missing required condition")
    artifacts = result["artifact_refs"]
    for artifact in artifacts:
        if artifact["tenant_id"] != task["tenant_id"]:
            failures.append("artifact belongs to another tenant")
    for condition in conditions:
        if condition["goal_revision"] != task["goal_revision"]:
            failures.append("condition belongs to another goal revision")
        if condition["artifact_ref"] not in artifacts:
            failures.append("condition validates a different artifact")
        for evidence in condition["evidence_refs"]:
            if evidence["tenant_id"] != task["tenant_id"]:
                failures.append("evidence belongs to another tenant")
        if condition["requirement_id"] in required and condition["verdict"] != "pass":
            failures.append("required condition did not pass")
    levels = {"verified": 0, "assessed": 1, "user_accepted": 2}
    if required <= indexed.keys():
        weakest = max(levels[indexed[key]["basis"]] for key in required)
        if levels[result["completion_basis"]] != weakest:
            failures.append("completion basis differs from required evidence")
    return failures


def main():
    valid_count = invalid_count = 0
    for path in sorted((ROOT / "examples").glob("*.json")):
        if path.name == "invalid-cases.json":
            continue
        found = errors(json.loads(path.read_text()))
        if found:
            raise AssertionError(f"{path.name}: {found}")
        valid_count += 1
    cases = json.loads((ROOT / "examples/invalid-cases.json").read_text())
    for case in cases:
        if not errors(case["snapshot"]):
            raise AssertionError(f"invalid case unexpectedly passed: {case['name']}")
        invalid_count += 1
    print(f"PASS: {valid_count} valid examples; {invalid_count} invalid examples rejected")
    print("Scope: schema and document fixture semantics only; runtime behavior not tested")


if __name__ == "__main__":
    main()
