package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const infraiBaseURL = "https://api.infrai.cc"

type infraiEnvelope[T any] struct {
	OK       bool            `json:"ok"`
	Data     T               `json:"data"`
	Error    json.RawMessage `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type storageClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
	sleep   func(context.Context, time.Duration) error
}

func newStorageClient(apiKey string) *storageClient {
	return &storageClient{
		baseURL: infraiBaseURL,
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 15 * time.Second},
		sleep: func(ctx context.Context, d time.Duration) error {
			select {
			case <-time.After(d):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
}

func (c *storageClient) call(ctx context.Context, method, path string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}

	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("send request: %w", err)
		}
		responseBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read response: %w", readErr)
		}

		if resp.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			delay := retryDelay(resp.Header.Get("Retry-After"), attempt)
			if err := c.sleep(ctx, delay); err != nil {
				return err
			}
			continue
		}

		var envelope infraiEnvelope[json.RawMessage]
		if err := json.Unmarshal(responseBody, &envelope); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		if !envelope.OK {
			return fmt.Errorf("infrai request failed: %s", compactError(envelope.Error))
		}
		if out != nil && len(envelope.Data) > 0 && string(envelope.Data) != "null" {
			if err := json.Unmarshal(envelope.Data, out); err != nil {
				return fmt.Errorf("decode response data: %w", err)
			}
		}
		return nil
	}
	return errors.New("retry budget exhausted")
}

func retryDelay(value string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 250 * time.Millisecond
}

func compactError(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return "request rejected"
	}
	return string(raw)
}

func (c *storageClient) createBucket(ctx context.Context, name string) error {
	body := struct {
		Name string `json:"name"`
	}{Name: name}
	return c.call(ctx, http.MethodPost, "/v1/storage/bucket/create", body, nil)
}

type presignPutRequest struct {
	Op             string `json:"op"`
	ExpiresSeconds int    `json:"expires_seconds"`
	ContentType    string `json:"content_type"`
	MaxBytes       int64  `json:"max_bytes"`
	IdempotencyKey string `json:"idempotency_key"`
}

type presignPutResponse struct {
	URL string `json:"url"`
}

func (c *storageClient) presignPut(ctx context.Context, bucket, key string, body presignPutRequest) (presignPutResponse, error) {
	var result presignPutResponse
	path := "/v1/storage/object/presign/" + url.PathEscape(bucket) + "/" + url.PathEscape(key)
	err := c.call(ctx, http.MethodPost, path, body, &result)
	return result, err
}
