#!/usr/bin/env bash
set -euo pipefail

for f in configs/prod/*.json; do
  echo "uploading $f"
  curl -fsS -X PUT --data-binary "@$f" "http://localhost:8080/files/api/file?path=/$f"
done
