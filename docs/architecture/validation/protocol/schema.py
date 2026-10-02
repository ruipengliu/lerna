"""Load only local, frozen draft assets; no network schema resolution."""
import json
from pathlib import Path
from jsonschema import Draft202012Validator, FormatChecker

ROOT = Path(__file__).resolve().parents[4] / 'contracts'
SCHEMA = json.loads((ROOT / 'schemas/protocol.schema.json').read_text())
REGISTRY = json.loads((ROOT / 'schemas/methods.json').read_text())
METHODS = REGISTRY['methods']
Draft202012Validator.check_schema(SCHEMA)


def validate(name, value):
    validator = Draft202012Validator(
        {'$schema': SCHEMA['$schema'], '$defs': SCHEMA['$defs'], '$ref': '#/$defs/' + name},
        format_checker=FormatChecker(),
    )
    return ['schema: ' + '/'.join(map(str, e.absolute_path)) + ': ' + e.message
            for e in validator.iter_errors(value)]


def walk(value):
    if isinstance(value, dict):
        yield value
        for item in value.values():
            yield from walk(item)
    elif isinstance(value, list):
        for item in value:
            yield from walk(item)
