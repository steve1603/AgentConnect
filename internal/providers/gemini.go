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

const (
	geminiInteractionsURL = "https://generativelanguage.googleapis.com/v1beta/interactions"
	geminiAPIRevision     = "2026-05-20"
)

// NewGemini builds a provider backed by the Gemini Interactions API. It is
// called over plain HTTP so the request and response shapes stay explicit and
// pinned to the API revision above.
func NewGemini(model, apiKey string, timeout time.Duration, retries, maxTokens int) Provider {
	p := &base{
		key:             "gemini",
		label:           "Gemini",
		model:           model,
		apiKey:          apiKey,
		timeout:         timeout,
		retries:         retries,
		maxOutputTokens: maxTokens,
		endpoint:        geminiInteractionsURL,
		sleep:           sleepWithContext,
	}
	client := &http.Client{Timeout: timeout}
	p.generate = func(ctx context.Context, system, prompt string) (string, error) {
		return geminiGenerate(ctx, client, p, system, prompt)
	}
	return p
}

type geminiRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type geminiResponse struct {
	OutputText string `json:"output_text"`
	Steps      []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"steps"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func geminiGenerate(ctx context.Context, client *http.Client, p *base, system, prompt string) (string, error) {
	// The Interactions API takes a single input, so the system framing is
	// folded into it and every turn stays a self-contained request.
	input := strings.TrimSpace(prompt)
	if system = strings.TrimSpace(system); system != "" {
		input = system + "\n\n" + input
	}

	body, err := json.Marshal(geminiRequest{Model: p.model, Input: input})
	if err != nil {
		return "", fatal("could not encode request: %v", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fatal("could not build request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("x-goog-api-key", p.apiKey)
	request.Header.Set("Api-Revision", geminiAPIRevision)

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

	var parsed geminiResponse
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

	return geminiText(parsed), nil
}

// geminiText collects the text of the interaction's model_output steps.
func geminiText(parsed geminiResponse) string {
	if strings.TrimSpace(parsed.OutputText) != "" {
		return parsed.OutputText
	}
	var chunks []string
	for _, step := range parsed.Steps {
		if step.Type != "model_output" {
			continue
		}
		for _, block := range step.Content {
			if block.Type == "text" && block.Text != "" {
				chunks = append(chunks, block.Text)
			}
		}
	}
	return strings.Join(chunks, "\n")
}
