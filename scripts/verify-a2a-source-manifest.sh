#!/usr/bin/env bash
set -euo pipefail

manifest="compatibility/a2a-1.0.0-source-manifest.json"
test -f "$manifest"

python3 - "$manifest" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as handle:
    manifest = json.load(handle)

assert manifest["protocol"]["version"] == "1.0.0"
assert manifest["protocol"]["tag"] == "v1.0.0"
assert manifest["officialSdk"]["package"] == "a2a-sdk"
assert manifest["officialSdk"]["version"] == "1.1.4"
assert manifest["verification"]["status"] == "pending-ci"
assert manifest["verification"]["gatewayEnabledByDefault"] is False
PY

spec_url="https://raw.githubusercontent.com/a2aproject/A2A/${A2A_SPEC_TAG:-v1.0.0}/specification/a2a.proto"
docs_url="https://raw.githubusercontent.com/a2aproject/A2A/${A2A_SPEC_TAG:-v1.0.0}/docs/specification.md"
sdk_url="https://raw.githubusercontent.com/a2aproject/a2a-python/${A2A_SDK_TAG:-v1.1.4}/pyproject.toml"

actual_spec="$(curl --fail --silent --show-error --location "$spec_url" | sha256sum | awk '{print $1}')"
actual_docs="$(curl --fail --silent --show-error --location "$docs_url" | sha256sum | awk '{print $1}')"
actual_sdk="$(curl --fail --silent --show-error --location "$sdk_url" | sha256sum | awk '{print $1}')"

expected="$(python3 - "$manifest" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as handle:
    manifest = json.load(handle)
print(
    manifest["protocol"]["specification"]["sha256"],
    manifest["protocol"]["documentation"]["sha256"],
    manifest["officialSdk"]["pyprojectSha256"],
)
PY
)"
read -r expected_spec expected_docs expected_sdk <<<"$expected"
test "$actual_spec" = "$expected_spec"
test "$actual_docs" = "$expected_docs"
test "$actual_sdk" = "$expected_sdk"
