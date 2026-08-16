package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// openAIResponsesURL is the default endpoint; the provider stores it so
// tests can point at a local server.
const openAIResponsesURL = "https://api.openai.com/v1/responses"

// NewOpenAI builds a provider backed by the OpenAI Responses API.
func NewOpenAI(model, apiKey string, timeout time.Duration, retries, maxTokens int) Provider {
	p := &base{
		key:             "openai",
		label:           "ChatGPT",
		model:           model,
		apiKey:          apiKey,
		timeout:         timeout,
		retries:         retries,
		maxOutputTokens: maxTokens,
		endpoint:        openAIResponsesURL,
		sleep:           sleepWithContext,
	}
	client := &http.Client{Timeout: timeout}
	p.generate = func(ctx context.Context, system, prompt string) (string, error) {
		return openAIGenerate(ctx, client, p, system, prompt)
	}
	return p
}

type openAIRequest struct {
	Model           string `json:"model"`
	Instructions    string `json:"instructions,omitempty"`
	Input           string `json:"input"`
	MaxOutputTokens int    `json:"max_output_tokens,omitempty"`
}

type openAIResponse struct {
	OutputText string `json:"output_text"`
	Output     []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func openAIGenerate(ctx context.Context, client *http.Client, p *base, system, prompt string) (string, error) {
	body, err := json.Marshal(openAIRequest{
		Model:           p.model,
		Instructions:    system,
		Input:           prompt,
		MaxOutputTokens: p.maxOutputTokens,
	})
	if err != nil {
		return "", fatal("could not encode request: %v", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fatal("could not build request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+p.apiKey)

	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", retryable("request timed out after %s", p.timeout)
		}
		return "", retryable("connection error: %v", err)
	}
	defer response.Body.Close()

	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return "", retryable("could not read response: %v", err)
	}

	var parsed openAIResponse
	_ = json.Unmarshal(payload, &parsed)

	if response.StatusCode >= 400 {
		detail := strings.TrimSpace(truncate(string(payload), 300))
		if parsed.Error != nil && parsed.Error.Message != "" {
			detail = parsed.Error.Message
		}
		message := "HTTP " + response.Status + ": " + detail
		if retryableStatus(response.StatusCode) {
			return "", retryable("%s", message)
		}
		return "", fatal("%s", message)
	}

	return openAIText(parsed), nil
}

// openAIText prefers the flattened output_text, falling back to walking the
// output items when the response also carries non-message items.
func openAIText(parsed openAIResponse) string {
	if strings.TrimSpace(parsed.OutputText) != "" {
		return parsed.OutputText
	}
	var chunks []string
	for _, item := range parsed.Output {
		if item.Type != "message" {
			continue
		}
		for _, block := range item.Content {
			if block.Type == "output_text" && block.Text != "" {
				chunks = append(chunks, block.Text)
			}
		}
	}
	return strings.Join(chunks, "\n")
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit]
}
