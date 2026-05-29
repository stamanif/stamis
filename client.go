package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ChatCompletionRequest defines the payload for an OpenAI-compatible API endpoint.
type ChatCompletionRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

// ChatCompletionChunk parses the dense SSE chunk JSON streamed from the endpoint.
type ChatCompletionChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

// Metrics tracks the performance of a single API request.
type Metrics struct {
	TTFT         time.Duration
	TotalLatency time.Duration
}

// StreamPrinter dynamically suppresses <TOOLCALL> blocks from the terminal UI while streaming.
type StreamPrinter struct {
	out        io.Writer
	buffer     string
	inToolCall bool
}

func (sp *StreamPrinter) Print(text string) {
	if sp.out == nil {
		return
	}

	sp.buffer += text

	for {
		if !sp.inToolCall {
			if idx := strings.Index(sp.buffer, "<TOOLCALL>"); idx != -1 {
				sp.inToolCall = true
				if idx > 0 {
					fmt.Fprint(sp.out, sp.buffer[:idx])
				}
				sp.buffer = sp.buffer[idx+len("<TOOLCALL>"):]
				continue
			}

			lastOpen := strings.LastIndex(sp.buffer, "<")
			if lastOpen != -1 {
				possiblePrefix := sp.buffer[lastOpen:]
				if strings.HasPrefix("<TOOLCALL>", possiblePrefix) {
					if lastOpen > 0 {
						fmt.Fprint(sp.out, sp.buffer[:lastOpen])
						sp.buffer = sp.buffer[lastOpen:]
					}
					return
				}
			}

			fmt.Fprint(sp.out, sp.buffer)
			sp.buffer = ""
			return
		} else {
			if idx := strings.Index(sp.buffer, "</TOOLCALL>"); idx != -1 {
				sp.inToolCall = false
				sp.buffer = sp.buffer[idx+len("</TOOLCALL>"):]
				continue
			}

			lastOpen := strings.LastIndex(sp.buffer, "<")
			if lastOpen != -1 {
				possiblePrefix := sp.buffer[lastOpen:]
				if strings.HasPrefix("</TOOLCALL>", possiblePrefix) {
					sp.buffer = possiblePrefix
					return
				}
			}

			sp.buffer = ""
			return
		}
	}
}

func (sp *StreamPrinter) Flush() {
	if sp.out != nil && !sp.inToolCall && sp.buffer != "" {
		fmt.Fprint(sp.out, sp.buffer)
		sp.buffer = ""
	}
}

// SendChatRequest acts as the Unified Local/Cloud Endpoint Router with real-time SSE streaming.
func SendChatRequest(config *Config, history []Message, out io.Writer, metrics *Metrics) (*Message, error) {
	startTime := time.Now()

	reqPayload := ChatCompletionRequest{
		Model:    config.Model,
		Messages: history,
		Stream:   true,
	}

	jsonData, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request payload: %w", err)
	}

	url := config.Endpoint
	if !strings.HasSuffix(url, "/chat/completions") {
		if !strings.HasSuffix(url, "/") {
			url += "/"
		}
		url += "chat/completions"
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	
	// Pillar 1: Strip auth headers for local models
	if config.Provider != "ollama" && config.Provider != "local" && config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+config.APIKey)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var fullContent strings.Builder
	reader := bufio.NewReader(resp.Body)
	dataPrefix := []byte("data: ")

	sp := &StreamPrinter{out: out}
	firstToken := false

	// Pillar 1: Zero buffering streaming using byte extraction
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("error reading stream: %w", err)
		}

		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue // Skip empty Server-Sent Event lines
		}

		if bytes.HasPrefix(line, dataPrefix) {
			dataPayload := bytes.TrimPrefix(line, dataPrefix)
			
			if string(dataPayload) == "[DONE]" {
				break
			}

			var chunk ChatCompletionChunk
			if err := json.Unmarshal(dataPayload, &chunk); err != nil {
				// Silently skip malformed chunks or keep-alive pings to keep the stream resilient
				continue
			}

			if len(chunk.Choices) > 0 {
				delta := chunk.Choices[0].Delta
				
				// Stream text content natively to terminal, dynamically masking <TOOLCALL> blocks
				if delta.Content != "" {
					if !firstToken && metrics != nil {
						metrics.TTFT = time.Since(startTime)
						firstToken = true
					}
					fullContent.WriteString(delta.Content)
					sp.Print(delta.Content)
				}
			}
		}
	}

	sp.Flush()

	if metrics != nil {
		metrics.TotalLatency = time.Since(startTime)
	}

	EmitTelemetry("latency_metrics", metrics)

	finalMessage := &Message{
		Role:    "assistant",
		Content: fullContent.String(),
	}

	return finalMessage, nil
}
