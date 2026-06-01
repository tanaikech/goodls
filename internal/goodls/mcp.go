package goodls

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// MCPRequest defines the JSON-RPC request from an MCP client
type MCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// MCPResponse defines the JSON-RPC response to an MCP client
type MCPResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Result  any    `json:"result,omitempty"`
	Error   any    `json:"error,omitempty"`
}

// stdoutMu protects writing to the JSON-RPC output stream to prevent interleaving
var stdoutMu sync.Mutex

// RunMCP starts the stdio JSON-RPC server for Model Context Protocol
func RunMCP() error {
	// DANGER: If goodls writes anything to os.Stdout during MCP mode, it will corrupt the JSON-RPC stream.
	// We MUST hijack os.Stdout and point it to os.Stderr for any rogue library logs.
	originalStdout := os.Stdout
	os.Stdout = os.Stderr

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var req MCPRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			sendError(originalStdout, req.ID, -32700, "Parse error", err.Error())
			continue
		}

		// Dispatch request to a goroutine so that long-running tools do not block ping/keep-alive messages
		go handleMCPRequest(originalStdout, req)
	}

	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func handleMCPRequest(out *os.File, req MCPRequest) {
	switch req.Method {
	case "initialize":
		sendResponse(out, req.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "goodls",
				"version": "3.4.0",
			},
		})
	case "ping":
		// Required by MCP to keep the connection alive
		sendResponse(out, req.ID, map[string]any{})
	case "notifications/initialized", "notifications/cancelled":
		// Ignore notifications (they have no ID and expect no response)
		return
	case "tools/list":
		sendResponse(out, req.ID, map[string]any{
			"tools": []map[string]any{
				{
					"name":        "download",
					"description": "Download shared files or folders from Google Drive without authentication. Use this to fetch data, datasets, or documents.",
					"inputSchema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"url":        map[string]any{"type": "string", "description": "Google Drive URL (file or folder)"},
							"conflict":   map[string]any{"type": "string", "description": "Conflict resolution strategy: 'skip', 'overwrite', 'newer', 'rename'."},
							"directory":  map[string]any{"type": "string", "description": "Target local directory to save the downloaded files."},
							"apikey":     map[string]any{"type": "string", "description": "Optional API key for downloading folders."},
							"proxy":      map[string]any{"type": "string", "description": "Optional HTTP/HTTPS proxy URL."},
							"retry":      map[string]any{"type": "integer", "description": "Optional max retry attempts for downloads."},
							"retryDelay": map[string]any{"type": "integer", "description": "Optional retry delay in seconds for exponential backoff."},
						},
						"required": []string{"url"},
					},
				},
			},
		})
	case "prompts/list":
		// Defensive fallback
		sendResponse(out, req.ID, map[string]any{"prompts": []any{}})
	case "resources/list":
		// Defensive fallback
		sendResponse(out, req.ID, map[string]any{"resources": []any{}})
	case "tools/call":
		var params struct {
			Name      string `json:"name"`
			Arguments struct {
				URL        string `json:"url"`
				Conflict   string `json:"conflict"`
				Directory  string `json:"directory"`
				APIKey     string `json:"apikey"`
				Proxy      string `json:"proxy"`
				Retry      int    `json:"retry"`
				RetryDelay int    `json:"retryDelay"`
			} `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(out, req.ID, -32602, "Invalid params", err.Error())
			return
		}

		if params.Name != "download" {
			sendError(out, req.ID, -32601, "Method not found", "Tool not found")
			return
		}

		retryDelay := params.Arguments.RetryDelay
		if retryDelay <= 0 {
			retryDelay = 2
		}

		executeToolDownload(out, req.ID, params.Arguments.URL, params.Arguments.Conflict, params.Arguments.Directory, params.Arguments.APIKey, params.Arguments.Proxy, params.Arguments.Retry, retryDelay)

	default:
		sendError(out, req.ID, -32601, "Method not found", fmt.Sprintf("Unsupported method: %s", req.Method))
	}
}

func executeToolDownload(out *os.File, reqID any, url, conflict, directory, apiKey, proxy string, retry, retryDelay int) {
	if directory == "" {
		directory, _ = filepath.Abs(".")
	} else {
		// Ensure robust resolution and auto-create the target directory to prevent 'no such file' errors
		directory, _ = filepath.Abs(directory)
	}

	if err := os.MkdirAll(directory, 0755); err != nil {
		sendResponse(out, reqID, map[string]any{
			"content": []map[string]any{
				{
					"type": "text",
					"text": fmt.Sprintf("Failed to create target directory: %v", err),
				},
			},
			"isError": true,
		})
		return
	}

	if conflict == "" {
		conflict = "prompt"
	}

	apiKeyToUse := apiKey
	if apiKeyToUse == "" {
		apiKeyToUse = os.Getenv("GOODLS_APIKEY") // directly read instead of referencing unexported var cleanly
	}

	p := &Para{
		Disp:             true, // Disables progress bar rendering which would break JSON-RPC
		MCPMode:          true, // Enables aggressive fail-fast logic for prompts
		DownloadBytes:    -1,
		WorkDir:          directory,
		Concurrency:      5,
		ConflictStrategy: conflict,
		APIKey:           apiKeyToUse,
		Proxy:            proxy,
		Retry:            retry,
		RetryDelay:       retryDelay,
		ResultJSONs:      &[]string{},
		mu:               &sync.Mutex{},
	}

	err := p.download(url)
	if err != nil {
		// As per MCP spec, tool execution errors should be returned gracefully inside the result object with isError: true
		sendResponse(out, reqID, map[string]any{
			"content": []map[string]any{
				{
					"type": "text",
					"text": err.Error(),
				},
			},
			"isError": true,
		})
		return
	}

	var results []string
	p.mu.Lock()
	results = append(results, *p.ResultJSONs...)
	p.mu.Unlock()

	summary := "Download completed successfully."
	if len(results) > 0 {
		summary += "\nDetails:\n" + strings.Join(results, "\n")
	}

	sendResponse(out, reqID, map[string]any{
		"content": []map[string]any{
			{
				"type": "text",
				"text": summary,
			},
		},
		"isError": false,
	})
}

func sendResponse(out *os.File, id any, result any) {
	// JSON-RPC 2.0: Notifications do not expect a response
	if id == nil {
		return
	}
	res := MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	b, _ := json.Marshal(res)

	stdoutMu.Lock()
	defer stdoutMu.Unlock()
	out.Write(b)
	out.Write([]byte("\n"))
}

func sendError(out *os.File, id any, code int, message, data string) {
	// JSON-RPC 2.0: Notifications do not expect an error response, except Parse Error (-32700)
	if id == nil && code != -32700 {
		return
	}
	res := MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: map[string]any{
			"code":    code,
			"message": message,
			"data":    data,
		},
	}
	b, _ := json.Marshal(res)

	stdoutMu.Lock()
	defer stdoutMu.Unlock()
	out.Write(b)
	out.Write([]byte("\n"))
}
