# httptines

[![Go Reference](https://pkg.go.dev/badge/golang.org/x/example.svg)](https://pkg.go.dev/github.com/grishkovelli/httptines)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://github.com/grishkovelli/httptines/blob/master/LICENSE)

**httptines** is a powerful Go package for parsing websites using public proxy servers. It provides an efficient and flexible way to handle web scraping tasks with automatic proxy management, load balancing, and real-time monitoring.

<img src="screenshot.png" width="480">

## Why

To better understand concurrency in Go.

## Features

- **Automatic Proxy Management**: Automatically fetches and validates proxy servers from multiple sources
- **Load Balancing**: Two strategies for proxy server utilization:
  - **Minimal Strategy**: Single-threaded mode, ideal for proxies with limited concurrent connections
  - **Auto Strategy**: Automatically determines optimal concurrent connections per proxy
- **Real-time Monitoring**: Web interface for monitoring proxy performance and statistics
- **Robust Error Handling**: Automatic proxy rotation and retry mechanism
- **Configurable Timeouts**: Customizable request timeouts
- **User Agent Rotation**: Built-in user agent rotation to avoid detection

## Installation

```bash
go get github.com/grishkovelli/httptines
```

## Usage

```go
w := httptines.Worker{
    Strategy: "minimal",  // or "auto"
    Timeout:  5,         // request timeout in seconds
    Sources: map[string][]string{
        "http": {
            "https://proxy-source-1/http.txt",
            "https://proxy-source-2/http.txt",
            "https://proxy-source-3/http.txt",
        },
    },
    Interval: 300,       // proxy check interval in seconds
}

targets := []string{
  "target.com/page-1",
  "target.com/page-2",
  "target.com/page-n",
}

w.Run(targets, "https://target.com", func(body []byte) {
    // Process the response body
    // e.g., parse HTML, extract data, write to database
})
```

### Configuration Options

The `Worker` struct supports the following configuration options:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `Strategy` | string | "minimal" | Proxy selection strategy ("minimal" or "auto") |
| `Timeout` | int | 10 | Request timeout in seconds |
| `Interval` | int | 120 | Time between proxy checks in seconds |
| `Port` | int | 8080 | HTTP server port for web interface |
| `PBuff` | int | 1000 | Buffer size for proxy processing |
| `StatInterval` | int | 2 | Statistics update interval in seconds |
| `Sources` | map[string][]string | required | Map of proxy source URLs grouped by schema |

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.