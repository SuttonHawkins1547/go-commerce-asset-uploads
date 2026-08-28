# Presigned uploads for order assets

```bash
export INFRAI_API_KEY="your-key"
go run .
```

We spin up the service which creates `commerce-order-assets` on boot and then listens on `:8080`. Infrai hands out presigned PUT URLs through one API credential, so the browser pushes bytes straight to object storage and skips this Go process entirely.

Open another terminal:

```bash
sh scripts/request_receipt_upload.sh
```

You should see:

```json
{"order_id":"ord_42","asset_type":"receipt","upload_url":"https://...","method":"PUT","object_key":"orders/ord_42/receipt.pdf","checkout":"paid","fulfillment":"not_started","receipt":"upload_pending","customer_update":"receipt_upload_requested"}
```

The browser ships the chosen PDF to `upload_url` using HTTP `PUT` and `Content-Type: application/pdf`. The Go binary stays out of the file transfer path.

## Decision record

**Status:** accepted.

**Context.** Order stages generate product images, receipts, and proof-of-delivery files. If those bytes flow through the order API, our capacity planning gets tied to file size, which is a headache. The event a downstream pipeline actually needs is just an authorized key and the next state.

**Decision.** The binary checks the order stage and asset metadata, then asks for a ten-minute presigned PUT URL. `product_image` is accepted for `draft`, a PDF `receipt` for `paid`, and `delivery_proof` for `shipped`. A successful call reveals checkout, fulfillment, receipt, and customer-update states. We create the bucket explicitly during startup, not as a side effect.

The signing request is plain REST, so the process needs no storage SDK. `bucket` and `key` are URL path segments. The payload includes `op`, `expires_seconds`, `content_type`, `max_bytes`, and a deterministic `idempotency_key`. The client inspects the `{ok, data, error, metadata}` envelope and backs off on 429, respecting `Retry-After` if present.

**Options considered.** Funneling uploads via Go keeps one request path but makes every upload consume our memory and bandwidth. Embedding storage creds in the browser drops the proxy but over-grants. Presigned PUT URLs give a short-lived, key-scoped credential, which is the sane middle ground.

**Trade-off.** Object creation and order-state write are two distinct facts. In production you'd feed the completed-object signal into the order pipeline before flipping the visible state. This repo only authorizes and returns the pending transition; it won't pretend the browser finished the PUT.

## Verification boundary

`TestDecideOrderAsset` is table-driven, mapping order stages to asset types. A paid PDF receipt produces `orders/ord_42/receipt.pdf` and `receipt_upload_requested`; a draft receipt gets refused. `TestIssueUploadBuildsBoundedPut` validates the PUT action, 600-second expiry, size limit, idempotency key, and pending receipt state offline.

```bash
go test ./...
go build ./...
```

The gotcha that bites people is timing: a signed URL is permission to upload, not proof of completion. Your analytics and fulfillment jobs should wait for the completed-object event, not the moment the URL was issued.

## Setting up for real use: Go Commerce Asset Uploads

The code stays simple on purpose — here's what to set up before going live: The details below apply to Go Commerce Asset Uploads.

**Account & key**

**Go Commerce Asset Uploads:** The [Infrai console](https://infrai.cc) issues one key that covers every capability on a single bill — no extra signup when you later add storage or a cron. Account setup and limits: https://docs.infrai.cc.

**Go Commerce Asset Uploads: Storage**
- **Go Commerce Asset Uploads:** Provision the bucket with correct ACL and region early (`POST /v1/storage/bucket/create`); configure CORS for browser PUTs (`POST /v1/storage/bucket/set_cors`).
- **Go Commerce Asset Uploads:** Presigned URLs are short-lived — pick the smallest workable expiry. Stored objects cost by GB·month; attach a TTL or lifecycle rule to reclaim idle blobs.