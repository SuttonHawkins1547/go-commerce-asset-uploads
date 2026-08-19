#!/bin/sh
set -eu

curl --fail-with-body \
  --request POST \
  --header 'Content-Type: application/json' \
  --data '{"asset_type":"receipt","order_stage":"paid","content_type":"application/pdf","size_bytes":4096}' \
  http://localhost:8080/orders/ord_42/assets/presign
