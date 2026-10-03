#!/usr/bin/env python3
"""在临时目录重建 SQL/Protobuf；不改写仓库来掩盖生成物漂移。"""
from pathlib import Path
import shutil
import subprocess
import tempfile

root = Path(__file__).resolve().parent.parent
with tempfile.TemporaryDirectory(prefix="harness-generated-") as work:
    temp = Path(work)
    shutil.copyfile(root / "sqlc.yaml", temp / "sqlc.yaml")
    for backend in ("postgres", "sqlite"):
        for folder in ("migrations", "queries"):
            shutil.copytree(root / f"adapters/{backend}/{folder}", temp / f"adapters/{backend}/{folder}")
    subprocess.run(["sqlc", "generate", "-f", str(temp / "sqlc.yaml")], cwd=temp, check=True)
    for backend in ("postgres", "sqlite"):
        generated = temp / f"adapters/{backend}/gen"
        original = root / f"adapters/{backend}/gen"
        names = {p.name for p in generated.glob("*.go")}
        if names != {p.name for p in original.glob("*.go")}:
            raise SystemExit(f"SQL generated file set drift: {backend}")
        for name in names:
            if (generated / name).read_bytes() != (original / name).read_bytes():
                raise SystemExit(f"SQL generated content drift: {backend}/{name}")
    proto = temp / "proto"
    proto.mkdir()
    package_map = "Mharness.proto=github.com/ruipengliu/lerna/api/proto/rpcv1"
    subprocess.run(["protoc", f"--proto_path={root / 'docs/architecture/protocol'}", f"--go_out={proto}", "--go_opt=paths=source_relative", "--go_opt="+package_map, f"--go-grpc_out={proto}", "--go-grpc_opt=paths=source_relative", "--go-grpc_opt="+package_map, str(root / "docs/architecture/protocol/harness.proto")], check=True)
    for name in ("harness.pb.go", "harness_grpc.pb.go"):
        if (proto / name).read_bytes() != (root / f"api/proto/rpcv1/{name}").read_bytes():
            raise SystemExit(f"Protobuf generated content drift: {name}")
print("SQL and Protobuf generated assets match their locked sources")
