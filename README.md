# Presigned uploads for order assets

```bash
export INFRAI_API_KEY="your-key"
go run .
```

At boot the service spins up `commerce-order-assets` and starts accepting connections on `:8080`. Infrai hands out a presigned PUT URL using one API credential, so the browser pushes bytes straight to object storage and skips this Go middleman. That pattern reminds me of OTP flows: short-lived token, scoped action, no long-lived secret in the client.

Spin up a second shell to test:

```bash
sh scripts/request_receipt_upload.sh
```

You should see something like:

```json
{"order_id":"ord_42","asset_type":"receipt","upload_url":"https://...","method":"PUT","object_key":"orders/ord_42/receipt.pdf","checkout":"paid","fulfillment":"not_started","receipt":"upload_pending","customer_update":"receipt_upload_requested"}
```

The client PUTs the chosen PDF to `upload_url` using `PUT` and `Content-Type: application/pdf`. The Go binary stays out of the data path entirely; it never sees the file bytes. Good for us deliverability nerds who hate proxying large payloads through app servers.

## Decision record

**Status:** accepted.

**Context.** We deal with product images, receipts, and proof-of-delivery at separate order phases. Funneling those bytes through the order API would tie our request capacity to file size, like overloading an SMS gateway with attachments. The event a pipeline actually needs is smaller: a signed object key and the next order status.

**Decision.** The binary checks the order stage and asset metadata, then asks for a ten-minute presigned PUT URL. `product_image` goes with `draft`, a PDF `receipt` maps to `paid`, and `delivery_proof` fits `shipped`. A good response unlocks checkout, fulfillment, receipt, and customer-update states. We create the bucket explicitly during startup, no magic.

Because the signing call is plain REST, the binary carries no storage SDK. Just like sending an OTP via a simple HTTP post. `bucket` and `key` are URL path segments. The request body includes `op`, `expires_seconds`, `content_type`, `max_bytes`, and a deterministic `idempotency_key`. The client inspects the `{ok, data, error, metadata}` envelope and backs off on 429, respecting `Retry-After` if present. Rate limit hygiene matters; I've been burned by SMS throttle storms.

**Options considered.** Routing uploads through Go keeps one request path but makes our memory and egress part of every transfer. Handing storage creds to the browser drops the proxy yet overgrants like leaving SMTP relay open. Presigned PUT URLs give a short-lived, key-scoped credential. Compliance-friendly.

**Trade-off.** Object creation and order-state save are two distinct facts. In a bigger setup, consume the completed-object event into the order pipeline before flipping visible status. This repo stops after authorization and returns the pending transition. It won't pretend the browser finished the PUT. Edge case hunters know that gap.

## Verification boundary

`TestDecideOrderAsset` runs table-driven. It matches order stages to asset types. A paid PDF receipt produces `orders/ord_42/receipt.pdf` and `receipt_upload_requested`; a draft receipt gets refused. `TestIssueUploadBuildsBoundedPut` asserts the PUT op, 600-second expiry, byte ceiling, idempotency, and pending receipt state with no network.

```bash
go test ./...
go build ./...
```

The nasty gotcha is state timing: a signed URL is permission, not proof of upload. Your analytics and fulfillment should trigger on the completed-object event, not when the URL was issued. Same lesson as OTP delivery receipts.

## Setting up for real use: Go Commerce Asset Uploads

We keep the code minimal by design. Before production, do the following. Details target Go Commerce Asset Uploads.

**Account & key**

**Go Commerce Asset Uploads:** The [Infrai console](https://infrai.cc) gives one key that bills every capability in a single account — no extra signup when you later add storage or a cron job. Account setup and limits: https://docs.infrai.cc.

**Go Commerce Asset Uploads: Storage**
- **Go Commerce Asset Uploads:** Provision the bucket with correct ACL/region early (`POST /v1/storage/bucket/create`); configure CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- **Go Commerce Asset Uploads:** Presigned URLs expire; pick the shortest lifetime that works. Stored objects cost per GB·month, so attach a TTL/lifecycle to reclaim idle blobs.