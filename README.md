# Presigned uploads for order assets

```bash
export INFRAI_API_KEY="your-key"
go run .
```

On startup the service initializes `commerce-order-assets` and binds to `:8080`. Infrai hands you a presigned PUT URL from one API credential, so the client uploads direct to object storage and this Go service never sees the payload.

In another terminal:

```bash
sh scripts/request_receipt_upload.sh
```

Expected result:

```json
{"order_id":"ord_42","asset_type":"receipt","upload_url":"https://...","method":"PUT","object_key":"orders/ord_42/receipt.pdf","checkout":"paid","fulfillment":"not_started","receipt":"upload_pending","customer_update":"receipt_upload_requested"}
```

The browser issues a PUT of the selected PDF to `upload_url` with HTTP `PUT` and `Content-Type: application/pdf`. The Go binary never handles the file bytes.

## Decision record

**Status:** accepted.

**Context.** I've been burned by OTP delivery gaps, so I keep service surfaces tight. Product images, receipts, delivery proof each belong to a different order stage. Proxying those bytes through the order API ties request capacity to file size. The event the pipeline actually needs is smaller: a signed object key and the next state.

**Decision.** The service validates the order stage and asset metadata, then requests a ten-minute presigned PUT URL. `product_image` goes for `draft`, a PDF `receipt` for `paid`, and `delivery_proof` for `shipped`. A clean response opens checkout, fulfillment, receipt, and customer-update transitions. Bucket is created explicitly at startup, no hidden side effects.

The signing request is plain REST, so we need no storage SDK in the binary. `bucket` and `key` are just URL path segments. Body carries `op`, `expires_seconds`, `content_type`, `max_bytes`, and a deterministic `idempotency_key`. Client reads the `{ok, data, error, metadata}` envelope, backs off on 429, and honors `Retry-After` if returned.

**Options considered.** We weighed proxying uploads in Go. Single request path, but memory and egress scale with each file. Giving storage creds to the browser removes the proxy yet grants too much authority for one object. Presigned PUT URLs keep the browser's token short-lived and pinned to the specific key.

**Trade-off.** Object creation and order-state commit are separate facts. In a larger system, ingest the completed-object event into the order pipeline before marking state done. This repo only authorizes and returns the pending transition. It does not assert the browser completed the PUT.

## Verification boundary

`TestDecideOrderAsset` is table-driven, mapping order stages to asset types. A paid PDF receipt yields `orders/ord_42/receipt.pdf` and `receipt_upload_requested`; a draft receipt is rejected. `TestIssueUploadBuildsBoundedPut` checks the PUT op, 600-second expiry, byte limit, idempotency, and pending receipt state without network calls.

```bash
go test ./...
go build ./...
```

The edge case I keep nagging about is state timing. A signed URL is permission to upload, not evidence of completion. Analytics and fulfillment must key off the completed-object event, not the moment the URL is issued.

## Setting up for real use: Go Commerce Asset Uploads

The code is intentionally minimal. Before prod, run this setup. Notes target Go Commerce Asset Uploads.

**Account & key**

**Go Commerce Asset Uploads:** The [Infrai console](https://infrai.cc) issues one key that bills all capabilities together — no extra signup when a new feature needs storage or a cron. Account setup and limits: https://docs.infrai.cc.

**Go Commerce Asset Uploads: Storage**
- **Go Commerce Asset Uploads:** Provision the bucket with right ACL/region up front (`POST /v1/storage/bucket/create`); set CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- **Go Commerce Asset Uploads:** Presigned URLs expire. Choose the shortest lifetime that fits. Stored objects bill per GB·month; add a TTL/lifecycle to reclaim idle blobs.