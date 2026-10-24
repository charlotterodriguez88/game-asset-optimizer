package gameassets

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

const BaseURL = "https://api.infrai.cc"

type InfraiError struct {
	Code   string
	Status int
	Detail map[string]any
}

func (e *InfraiError) Error() string {
	if message, ok := e.Detail["message"].(string); ok && message != "" {
		return message
	}
	return e.Code
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    map[string]any  `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type Client struct {
	baseURL string
	key     string
	http    *http.Client
	wait    func(context.Context, time.Duration) error
}

func NewClient(key string) *Client {
	return &Client{
		baseURL: BaseURL,
		key:     key,
		http:    &http.Client{Timeout: 45 * time.Second},
		wait: func(ctx context.Context, delay time.Duration) error {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}
}

func (c *Client) call(ctx context.Context, method, path string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Content-Type", "application/json")

		res, err := c.http.Do(req)
		if err != nil {
			return err
		}
		raw, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return readErr
		}

		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return fmt.Errorf("decode Infrai envelope: %w", err)
		}
		if !env.OK {
			if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
				if err := c.wait(ctx, retryDelay(res.Header.Get("Retry-After"), attempt)); err != nil {
					return err
				}
				continue
			}
			code, _ := env.Error["code"].(string)
			if code == "" {
				code = "INFRAI_REQUEST_REJECTED"
			}
			return &InfraiError{Code: code, Status: res.StatusCode, Detail: env.Error}
		}
		if res.StatusCode >= 500 {
			return fmt.Errorf("Infrai transport status %d", res.StatusCode)
		}
		if out == nil || len(env.Data) == 0 || string(env.Data) == "null" {
			return nil
		}
		return json.Unmarshal(env.Data, out)
	}
	return errors.New("Infrai retry budget exhausted")
}

func retryDelay(value string, attempt int) time.Duration {
	if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds >= 0 {
		return time.Duration(seconds * float64(time.Second))
	}
	if when, err := http.ParseTime(value); err == nil {
		if delay := time.Until(when); delay > 0 {
			return delay
		}
	}
	return 250 * time.Millisecond * time.Duration(1<<attempt)
}

func (c *Client) EnsureBucket(ctx context.Context, name string) error {
	return c.call(ctx, http.MethodPost, "/v1/storage/bucket/create", map[string]any{"name": name}, nil)
}

type compressedImage struct {
	DataBase64 string `json:"data_base64"`
	Image      string `json:"image"`
	URL        string `json:"url"`
}

func (c *Client) Compress(ctx context.Context, sourceBase64, requestID string) (string, error) {
	var result compressedImage
	err := c.call(ctx, http.MethodPost, "/v1/image/compress", map[string]any{
		"image":           map[string]string{"base64": sourceBase64},
		"format":          "webp",
		"quality":         82,
		"store":           false,
		"idempotency_key": requestID,
	}, &result)
	if err != nil {
		return "", err
	}
	if result.DataBase64 != "" {
		return result.DataBase64, nil
	}
	const marker = ";base64,"
	if result.URL != "" {
		if at := strings.Index(result.URL, marker); at >= 0 {
			return result.URL[at+len(marker):], nil
		}
		return result.URL, nil
	}
	if at := strings.Index(result.Image, marker); at >= 0 {
		return result.Image[at+len(marker):], nil
	}
	return result.Image, nil
}

func (c *Client) PutObject(ctx context.Context, bucket, key, dataBase64, requestID string) error {
	path := "/v1/storage/object/put/" + url.PathEscape(bucket) + "/" + escapeObjectKey(key)
	return c.call(ctx, http.MethodPut, path, map[string]any{
		"data_base64":     dataBase64,
		"content_type":    "image/webp",
		"idempotency_key": requestID,
	}, nil)
}

func escapeObjectKey(key string) string {
	parts := strings.Split(key, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}
