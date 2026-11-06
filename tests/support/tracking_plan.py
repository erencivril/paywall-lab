"""Turns tracking-plan.yaml into one JSON Schema per event.

Run `python -m tests.support.tracking_plan <dir>` to write the schemas to
disk, e.g. to share them with another team.
"""
import json
import sys
from pathlib import Path

import yaml

PLAN_PATH = Path(__file__).resolve().parents[2] / "tracking-plan.yaml"
UUID_PATTERN = "^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"
JAN_1_2020_MS = 1_577_836_800_000


def load_plan(path=PLAN_PATH):
    with open(path, encoding="utf-8") as f:
        return yaml.safe_load(f)


def _property_schema(spec):
    schema = {"type": spec["type"]}
    for plan_key, schema_key in (
        ("enum", "enum"),
        ("pattern", "pattern"),
        ("minimum", "minimum"),
        ("min_length", "minLength"),
        ("description", "description"),
    ):
        if plan_key in spec:
            schema[schema_key] = spec[plan_key]
    return schema


def event_schema(name, spec):
    props = spec["properties"]
    required = [p for p, s in props.items() if s.get("required", True)]
    return {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "$id": f"paywall-lab/events/{name}",
        "title": name,
        "description": spec["description"],
        "type": "object",
        "additionalProperties": False,
        "required": ["insert_id", "event_type", "user_id", "time", "platform", "event_properties"],
        "properties": {
            "insert_id": {"type": "string", "pattern": UUID_PATTERN},
            "event_type": {"const": name},
            "user_id": {"type": "string", "minLength": 1, "maxLength": 128},
            "time": {"type": "integer", "minimum": JAN_1_2020_MS},
            "platform": {"enum": spec["platforms"]},
            "event_properties": {
                "type": "object",
                "additionalProperties": False,
                "required": required,
                "properties": {p: _property_schema(s) for p, s in props.items()},
            },
        },
    }


def build_schemas(plan=None):
    plan = plan or load_plan()
    return {name: event_schema(name, spec) for name, spec in plan["events"].items()}


def write_schemas(directory):
    out = Path(directory)
    out.mkdir(parents=True, exist_ok=True)
    for name, schema in build_schemas().items():
        (out / f"{name}.schema.json").write_text(json.dumps(schema, indent=2) + "\n", encoding="utf-8")
    return out


if __name__ == "__main__":
    target = write_schemas(sys.argv[1] if len(sys.argv) > 1 else "reports/schemas")
    print(f"wrote {len(build_schemas())} schemas to {target}")
