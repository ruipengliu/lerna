"""Resolve historical evidence paths without consulting the current architecture."""
from pathlib import Path

FORMAL = Path(__file__).resolve().parent
REPO = FORMAL.parents[2]
ARCHITECTURE = FORMAL.parent / "architecture-2026-09-26"


def resolve_source(name):
    """Keep recorded names/hashes intact; map only their filesystem location."""
    name = str(name)
    for old, current in (("formal/", FORMAL), ("docs/architecture/", ARCHITECTURE)):
        if name.startswith(old):
            return current / name[len(old):]
    return REPO / name


def historical_name(path):
    path = Path(path).resolve()
    for current, old in ((FORMAL, "formal/"), (ARCHITECTURE, "docs/architecture/")):
        try:
            return old + str(path.relative_to(current))
        except ValueError:
            pass
    return str(path.relative_to(REPO))
