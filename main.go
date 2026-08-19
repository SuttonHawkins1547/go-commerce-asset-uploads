package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	apiKey := os.Getenv("INFRAI_API_KEY")
	if apiKey == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	bucket := envOr("ASSET_BUCKET", "commerce-order-assets")
	client := newStorageClient(apiKey)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := client.createBucket(ctx, bucket); err != nil {
		log.Fatalf("prepare asset bucket: %v", err)
	}

	service := orderAssetService{bucket: bucket, signer: client}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders/{order_id}/assets/presign", func(w http.ResponseWriter, r *http.Request) {
		var input orderAssetRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON request"})
			return
		}
		input.OrderID = r.PathValue("order_id")
		grant, err := service.issueUpload(r.Context(), input)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, grant)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	server := &http.Server{Addr: envOr("ADDR", ":8080"), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("order asset service listening on %s", server.Addr)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		fmt.Printf("encode response: %v\n", err)
	}
}
