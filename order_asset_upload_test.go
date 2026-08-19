package main

import (
	"context"
	"testing"
)

func TestDecideOrderAsset(t *testing.T) {
	tests := []struct {
		name      string
		input     orderAssetRequest
		wantKey   string
		wantState string
		wantErr   bool
	}{
		{"draft product image", orderAssetRequest{"ord_42", "product_image", "draft", "image/png", 2048}, "orders/ord_42/product-image.png", "catalog_asset_requested", false},
		{"paid receipt", orderAssetRequest{"ord_42", "receipt", "paid", "application/pdf", 4096}, "orders/ord_42/receipt.pdf", "receipt_upload_requested", false},
		{"shipped delivery proof", orderAssetRequest{"ord_42", "delivery_proof", "shipped", "image/jpeg", 1024}, "orders/ord_42/delivery-proof.jpg", "delivery_proof_requested", false},
		{"receipt before payment", orderAssetRequest{"ord_42", "receipt", "draft", "application/pdf", 4096}, "", "", true},
		{"oversized asset", orderAssetRequest{"ord_42", "product_image", "draft", "image/png", maxAssetBytes + 1}, "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decideOrderAsset(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if got.ObjectKey != tt.wantKey || got.CustomerUpdate != tt.wantState {
				t.Fatalf("decision = %#v, want key %q and update %q", got, tt.wantKey, tt.wantState)
			}
		})
	}
}

type recordingSigner struct {
	request presignPutRequest
}

func (s *recordingSigner) presignPut(_ context.Context, _, _ string, request presignPutRequest) (presignPutResponse, error) {
	s.request = request
	return presignPutResponse{URL: "https://upload.example/signed"}, nil
}

func TestIssueUploadBuildsBoundedPut(t *testing.T) {
	signer := &recordingSigner{}
	service := orderAssetService{bucket: "test-assets", signer: signer}
	input := orderAssetRequest{"ord_7", "receipt", "paid", "application/pdf", 1234}
	grant, err := service.issueUpload(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if signer.request.Op != "put" || signer.request.ExpiresSeconds != 600 || signer.request.MaxBytes != 1234 {
		t.Fatalf("presign request = %#v", signer.request)
	}
	if signer.request.IdempotencyKey == "" || grant.Method != "PUT" || grant.Receipt != "upload_pending" {
		t.Fatalf("grant = %#v; request = %#v", grant, signer.request)
	}
}
