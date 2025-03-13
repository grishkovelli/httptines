package httptines

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	srvsCh = make(chan *Server, 500)          // Channel for server instances. TODO: make channel length configurable
	statCh = make(chan map[string]any)        // Channel for statistics updates
	timeCh = make(chan time.Time)             // Channel for time updates
	stat   = &Stat{Servers: map[string]any{}} // Global statistics object
	cfg    config                             // Global configuration
)

// proxySrc represents a map of proxy source URLs grouped by schema
type proxySrc map[string][]string

// proxyMap represents a set of proxy URLs
type proxyMap map[*url.URL]bool

// Worker represents a worker instance that manages proxy servers and request processing
type Worker struct {
	// Interval specifies the time in seconds between proxy checks
	Interval int `default:"120"`
	// Port specifies the HTTP server port for the web interface
	Port int `default:"8080"`
	// PBuff specifies the buffer size for proxy processing
	PBuff int `default:"1000"`
	// Sources contains a map of proxy source URLs grouped by schema (http/https)
	Sources proxySrc `validate:"required"`
	// StatInterval specifies the interval in seconds for statistics updates
	StatInterval int `default:"2"`
	// Strategy specifies the proxy selection strategy (minimal/auto)
	Strategy string `default:"minimal"`
	// Timeout specifies the request timeout in seconds
	Timeout int `default:"10"`

	m       sync.RWMutex // Mutex for thread-safe operations
	targets []string     // List of target URLs to process
}

// Run initializes and starts the worker with the given targets and handler function.
// Parameters:
//   - targets: List of URLs to process
//   - testTarget: URL used for testing the connection
//   - handleBody: Callback function to process the response body
func (w *Worker) Run(targets []string, testTarget string, handleBody func([]byte)) {
	w.targets = targets
	stat.Targets = len(targets)

	validate(w)
	setDefaultValues(w)

	cfg = config{
		interval:     w.Interval,
		pbuff:        w.PBuff,
		sources:      w.Sources,
		strategy:     w.Strategy,
		testTarget:   testTarget,
		timeout:      w.Timeout,
		statInterval: w.StatInterval,
	}

	go listenAndServe(w.Port)
	go fetchAndCheck()
	go updateStat()
	go sendStatistics()

	for {
		size := w.size()

		if size < 1 {
			fmt.Println("No items")
			break
		}

		for s := range srvsCh {
			go handleServer(w, s, handleBody)
		}
	}
}

// handleServer processes requests for a specific proxy server
// Parameters:
//   - w: The worker instance managing the proxy servers
//   - s: The server instance to handle requests for
//   - handleBody: Callback function to process the response body
func handleServer(w *Worker, s *Server, handleBody func([]byte)) {
	defer func() { stat.removeServer(s.URL.String()) }()
	cap := s.Capacity
	queue := make(chan any, cap)

	for {
		targets := w.shift(cap)

		if len(targets) == 0 || atomic.LoadUint32(&s.Disabled) > 0 {
			break
		}

		for _, t := range targets {
			queue <- struct{}{}

			go func(t string) {
				defer func() { <-queue }()

				body, err := processTarget(t, s)
				if err != nil {
					w.retrigger(t)
				} else {
					handleBody(body)
				}
			}(t)
		}
	}
}

// retrigger adds a URL back to the target list for reprocessing.
// Parameters:
//   - u: URL to be reprocessed
func (w *Worker) retrigger(u string) {
	w.m.Lock()
	w.targets = append(w.targets, u)
	w.m.Unlock()
}

// shift removes and returns the first n targets from the worker's target list.
// Parameters:
//   - n: Number of targets to remove and return
//
// Returns:
//   - []string: Slice of removed targets
func (w *Worker) shift(n int) []string {
	w.m.Lock()
	defer w.m.Unlock()

	if len(w.targets) <= n {
		items := w.targets
		w.targets = nil
		return items
	}
	items := w.targets[:n]
	w.targets = w.targets[n:]
	return items
}

// size returns the current number of remaining targets.
// Returns:
//   - int: Number of remaining targets
func (w *Worker) size() int {
	w.m.RLock()
	defer w.m.RUnlock()
	return len(w.targets)
}

// fetchAndCheck periodically fetches and validates proxy servers
// It runs in a separate goroutine and performs the following tasks:
// 1. Fetches proxy lists from configured sources
// 2. Validates each proxy's connectivity and performance
// 3. Updates the server channel with working proxies
func fetchAndCheck() {
	ticker := time.NewTicker(time.Duration(cfg.interval) * time.Second)
	defer ticker.Stop()

	for {
		proxies := fetchProxies(cfg.sources)
		checkProxies(proxies)
		<-ticker.C
	}
}

// fetchProxies retrieves proxy lists from configured sources
// Parameters:
//   - s: Map of proxy source URLs grouped by schema
//
// Returns:
//   - proxyMap: Set of valid proxy URLs
func fetchProxies(s proxySrc) proxyMap {
	proxies := proxyMap{}

	wlog("fetching proxies")

	for schema, links := range s {
		for _, link := range links {
			resp, err := http.Get(link)
			if err != nil {
				wlog(fmt.Sprintf("error fetching proxies from %s: %v\n", link, err))
				continue
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				wlog(fmt.Sprintf("failed to download proxy list from %s: status %d\n", link, resp.StatusCode))
				continue
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				wlog(fmt.Sprintf("error reading response body from %s: %v\n", link, err))
				continue
			}

			for _, host := range strings.Split(string(body), "\n") {
				host = strings.TrimSpace(host)
				if host == "" {
					continue
				}

				if u, err := url.Parse(schema + "://" + host); err == nil {
					proxies[u] = true
				}
			}
		}
	}

	return proxies
}

// checkProxies validates and tests proxy servers
// Parameters:
//   - proxies: Set of proxy URLs to check
//
// The function:
// 1. Tests each proxy's connectivity using the configured test target
// 2. Determines optimal capacity based on the selected strategy
// 3. Sends working proxies to the server channel
func checkProxies(proxies proxyMap) {
	var alive []*Server
	var mu sync.Mutex
	var count uint32

	ch := make(chan any, cfg.pbuff)

	if len(proxies) == 0 {
		wlog("no proxies to check")
		return
	}

	wlog(fmt.Sprintf("%s strategy was applied", cfg.strategy))
	wlog(fmt.Sprintf("checking %d proxies", len(proxies)))

	for u := range proxies {
		ch <- struct{}{}

		go func(u *url.URL) {
			defer func() {
				<-ch
				atomic.AddUint32(&count, 1)
			}()

			s := &Server{URL: u}
			s.ctx, s.cancel = context.WithCancel(context.Background())
			s.computeCapacity(cfg.testTarget)
			if s.Capacity > 0 {
				mu.Lock()
				alive = append(alive, s)
				mu.Unlock()
			}
		}(u)
	}

	// Wait until all proxies are checked
	for atomic.LoadUint32(&count) < uint32(len(proxies)) {
		time.Sleep(time.Second)
	}

	wlog(fmt.Sprintf("Found %d alive proxies", len(alive)))

	for _, s := range alive {
		srvsCh <- s
	}
}

// processTarget processes a target URL using the provided proxy server and returns the response body.
// Parameters:
//   - target: URL to process
//   - s: Server to use for the request
//
// Returns:
//   - []byte: Response body
//   - error: Any error that occurred
func processTarget(target string, s *Server) ([]byte, error) {
	startedAt := s.start()
	body, err := request(s.ctx, target, s)
	s.finish(startedAt, err)
	return body, err
}
