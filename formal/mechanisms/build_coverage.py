#!/usr/bin/env python3
"""Assemble inventories and reject missing/duplicate architecture test rows."""
import json
from pathlib import Path
import re
from collections import Counter

ROOT = Path(__file__).resolve().parent
REPO = ROOT.parents[1]
CASE = re.compile(r"^\|\s*([A-Z]{2,3}-(?:\d+[a-z]?|P\d+))\b")


def compact(value):
    if value is None:
        return ""
    if isinstance(value, str):
        return value.replace("|", "／").replace("\n", " ")
    if isinstance(value, list):
        return "；".join(filter(None, map(compact, value)))
    return compact(json.dumps(value, ensure_ascii=False))


def main():
    expected = {}
    files = sorted((REPO / "docs/architecture").glob("*/validation.md"))
    files.append(REPO / "docs/architecture/task-kernel/recovery-and-validation.md")
    for f in files:
        for line_number, line in enumerate(f.read_text().splitlines(), 1):
            found = CASE.match(line)
            if found:
                expected[(str(f.relative_to(REPO)), found[1])] = {"line": line_number, "row": line}
    merged = {"scope": "current architecture mechanism inventory; no full runtime acceptance claim",
              "mechanisms": [], "validation_cases": [], "supplemental_cases": [], "groups": []}
    seen = Counter()
    issues = []
    for path in sorted(ROOT.glob("*/inventory.json")):
        group = path.parent.name
        inventory = json.loads(path.read_text())
        for field in ("mechanisms", "validation_cases", "supplemental_cases"):
            for entry in inventory.get(field, []):
                merged[field].append({"group": group, **entry})
                if field != "validation_cases":
                    continue
                source = entry["source"]
                key = (source["path"], entry["id"])
                seen[key] += 1
                if key not in expected:
                    issues.append(f"Unknown acceptance ID/source: {key}")
                elif source.get("line") != expected[key]["line"]:
                    issues.append(f"Stale source line: {key}")
                if entry.get("whole_acceptance_case_verified") is True:
                    issues.append(f"Unsupported full runtime claim: {key}")
        merged["groups"].append({"group": group,
            "mechanisms": len(inventory.get("mechanisms", [])),
            "validation_cases": len(inventory.get("validation_cases", []))})
    issues += [f"Missing acceptance case: {key}" for key in expected if seen[key] == 0]
    issues += [f"Duplicate acceptance case: {key}" for key, count in seen.items() if count > 1]
    overlay_path = ROOT / "cross-group-mappings.json"
    overlays = json.loads(overlay_path.read_text())["mappings"] if overlay_path.exists() else []
    mechanism_keys = {(x["group"], x["id"]) for x in merged["mechanisms"]}
    overlay_index = {}
    for entry in overlays:
        key = (entry["owner_group"], entry["mechanism_id"])
        if key not in mechanism_keys or key in overlay_index:
            issues.append(f"Invalid cross-group mechanism mapping: {key}")
        overlay_index[key] = entry
        if entry.get("whole_mechanism_verified") or entry.get("whole_acceptance_case_verified"):
            issues.append(f"Unsupported cross-group full claim: {key}")
    merged["cross_group_mappings"] = overlays
    merged["expected_case_count"] = len(expected)
    merged["issues"] = issues
    target = ROOT / "evidence/coverage.json"
    target.parent.mkdir(exist_ok=True)
    target.write_text(json.dumps(merged, ensure_ascii=False, indent=2) + "\n")

    lines = ["# 全机制与验收用例覆盖索引", "",
             "本索引由各组库存合并，逐项关联当前方案。**登记了覆盖关系不等于完整用例通过。** model-property 只表示已建模子性质；static-only 只表示结构／固定样本；runtime-required 表示需要实际提供方；uncovered 表示规则尚未进入模型。",
             "", f"当前登记 {len(merged['mechanisms'])} 个机制族、{len(merged['validation_cases'])} 条模块原编号用例及 {len(merged['supplemental_cases'])} 条内容专题本地编号用例。原始义务、模型属性和剩余边界见 [机器可读库存](../../formal/mechanisms/evidence/coverage.json)。实际工具结果见 [总报告](all-mechanisms-verification-results.md)。", ""]
    for group in merged["groups"]:
        name = group["group"]
        lines += [f"## {name}：机制", "", f"[组内结果与模型边界](../../formal/mechanisms/{name}/README.md)", "",
                  "| ID | 机制 | 库存覆盖声明 |", "| --- | --- | --- |"]
        for entry in merged["mechanisms"]:
            if entry["group"] != name:
                continue
            title = entry.get("name", entry.get("title", ""))
            status = entry.get("coverage", entry.get("classification"))
            if status is None:
                status = "abstract-partial" if entry.get("formal") else "uncovered"
            if (name, entry["id"]) in overlay_index:
                status = "partial-reused（跨组通用规则；原组状态保留）"
            lines.append(f"| {entry['id']} | {compact(title)} | {compact(status)} |")
        lines += ["", f"### {name}：原编号用例", "",
                  "下列分类针对抽象义务；真实实现的整条运行验收均未执行。每行完整刺激／判据及未覆盖细项在 JSON 库存中保留。", "",
                  "| 用例与原文 | 证据分类 | 抽象范围／剩余边界 |", "| --- | --- | --- |"]
        for entry in merged["validation_cases"]:
            if entry["group"] != name:
                continue
            source = entry["source"]["path"]
            link = "../architecture/" + source.split("docs/architecture/", 1)[1]
            scope = entry.get("covered_abstraction") or entry.get("coverage") or "逐义务见库存"
            status = entry.get("classification", "逐义务分类见库存")
            lines.append(f"| [{entry['id']}]({link})（第 {entry['source']['line']} 行） | {compact(status)} | {compact(scope)} |")
        lines.append("")
    if overlays:
        lines += ["## 跨组通用规则复用", "",
                  "以下映射补充原组未形式化的通用子规则，保留原库存与未建模边界，不增加检查次数。逐项性质、检查 ID 和假设见 [跨组映射](../../formal/mechanisms/cross-group-mappings.json)。", "",
                  "| 机制 | 可复用的局部规则 | 仍未覆盖 |", "| --- | --- | --- |"]
        for entry in overlays:
            lines.append(f"| {entry['mechanism_id']} | {compact(entry['direct_scope'])} | {compact(entry['uncovered_details'])} |")
        lines.append("")
    lines += ["## 内容专题无编号场景", "",
              "CP-LOCAL 编号只用于本次库存，不是新架构规范 ID。", "",
              "| 本地索引 | 覆盖范围 |", "| --- | --- |"]
    for entry in merged["supplemental_cases"]:
        lines.append(f"| {entry['id']} | {compact(entry.get('covered_abstraction', entry.get('coverage')))} |")
    (REPO / "docs/research/all-mechanisms-coverage.md").write_text("\n".join(lines) + "\n")
    print(json.dumps({"mechanisms": len(merged["mechanisms"]), "expected_cases": len(expected),
                      "mapped_cases": len(merged["validation_cases"]), "issues": issues}, ensure_ascii=False, indent=2))
    raise SystemExit(bool(issues))


if __name__ == "__main__":
    main()
