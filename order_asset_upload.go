package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

const (
	maxAssetBytes = 8 << 20
	uploadTTL     = 10 * 60
)

var safeID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type orderAssetRequest struct {
	OrderID     string `json:"order_id"`
	AssetType   string `json:"asset_type"`
	OrderStage  string `json:"order_stage"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

type orderDecision struct {
	ObjectKey      string `json:"object_key"`
	Checkout       string `json:"checkout"`
	Fulfillment    string `json:"fulfillment"`
	Receipt        string `json:"receipt"`
	CustomerUpdate string `json:"customer_update"`
}

type uploadGrant struct {
	OrderID        string `json:"order_id"`
	AssetType      string `json:"asset_type"`
	UploadURL      string `json:"upload_url"`
	Method         string `json:"method"`
	ObjectKey      string `json:"object_key"`
	Checkout       string `json:"checkout"`
	Fulfillment    string `json:"fulfillment"`
	Receipt        string `json:"receipt"`
	CustomerUpdate string `json:"customer_update"`
}

func decideOrderAsset(input orderAssetRequest) (orderDecision, error) {
	if !safeID.MatchString(input.OrderID) {
		return orderDecision{}, fmt.Errorf("order_id must contain 1-64 letters, digits, underscores, or hyphens")
	}
	if input.SizeBytes <= 0 || input.SizeBytes > maxAssetBytes {
		return orderDecision{}, fmt.Errorf("size_bytes must be between 1 and %d", maxAssetBytes)
	}
	if input.ContentType != "image/jpeg" && input.ContentType != "image/png" && input.ContentType != "application/pdf" {
		return orderDecision{}, fmt.Errorf("content_type is not allowed")
	}

	extension := extensionFor(input.ContentType)
	decision := orderDecision{
		Checkout:       input.OrderStage,
		Fulfillment:    "not_started",
		Receipt:        "not_requested",
		CustomerUpdate: "none",
	}

	switch {
	case input.AssetType == "product_image" && input.OrderStage == "draft":
		decision.ObjectKey = fmt.Sprintf("orders/%s/product-image.%s", input.OrderID, extension)
		decision.Checkout = "asset_pending"
		decision.CustomerUpdate = "catalog_asset_requested"
	case input.AssetType == "receipt" && input.OrderStage == "paid" && input.ContentType == "application/pdf":
		decision.ObjectKey = fmt.Sprintf("orders/%s/receipt.pdf", input.OrderID)
		decision.Checkout = "paid"
		decision.Receipt = "upload_pending"
		decision.CustomerUpdate = "receipt_upload_requested"
	case input.AssetType == "delivery_proof" && input.OrderStage == "shipped":
		decision.ObjectKey = fmt.Sprintf("orders/%s/delivery-proof.%s", input.OrderID, extension)
		decision.Checkout = "paid"
		decision.Fulfillment = "proof_pending"
		decision.CustomerUpdate = "delivery_proof_requested"
	default:
		return orderDecision{}, fmt.Errorf("asset_type is not valid for order_stage")
	}
	return decision, nil
}

func extensionFor(contentType string) string {
	return map[string]string{"image/jpeg": "jpg", "image/png": "png", "application/pdf": "pdf"}[contentType]
}

type uploadSigner interface {
	presignPut(context.Context, string, string, presignPutRequest) (presignPutResponse, error)
}

type orderAssetService struct {
	bucket string
	signer uploadSigner
}

func (s orderAssetService) issueUpload(ctx context.Context, input orderAssetRequest) (uploadGrant, error) {
	decision, err := decideOrderAsset(input)
	if err != nil {
		return uploadGrant{}, err
	}
	idempotencyKey := uploadID(input, decision.ObjectKey)
	signed, err := s.signer.presignPut(ctx, s.bucket, decision.ObjectKey, presignPutRequest{
		Op:             "put",
		ExpiresSeconds: uploadTTL,
		ContentType:    input.ContentType,
		MaxBytes:       input.SizeBytes,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return uploadGrant{}, err
	}
	return uploadGrant{
		OrderID: input.OrderID, AssetType: input.AssetType, UploadURL: signed.URL,
		Method: "PUT", ObjectKey: decision.ObjectKey, Checkout: decision.Checkout,
		Fulfillment: decision.Fulfillment, Receipt: decision.Receipt,
		CustomerUpdate: decision.CustomerUpdate,
	}, nil
}

func uploadID(input orderAssetRequest, key string) string {
	raw := strings.Join([]string{input.OrderID, input.AssetType, input.OrderStage, input.ContentType, fmt.Sprint(input.SizeBytes), key}, "|")
	sum := sha256.Sum256([]byte(raw))
	return "order-asset-" + hex.EncodeToString(sum[:12])
}
