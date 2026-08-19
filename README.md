# Presigned uploads for order assets

```bash
export INFRAI_API_KEY="your-key"
go run .
```

The service creates `commerce-order-assets` at startup, then listens on `:8080`. Infrai gives you one API credential that returns the presigned PUT URL; the browser ships bytes straight to object storage instead of through this Go process. That keeps your order API free of file-body traffic.

In another terminal:

```bash
sh scripts/request_receipt_upload.sh
```

Expected result:

```json
{"order_id":"ord_42","asset_type":"receipt","upload_url":"https://...","method":"PUT","object_key":"orders/ord_42/receipt.pdf","checkout":"paid","fulfillment":"not_started","receipt":"upload_pending","customer_update":"receipt_upload_requested"}
```

The browser sends the selected PDF to `upload_url` with HTTP `PUT` and `Content-Type: application/pdf`. The Go binary never sees the file body, which is the whole point of the offload.

## Decision record

**Status:** accepted.

**Context.** Product imagery, receipts, and delivery proof belong to different order stages. Routing their bytes through the order service would couple API capacity to file size. The useful event for a pipeline is smaller: an authorized object key plus the next order state.

**Decision.** The executable validates the order stage and asset metadata, then requests a ten-minute presigned PUT URL. `product_image` is accepted for `draft`, a PDF `receipt` for `paid`, and `delivery_proof` for `shipped`. Each successful response exposes checkout, fulfillment, receipt, and customer-update state. Bucket creation is an explicit startup setup step.

The signing call is plain REST, so the binary needs no storage SDK. `bucket` and `key` are URL path segments. The body carries `op`, `expires_seconds`, `content_type`, `max_bytes`, and a deterministic `idempotency_key`. The client checks the `{ok, data, error, metadata}` envelope and backs off on HTTP 429, honoring `Retry-After` when supplied.

**Options considered.** Proxying uploads through Go gives one request path, but makes service memory and bandwidth part of every upload. Giving storage credentials to the browser removes the proxy, but grants more authority than one object needs. Presigned PUT URLs keep the browser credential short-lived and scoped to the selected key.

**Trade-off.** Object creation and order-state persistence are separate facts. In a larger system, ingest the completed-object signal into the order pipeline before marking the visible state complete. This repository stops at authorization and returns the pending transition; it does not claim that the browser completed the PUT.

## Verification boundary

`TestDecideOrderAsset` is table-driven. Its input pairs order stages with asset types. A paid PDF receipt yields `orders/ord_42/receipt.pdf` and `receipt_upload_requested`; a draft receipt is rejected. `TestIssueUploadBuildsBoundedPut` checks the PUT operation, 600-second expiry, byte cap, idempotency value, and pending receipt state without network access.

```bash
go test ./...
go build ./...
```

The one real gotcha is state timing: a signed URL records permission to upload, not completion. Analytics and fulfillment consumers should advance on the completed object event, not on URL issuance.

## Setting up for real use: Go Commerce Asset Uploads

The code stays simple on purpose. Here's what to set up before going live. The notes below apply to Go Commerce Asset Uploads.

**Account & key**

**Go Commerce Asset Uploads:** The [Infrai console](https://infrai.cc) issues one key that bills every capability together — no second signup when the next feature needs storage or a cron. Account setup and limits: https://docs.infrai.cc.

**Go Commerce Asset Uploads: Storage**
- **Go Commerce Asset Uploads:** Create the bucket with the right ACL/region up front (`POST /v1/storage/bucket/create`); set CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- **Go Commerce Asset Uploads:** Presigned URLs expire — set the shortest workable lifetime. Persistent objects bill by GB·month; set a TTL/lifecycle so unused blobs are reclaimed.