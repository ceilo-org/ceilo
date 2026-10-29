package net

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Leoj030/go-luau/luau"
)

type metadata struct {
	Method  string            `luau:"method"`
	Body    string            `luau:"body"`
	Headers map[string]string `luau:"headers"`
}

type clientResponse struct {
	Body    string            `luau:"body"`
	Headers map[string]string `luau:"headers"`
	Status  int               `luau:"status"`
	OK      bool              `luau:"ok"`
}

type clientModule struct {
	ctx context.Context
}

func ClientModule(ctx context.Context) map[string]luau.Func {
	if ctx == nil {
		ctx = context.Background()
	}
	client := &clientModule{ctx: ctx}
	return map[string]luau.Func{"request": client.request}
}

func (m *clientModule) request(c *luau.State) (int, error) {
	urlArg, metadataArg := 1, 2
	if c.TypeName(1) == "table" {
		urlArg, metadataArg = 2, 3
	}
	if c.TypeName(urlArg) != "string" {
		return 0, errors.New("net.client.request: argument 1 must be a URL string")
	}
	urlValue, err := c.Get(urlArg)
	if err != nil {
		return 0, err
	}

	requestMetadata := metadata{Method: http.MethodGet}
	switch c.TypeName(metadataArg) {
	case "nil", "no value":
	case "table":
		if err := c.Decode(metadataArg, &requestMetadata); err != nil {
			return 0, fmt.Errorf("net.client.request: metadata: %w", err)
		}
		if requestMetadata.Method == "" {
			requestMetadata.Method = http.MethodGet
		}
	default:
		return 0, errors.New("net.client.request: argument 2 must be a metadata table")
	}

	req, err := http.NewRequestWithContext(m.ctx, requestMetadata.Method, urlValue.(string), strings.NewReader(requestMetadata.Body))
	if err != nil {
		return 0, fmt.Errorf("net.client.request: create request: %w", err)
	}
	for name, value := range requestMetadata.Headers {
		req.Header.Set(name, value)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("net.client.request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("net.client.request: read response body: %w", err)
	}
	headers := make(map[string]string, len(resp.Header))
	for name, values := range resp.Header {
		headers[name] = strings.Join(values, ", ")
	}
	result := clientResponse{
		Body:    string(body),
		Headers: headers,
		Status:  resp.StatusCode,
		OK:      resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices,
	}
	encoded, err := luau.Encode(result)
	if err != nil {
		return 0, fmt.Errorf("net.client.request: encode response: %w", err)
	}
	if err := c.PushEncoded(encoded); err != nil {
		return 0, err
	}
	return 1, nil
}
