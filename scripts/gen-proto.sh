#!/bin/sh
# Regenerate internal/protocol/pb from proto/. Generated code is committed;
# run this after editing wire.proto and keep hand-written convert.go aligned.
set -eu
cd "$(dirname "$0")/.."
protoc --proto_path=proto \
  --go_out=. --go_opt=module=cleanc2 \
  proto/cleanc2/v1/wire.proto
gofmt -l internal/protocol/pb
