"""Offline checks for the protocol v1 design draft; not a runtime."""

from copy import deepcopy
from datetime import datetime
import json
import math
from pathlib import Path
import re

from jsonschema import Draft202012Validator, FormatChecker
from jsonschema.exceptions import ValidationError
from referencing import Registry, Resource


ROOT = Path(__file__).resolve().parents[1]
BASE = "https://harness.invalid/protocol/v1/"
FORMATS = FormatChecker()


@FORMATS.checks("harness-time")
def valid_time(value):
    if not isinstance(value, str):
        return True  # Schema's type keyword reports type errors.
    if not re.fullmatch(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:[0-5]\d\.\d{3}Z", value):
        return False
    try:
        datetime.strptime(value, "%Y-%m-%dT%H:%M:%S.%fZ")
        return True
    except ValueError:
        return False


@FORMATS.checks("harness-uint64")
def valid_uint64(value):
    return not isinstance(value, str) or (
        re.fullmatch(r"0|[1-9][0-9]*", value) is not None
        and int(value) <= 18446744073709551615
    )


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON key: {key}")
        result[key] = value
    return result


def invalid_constant(value):
    raise ValueError(f"non-JSON number: {value}")


def parse(text):
    return json.loads(text, object_pairs_hook=unique_object, parse_constant=invalid_constant)


def read(path):
    return parse(path.read_text())


def require(condition, message):
    if not condition:
        raise ValueError(message)


schemas = [read(path) for path in sorted((ROOT / "schemas").glob("*.schema.json"))]
for item in schemas:
    Draft202012Validator.check_schema(item)
REGISTRY = Registry().with_resources(
    (item["$id"], Resource.from_contents(item)) for item in schemas
)


def check_references(value, resolver):
    if isinstance(value, dict):
        if "$ref" in value:
            resolver.lookup(value["$ref"])
        for child in value.values():
            check_references(child, resolver)
    elif isinstance(value, list):
        for child in value:
            check_references(child, resolver)


for item in schemas:
    check_references(item, REGISTRY.resolver(item["$id"]))


def validate_ref(reference, value):
    Draft202012Validator(
        {"$ref": reference}, registry=REGISTRY, format_checker=FORMATS
    ).validate(value)


standard = read(ROOT / "schemas/standard-registry.json")
example = read(ROOT / "examples/extension-manifest.json")
TYPES, EXTENSIONS, VIEWS = {}, {}, {}
for manifest in (standard, example):
    validate_ref(BASE + "extension-manifest.schema.json", manifest)
    for descriptor in manifest["messages"]:
        key = (descriptor["type"], descriptor["type_version"])
        require(key not in TYPES, f"duplicate type: {key}")
        require(descriptor["type"].startswith(manifest["namespace"] + "."), "namespace mismatch")
        require(set(descriptor["kinds"]) == set(descriptor["payload_schemas"]), "missing kind schema")
        if "request" in descriptor["kinds"]:
            require("response" in descriptor["kinds"], "request needs response schema")
            require(descriptor["delivery"] == "reliable", "requests must be reliable")
        if descriptor["side_effecting"]:
            require(descriptor["operation_id"] == "required", "effects require operation identity")
        if descriptor["lane"] == "recovery":
            require(not descriptor["side_effecting"], "recovery lane cannot introduce effects")
        if manifest["namespace"] != "harness":
            require("control" not in descriptor["kinds"], "third party cannot define session controls")
        for reference in descriptor["payload_schemas"].values():
            REGISTRY.resolver().lookup(reference)  # No network retrieval is configured.
        TYPES[key] = descriptor
    for kind, destination, name_key in (("extensions", EXTENSIONS, "name"), ("views", VIEWS, "type")):
        for descriptor in manifest[kind]:
            key = (descriptor[name_key], descriptor["version"])
            require(key not in destination, "duplicate installed extension or view")
            REGISTRY.resolver().lookup(descriptor["schema"])
            destination[key] = descriptor


def inspect_numbers(value):
    if isinstance(value, dict):
        for child in value.values():
            inspect_numbers(child)
    elif isinstance(value, list):
        for child in value:
            inspect_numbers(child)
    elif isinstance(value, (int, float)) and not isinstance(value, bool):
        if isinstance(value, float):
            require(math.isfinite(value), "non-finite JSON number")
        if isinstance(value, int) or value.is_integer():
            require(abs(value) <= 9007199254740991, "unsafe JSON integer; use decimal string")


def check_budget_objects(value):
    if isinstance(value, list):
        for child in value:
            check_budget_objects(child)
    if not isinstance(value, dict):
        return
    amounts = value.get("amounts")
    if isinstance(amounts, list) and amounts and all(isinstance(item, dict) and "target_limit" in item for item in amounts):
        dimensions = [item["dimension"] for item in amounts]
        require(len(set(dimensions)) == len(dimensions), "duplicate budget dimension")
        if "balances" in value:
            unit_key = lambda item: tuple(item.get(k) for k in ("dimension", "unit", "currency", "scale"))
            expected = {unit_key(item) for item in amounts}
            balances = [unit_key(item) for item in value["balances"]]
            require(len(balances) == len(expected) and set(balances) == expected, "budget balances do not match units and dimensions")
    for child in value.values():
        check_budget_objects(child)


def validate_message(message):
    inspect_numbers(message)
    validate_ref(BASE + "envelope.schema.json", message)
    descriptor = TYPES.get((message["type"], message["type_version"]))
    require(descriptor is not None, "unsupported type/version")
    require(message["kind"] in descriptor["kinds"], "unsupported kind")
    # Validate the selected payload first so failures identify its local contract.
    validate_ref(descriptor["payload_schemas"][message["kind"]], message["payload"])
    if message["type"].startswith("harness."):
        validate_ref(BASE + "standard-message.schema.json", message)
    if descriptor["operation_id"] == "required":
        require("operation_id" in message, "operation_id required")
    if descriptor["operation_id"] == "forbidden":
        require("operation_id" not in message, "operation_id forbidden")
    if message["kind"] != "control":
        require(message["delivery"]["mode"] == descriptor["delivery"], "delivery disagrees with installed type")

    names = set()
    for extension in message.get("extensions", []):
        require(extension["name"] not in names, "duplicate extension name")
        names.add(extension["name"])
        installed = EXTENSIONS.get((extension["name"], extension["version"]))
        if installed is None:
            require(not extension["required"], "unsupported required extension")
            continue
        require(not installed["required"] or extension["required"], "cannot downgrade required extension")
        validate_ref(installed["schema"], extension["data"])

    payload = message["payload"]
    check_budget_objects(payload)
    limit_sets = []
    if message["type"] == "harness.core.hello":
        limit_sets.append(payload["receive_limits"])
    if message["type"] == "harness.core.welcome":
        limit_sets.extend((payload["client_receive_limits"], payload["server_receive_limits"]))
        require(payload["idle_timeout_ms"] > payload["heartbeat_ms"], "idle timeout must exceed heartbeat period")
    if message["type"] == "harness.core.capabilities":
        limit_sets.append(payload["receive_limits"])
    for limits in limit_sets:
        require(int(limits["max_inflight_bytes"]) >= limits["max_message_bytes"], "work capacity cannot hold a message")
        require(int(limits["control_reserve_bytes"]) >= limits["max_message_bytes"], "control capacity cannot hold a message")
    surface = payload if message["type"] == "harness.ui.snapshot" else (
        payload.get("result", {}) if message["type"] == "harness.ui.get" and message["kind"] == "response" else {}
    )
    if "view" in surface:
        view = surface["view"]
        installed = VIEWS.get((view["type"], view["version"]))
        require(installed is not None, "unsupported view")
        validate_ref(installed["schema"], view["data"])
        requests = {item["input_request_id"]: item for item in surface["input_requests"]}
        require(len(requests) == len(surface["input_requests"]), "duplicate snapshot input request")
        for item in requests.values():
            if "task_id" in surface and "task_id" in item:
                require(item["task_id"] == surface["task_id"], "snapshot input task mismatch")
            field_ids = [field["id"] for field in item["fields"]]
            require(len(field_ids) == len(set(field_ids)), "duplicate snapshot input field")
        if view["type"] == "harness.ui.document":
            block_ids = [block["id"] for block in view["data"]["blocks"]]
            require(len(block_ids) == len(set(block_ids)), "duplicate UI block id")
            action_ids = [action["id"] for block in view["data"]["blocks"] if block["type"] == "actions" for action in block["actions"]]
            require(len(action_ids) == len(set(action_ids)), "duplicate UI action id")
            for block in view["data"]["blocks"]:
                if block["type"] == "form":
                    item = requests.get(block["input_request_id"])
                    require(item is not None, "form lacks recoverable input description")
                    require(block["fields"] == item["fields"], "form changes authoritative input fields")
                if block["type"] == "actions":
                    for action in block["actions"]:
                        if action["intent"] == "submit_input":
                            require(action["input_request_id"] in requests, "action lacks recoverable input description")
    if message["type"] in {"harness.execution.fact", "harness.execution.result"}:
        require(payload["operation_id"] == message["operation_id"], "execution operation mismatch")
    if message["type"] == "harness.task.status":
        require(payload["control"]["effects_pending"] == payload["effects_pending"], "control effects disagree with task projection")
    if message["type"] == "harness.task.query" and message["kind"] == "response" and payload["status"] == "completed":
        state = payload["result"]
        require(state["control"]["effects_pending"] == state["effects_pending"], "query control effects disagree with task projection")
    typ = message["type"]
    if message["kind"] != "response":
        own_id = {
            "harness.brain.observation": "call_operation_id",
            "harness.task.evidence_result": "evidence_operation_id",
            "harness.execution.usage": "target_operation_id",
            "harness.coordination.delegate": "delegation_operation_id",
            "harness.coordination.admit_child": "child_submit_operation_id",
            "harness.coordination.control": "control_operation_id",
            "harness.coordination.input": "input_command_id",
            "harness.coordination.fact": "delegation_operation_id",
            "harness.coordination.input_requested": "delegation_operation_id",
            "harness.release.apply": "command_id",
            "harness.release.disable": "command_id",
            "harness.release.approval": "approval_id",
            "harness.release.command_fact": "command_id",
        }.get(typ)
        if own_id:
            require(payload[own_id] == message["operation_id"], "domain operation identity mismatch")
    if message["kind"] == "event" and "producer_endpoint" in payload:
        require(payload["producer_endpoint"] == message["source"], "fact producer differs from authenticated source")
    if typ in {"harness.release.apply", "harness.release.disable"} and message["kind"] == "request":
        require(payload["target_endpoint"] == message["target"], "release command targets another endpoint")
    if typ == "harness.release.command_fact":
        require(payload["target_endpoint"] == message["source"], "release receipt names another producer")
    if typ == "harness.release.approval":
        require(payload["authority_endpoint"] == message["source"], "approval fact names another authority")
    if typ == "harness.execution.usage":
        for item in payload["items"]:
            require(item["allocation_id"] == payload["allocation_id"], "usage allocation mismatch")
            require(item["value"]["source_endpoint"] == payload["producer_endpoint"], "usage producer mismatch")
    if typ in {"harness.ui.input", "harness.task.input"} and message["kind"] == "request" and "preview_receipt" in payload:
        receipt = payload["preview_receipt"]
        require(receipt["input_request_id"] == payload["input_request_id"], "preview receipt input mismatch")
        require(receipt["acquired_at"] < receipt["expires_at"], "preview receipt expires before acquisition")
        require(receipt["proof"]["authority_endpoint"] == receipt["host_endpoint"], "preview receipt authority differs from trusted host")
        preview_ids = [item["preview_id"] for item in receipt["previews"]]
        require(len(set(preview_ids)) == len(preview_ids), "duplicate acquired preview")
        if typ == "harness.ui.input":
            require(receipt["ui_operation_id"] == message["operation_id"], "preview receipt binds another UI operation")
            require(receipt["surface_id"] == payload["surface_id"], "preview receipt surface mismatch")
            require(receipt["host_endpoint"] == message["source"], "preview receipt belongs to another rendering host")
        else:
            require(receipt["task_id"] == payload["task_id"], "preview receipt task mismatch")
    if typ == "harness.task.query_projection" and message["kind"] == "response" and payload["status"] == "completed":
        result = payload["result"]
        require(len({p["preview_id"] for p in result["previews"]}) == len(result["previews"]), "duplicate projection preview")
        for preview in result["previews"]:
            require(preview["task_id"] == result["task_id"] and preview["projection_revision"] == result["projection_revision"], "preview belongs to another projection")
    # effects_pending also covers nonessential work. Whether pending effects can
    # alter the accepted outcome requires task evidence, absent from this envelope.


def check_values(input_request, supplied):
    fields = {field["id"]: field for field in input_request["fields"]}
    require(len(fields) == len(input_request["fields"]), "duplicate input field")
    require(set(supplied) <= set(fields), "unknown input field")
    for name, field in fields.items():
        require(not field["required"] or name in supplied, "required input missing")
        if name not in supplied:
            continue
        value = supplied[name]
        field_type = field["type"]
        if field_type in {"text", "choice"}:
            require(isinstance(value, str), "expected text input")
        if field_type == "boolean":
            require(isinstance(value, bool), "expected boolean input")
        if field_type == "number":
            require(isinstance(value, (int, float)) and not isinstance(value, bool), "expected numeric input")
            require(field.get("min", -math.inf) <= value <= field.get("max", math.inf), "input outside range")
        if field_type == "choice":
            require(value in field["options"], "invalid choice")
        if field_type == "text":
            require(len(value) <= field.get("max_length", 32768), "text input too long")


def message_scope(message, seen):
    """Resolve the fixed examples' scope from registered domain associations."""
    if message["kind"] == "response":
        return message_scope(seen[message["reply_to"]], seen)
    descriptor = TYPES[(message["type"], message["type_version"])]
    binding = descriptor["scope_binding"]
    value = message
    for segment in binding["field"].split("."):
        value = value[segment]
    return (binding["kind"], value)


def validate_flow(messages):
    seen, streams, inputs, consumed, surfaces, ui_submissions = {}, {}, {}, {}, {}, {}
    presentations, presentation_operations, projections = {}, {}, {}
    for message in messages:
        validate_message(message)
        identity = message["message_id"]
        if identity in seen:
            require(message == seen[identity], "same message id with changed content")
            continue
        seen[identity] = message
        kind, typ, payload = message["kind"], message["type"], message["payload"]
        if kind == "response":
            request = seen[message["reply_to"]]
            require(request["kind"] == "request", "response must refer to request")
            require((typ, message["type_version"]) == (request["type"], request["type_version"]), "response type mismatch")
            require(message.get("operation_id") == request.get("operation_id"), "response operation mismatch")
            require((message["source"], message["target"]) == (request["target"], request["source"]), "response route mismatch")
            if payload["status"] == "completed" and typ == "harness.execution.query":
                require(payload["result"]["operation_id"] == request["payload"]["target_operation_id"], "query returned another operation")
            if payload["status"] == "completed" and typ == "harness.task.query_result":
                require(payload["result"]["task_id"] == request["payload"]["task_id"], "query returned another task result")
            if payload["status"] == "completed" and isinstance(payload.get("result"), dict):
                result = payload["result"]
                for field in ("task_id", "surface_id", "collection_id", "memory_id", "content_id", "view_id", "delegation_operation_id", "approval_id", "command_id", "call_operation_id"):
                    if field in request["payload"] and field in result:
                        require(result[field] == request["payload"][field], f"response {field} differs from request")
            repeated_presentation = False
            if typ == "harness.ui.set_presentation":
                operation = (request["source"], request["operation_id"])
                original = presentation_operations.get(operation)
                repeated_presentation = original is not None
                if repeated_presentation:
                    require(original == (request["payload"], payload), "presentation retry changed original decision")
                else:
                    presentation_operations[operation] = (request["payload"], payload)
            if payload["status"] == "completed" and typ in {"harness.ui.get", "harness.ui.set_presentation"}:
                result = payload["result"]
                require(result["surface_id"] == request["payload"]["surface_id"], "response returned another surface")
                key = (request["source"], result["surface_id"])
                if typ == "harness.ui.set_presentation":
                    if not repeated_presentation:
                        expected = int(request["payload"]["expected_revision"])
                        prior = presentations.get(key, {"revision": "0", "state": "closed"})
                        require(expected == int(prior["revision"]), "presentation accepted a stale precondition")
                        require(int(result["revision"]) == expected + 1, "presentation revision must advance once")
                        require(result["state"] == request["payload"]["state"], "presentation returned a different intent")
                        presentations[key] = {"revision": result["revision"], "state": result["state"]}
                else:
                    prior = presentations.get(key)
                    if prior is not None:
                        require(result["presentation"] == prior, "sample query lost current presentation state")
                    presentations[key] = result["presentation"]
        delivery = message.get("delivery", {})
        if delivery.get("mode") == "reliable":
            key = (message["source"], message["target"], delivery["stream_id"])
            lane = TYPES[(typ, message["type_version"])]["lane"]
            declared = delivery["scope"]
            scope = (declared["kind"], declared["id"])
            require(scope == message_scope(message, seen), "declared scope does not match domain association")
            previous, bound_lane, bound_scope = streams.get(key, (0, lane, scope))
            require(int(delivery["seq"]) == previous + 1, "sample has noncontiguous sequence")
            require(lane == bound_lane, "sample mixes traffic lanes")
            require(scope == bound_scope, "sample mixes independent recovery scopes")
            streams[key] = (int(delivery["seq"]), lane, scope)
        if typ == "harness.core.ack":
            key = (payload["source"], payload["target"], payload["stream_id"])
            require(message["source"] == payload["target"] and message["target"] == payload["source"], "ack must come from destination")
            require(int(payload["through_seq"]) <= streams[key][0], "ack exceeds sent position")
        if typ in {"harness.task.input_requested", "harness.ui.input_requested"}:
            item = payload["input"]
            prior = inputs.setdefault(item["input_request_id"], item)
            require(prior == item, "same input request changed semantics")
        snapshot = payload if typ == "harness.ui.snapshot" else (
            payload.get("result", {}) if typ == "harness.ui.get" and kind == "response" else {}
        )
        for item in snapshot.get("input_requests", []):
            prior = inputs.setdefault(item["input_request_id"], item)
            require(prior == item, "snapshot changed an existing input request")
        if typ == "harness.ui.snapshot":
            previous = surfaces.get(payload["surface_id"], 0)
            require(int(payload["revision"]) > previous, "snapshot revision regressed")
            surfaces[payload["surface_id"]] = int(payload["revision"])
        if typ == "harness.ui.delta":
            require(surfaces[payload["surface_id"]] == int(payload["base_revision"]), "delta base mismatch")
            require(int(payload["revision"]) > int(payload["base_revision"]), "delta revision must advance")
            surfaces[payload["surface_id"]] = int(payload["revision"])
        if typ == "harness.task.query_projection" and kind == "response" and payload["status"] == "completed":
            result = payload["result"]
            projections[result["task_id"], result["projection_revision"]] = result
        if typ in {"harness.ui.input", "harness.task.input"} and kind == "request":
            input_request = inputs[payload["input_request_id"]]
            check_values(input_request, payload["values"])
            required = set(input_request.get("required_preview_ids", []))
            if required:
                receipt = payload["preview_receipt"]
                require(receipt["task_id"] == input_request["task_id"], "preview receipt binds another task")
                projection = projections[receipt["task_id"], receipt["projection_revision"]]
                binding = next(b for b in projection["input_bindings"] if b["input_request_id"] == payload["input_request_id"])
                require(set(binding["required_preview_ids"]) == required, "input preview requirements changed")
                available = {p["preview_id"]: p["content"]["sha256"] for p in projection["previews"]}
                acquired = {p["preview_id"]: p["content_sha256"] for p in receipt["previews"]}
                require(required <= set(available) and required <= set(acquired), "required preview missing")
                require(all(acquired[k] == available[k] for k in required), "acquired preview differs from authoritative content")
                if typ == "harness.task.input" and receipt["ui_operation_id"] in ui_submissions:
                    require(receipt == ui_submissions[receipt["ui_operation_id"]]["preview_receipt"], "UI forwarding changed host receipt")
            if typ == "harness.ui.input":
                ui_submissions[message["operation_id"]] = payload
            else:
                require(inputs[payload["input_request_id"]].get("task_id") == payload["task_id"], "wrong task input binding")
        if typ == "harness.task.input" and kind == "response" and payload["status"] == "completed":
            input_id = payload["result"]["input_request_id"]
            require(input_id not in consumed, "sample consumes input twice")
            consumed[input_id] = message["operation_id"]
        if typ == "harness.ui.input_result" and payload["status"] == "applied":
            original = ui_submissions[message["operation_id"]]
            require(original["input_request_id"] == payload["input_request_id"], "UI result input mismatch")
            if "task_id" in payload:
                require(payload["input_request_id"] in consumed, "UI applied before task consumption")


domain_flows = {
    path.stem: read(path) for path in sorted((ROOT / "examples").glob("*-domain-flow.json"))
}
for messages in domain_flows.values():
    validate_flow(messages)
flow = read(ROOT / "examples/task-ui-flow.json")
result_flow = read(ROOT / "examples/result-and-presentation.json")
extension_message = read(ROOT / "examples/extension-message.json")
validate_flow(flow)
validate_flow(result_flow)
validate_message(extension_message)
recovery_samples = read(ROOT / "examples/recovery-and-cancel.json")
for message in recovery_samples:
    validate_message(message)
unknown_resume = deepcopy(next(message for message in recovery_samples if message["type"] == "harness.core.resumed"))
unknown_resume["payload"]["streams"][0]["status"] = "unknown"
del unknown_resume["payload"]["streams"][0]["through_seq"]
validate_message(unknown_resume)
ready_resume = deepcopy(next(message for message in recovery_samples if message["type"] == "harness.core.resumed"))
ready_resume["payload"]["streams"][0]["status"] = "ready"
validate_message(ready_resume)
http_samples = read(ROOT / "examples/http-exchanges.json")
for exchange in http_samples:
    validate_ref(exchange["schema"], exchange["body"])
    for allowed_type in exchange["body"].get("allowed_types", []):
        require((allowed_type, 1) in TYPES, "HTTP grant allows an unregistered standard type")


rejected_cases = 0


def expect_failure(label, callback):
    global rejected_cases
    try:
        callback()
    except (ValueError, KeyError, TypeError, ValidationError):
        rejected_cases += 1
        return
    raise AssertionError(f"invalid case unexpectedly passed: {label}")


for case in read(ROOT / "validation/invalid-cases.json"):
    if "raw" in case:
        expect_failure(case["name"], lambda: parse(case["raw"]))
        continue
    if "message" in case:
        expect_failure(case["name"], lambda: validate_message(case["message"]))
        continue
    original = extension_message if case["base"] == "extension" else next(
        message for message in flow + result_flow + [m for messages in domain_flows.values() for m in messages]
        if message["type"] == case["base"] and message["kind"] == case.get("kind", "request")
        and message["type_version"] == case.get("type_version", message["type_version"])
    )
    modified = deepcopy(original)
    for edit in case["edits"]:
        parts = edit["path"].strip("/").split("/")
        current = modified
        for part in parts[:-1]:
            current = current[int(part)] if isinstance(current, list) else current[part]
        key = int(parts[-1]) if isinstance(current, list) else parts[-1]
        if edit["op"] == "remove":
            del current[key]
        else:
            current[key] = edit["value"]
    expect_failure(case["name"], lambda: validate_message(modified))

optional = deepcopy(extension_message)
optional["extensions"] = [{"name":"net.example.unknown", "version":1, "required":False, "data":{}}]
validate_message(optional)
conflicting = deepcopy(flow)
changed = deepcopy(next(message for message in flow if message["type"] == "harness.task.submit" and message["kind"] == "request"))
changed["payload"]["goal"] = "A changed goal under the same message id"
conflicting.append(changed)
expect_failure("same id, changed content", lambda: validate_flow(conflicting))

mixed_scope = deepcopy(flow)
task_message = next(message for message in mixed_scope if message["type"] == "harness.task.status")
snapshot_message = next(message for message in mixed_scope if message["type"] == "harness.ui.snapshot")
snapshot_message["delivery"]["stream_id"] = task_message["delivery"]["stream_id"]
snapshot_message["delivery"]["seq"] = str(int(task_message["delivery"]["seq"]) + 1)
expect_failure("task and surface mixed in one scoped stream", lambda: validate_flow(mixed_scope))
forged_scope = deepcopy(flow)
scoped_request = next(message for message in forged_scope if message["type"] == "harness.task.submit" and message["kind"] == "request")
scoped_request["delivery"]["scope"]["id"] = "00000000-0000-4000-8000-000000009997"
expect_failure("scope tag disagrees with task identity", lambda: validate_flow(forged_scope))

changed_mode = deepcopy(flow)
last_snapshot = next(message for message in reversed(changed_mode) if message["type"] == "harness.ui.snapshot")
last_snapshot["delivery"].pop("scope")
expect_failure("scope removed midway through a stream", lambda: validate_flow(changed_mode))

for typ in ("harness.task.status", "harness.task.result"):
    pending = deepcopy(next(message for message in flow if message["type"] == typ))
    pending["payload"].update(state="completed", effects_pending=True)
    if "control" in pending["payload"]:
        pending["payload"]["control"]["effects_pending"] = True
    validate_message(pending)

wrong_presentation = deepcopy(result_flow)
last_get = next(message for message in reversed(wrong_presentation) if message["type"] == "harness.ui.get" and message["kind"] == "response")
last_get["payload"]["result"]["presentation"]["state"] = "closed"
expect_failure("late close overwrote explicit reopen", lambda: validate_flow(wrong_presentation))

wrong_result = deepcopy(result_flow)
result_response = next(message for message in wrong_result if message["type"] == "harness.task.query_result" and message["kind"] == "response")
result_response["payload"]["result"]["task_id"] = "00000000-0000-4000-8000-000000009999"
expect_failure("result query returned another task", lambda: validate_flow(wrong_result))

changed_decision = deepcopy(result_flow)
first_write = next(message for message in changed_decision if message["type"] == "harness.ui.set_presentation" and message["kind"] == "request")
first_response = next(message for message in changed_decision if message.get("reply_to") == first_write["message_id"])
rejected = deepcopy(first_response)
rejected["message_id"] = "00000000-0000-4000-8000-000000009998"
rejected["payload"] = {"status": "rejected", "error": {"code": "core.precondition_failed", "message": "Stored rejection", "retry": "after_change"}}
changed_decision.insert(changed_decision.index(first_response), rejected)
positions = {}
for message in changed_decision:
    delivery = message.get("delivery", {})
    if delivery.get("mode") == "reliable":
        key = (message["source"], message["target"], delivery["stream_id"])
        positions[key] = positions.get(key, 0) + 1
        delivery["seq"] = str(positions[key])
expect_failure("rejected presentation operation later accepted", lambda: validate_flow(changed_decision))

duplicate_inputs = deepcopy(next(message for message in result_flow if message["type"] == "harness.ui.get" and message["kind"] == "response"))
# The installed custom view is independent of the standard form model.
duplicate_inputs["payload"]["result"]["view"] = {
    "type": "com.example.camera.preview", "version": 1,
    "data": {"content": next(message for message in result_flow if message["type"] == "harness.task.query_result" and message["kind"] == "response")["payload"]["result"]["artifacts"][0]}
}
validate_message(duplicate_inputs)
duplicate_inputs["payload"]["result"]["input_requests"] *= 2
expect_failure("duplicate input descriptions in a custom view", lambda: validate_message(duplicate_inputs))

for label, mutation in (
    ("required preview receipt omitted", lambda payload: payload.pop("preview_receipt")),
    ("preview receipt names different content", lambda payload: payload["preview_receipt"]["previews"][0].update(content_sha256="f" * 64)),
):
    changed = deepcopy(flow)
    submission = next(m for m in changed if m["type"] == "harness.ui.input" and m["kind"] == "request")
    mutation(submission["payload"])
    expect_failure(label, lambda: validate_flow(changed))

print(f"PASS: {len(schemas)} schemas, {len(standard['messages'])} standard type/version entries, 2 manifests, {len(flow) + len(result_flow) + sum(len(messages) for messages in domain_flows.values())} linked messages, extension and optional-extension examples.")
print(f"PASS: {len(recovery_samples)} recovery/cancel messages and {len(http_samples)} HTTP bodies.")
print(f"PASS: {rejected_cases} invalid cases rejected; completed/pending regressions passed. Runtime delivery, authorization and effects are not tested.")
