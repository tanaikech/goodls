/*
Package goodls (doc.go) :
This is a high-performance CLI tool to download shared files and entire folder structures from Google Drive.

We know that shared files on Google Drive can be downloaded without authorization. However, handling large files, extracting entire folder structures, and doing so efficiently requires a robust architecture. goodls automates these complex underlying Drive API mechanics.

# Core Features

  - Frictionless Anonymous Downloads:
    Download shared files directly without OAuth2 setup. Large file endpoint routing is handled automatically.

  - High-Speed Concurrent Folder Extraction:
    Download entire shared folders perfectly preserving their internal directory structure. Powered by Go's advanced Goroutine worker pools and strictly enforced channel semaphores, multiple files are downloaded in parallel to maximize throughput.

  - Beautiful Multi-Progress UI:
    A synchronized, real-time multi-bar interface displays live speeds, ETAs, and completion percentages for every active thread, gracefully handling indeterminate sizes for Google Workspace exports.

  - Bulletproof Resumable Downloads & Proxies:
    Run resumable downloads for massive datasets using specific byte chunks, backed by MD5 checksum verification. Full support for corporate proxy environments via standard configuration or explicit flags.

  - Secure Credential Management:
    Strict API key masking prevents credential leaks in CI/CD logs. Users can also enforce an explicit anonymous mode to bypass local environment variables safely.

  - AI Agent Resilience (MCP Integration):
    Fully compliant with Model Context Protocol via standard stdio JSON-RPC. Automatically manages headless conflict resolutions, execution constraints, explicit JSON outputs, and exponential backoff retries when invoked by autonomous agents (e.g. Claude Desktop, Cursor).

---------------------------------------------------------------

# MCP (Model Context Protocol) Integration

goodls natively supports MCP, allowing AI agents to directly invoke downloading capabilities via stdio JSON-RPC.

Configuration Example:

	{
	  "mcpServers": {
	    "goodls": {
	      "command": "/absolute/path/to/goodls",
	      "args": ["mcp"],
	      "env": {
	        "GOODLS_APIKEY": "YOUR_API_KEY_HERE"
	      }
	    }
	  }
	}

Sample Prompts for AI Agents:

  - "Download the Google Drive file at https://drive.google.com/file/d/xxxxxx/view to the ./data directory. If it already exists, please overwrite it."

  - "Fetch all files from this shared folder (https://drive.google.com/drive/folders/xxxxxx) using goodls, save them to ./datasets, skip any files we already have locally, and use 3 retries in case of network issues."

---------------------------------------------------------------

# How to Install

Option 1: Download a pre-compiled binary
Download an executable file of goodls from https://github.com/tanaikech/goodls/releases.
We support modern architectures including Apple Silicon (darwin_arm64), Windows on ARM (windows_arm64), and standard 64-bit/32-bit systems for macOS, Linux, and Windows.

Option 2: Use go install (Requires Go 1.26+)

	$ go install github.com/tanaikech/goodls/cmd/goodls@latest

---------------------------------------------------------------

# Usage Examples

Basic File Download (No API key required):

	$ goodls -u [URL of shared file on Google Drive]

Download a Folder (Requires API key):

	$ goodls -u [URL of shared folder on Google Drive] -key [API key]

Download a Folder with Custom Concurrency (e.g., 10 parallel downloads):

	$ goodls -u [URL of shared folder] -key [API key] -c 10

Force Anonymous Access (Ignore environment variables):

	$ goodls -u [URL of shared file] --no-apikey

Download behind Corporate Proxy with Debug Logging and JSON Output:

	$ goodls -u [URL] --proxy http://proxy.example.com:8080 --verbose --json

Run as MCP Server:

	$ goodls mcp

---------------------------------------------------------------
*/
package goodls
