package httptines

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// proxySrc represents a map of proxy source URLs grouped by schema
type proxySrc map[string][]string

// proxyMap represents a set of proxy URLs
type proxyMap map[*url.URL]bool

// srvMap represents a map of server
type srvMap map[string]any

// Worker represents a worker instance that manages proxy servers and request processing
type Worker struct {
	// Interval defines the time (in seconds) between proxy downloads and health checks.
	Interval int `default:"120"`
	// Port specifies the HTTP server port for the web interface
	Port int `default:"8080"`
	// Workers determines the buffer size for proxy servers.
	// - In "minimal" strategy, it represents the maximum number of concurrent connections.
	// - In "auto" strategy, it defines the number of parent workers, while child workers
	//   are dynamically allocated based on proxy server capacity.
	//
	// Example (minimal):
	//   If Workers == 100, then max concurrent requests == 100.
	//
	// Example (auto):
	//   If Workers == 50 and each proxy server supports 100 concurrent connections,
	//   then max concurrent requests == 500.
	Workers int `default:"100"`
	// Sources contains a map of proxy source URLs grouped by schema (http/https/socks4/socks5)
	Sources proxySrc `validate:"required"`
	// StatInterval defines the interval (in seconds) for updating statistics.
	StatInterval int `default:"2"`
	// Strategy determines the load balancing approach: "minimal" or "auto".
	//
	// - "minimal" Single-threaded mode, suitable for proxies with limited concurrency.
	// - "auto" Dynamically adjusts concurrency based on proxy capabilities.
	Strategy string `default:"minimal"`
	// Timeout specifies the request timeout in seconds
	Timeout int `default:"10"`
	// URL used for testing the connection
	TestTarget string `validate:"required"`

	srvCh   chan *Server   // Channel for server instances
	timCh   chan time.Time // Channel for time updates
	stsCh   chan srvMap    // Channel for statistics updates
	m       sync.RWMutex   // Mutex for thread-safe operations
	o       sync.Once      // Used to close srvCh
	stat    *Stat          // Servers statistics
	targets []string       // List of target URLs to process
}

// Run initializes and starts the worker with the given targets and handler function.
// Parameters:
//   - targets: List of URLs to process
//   - handler: Callback function to process the response body
func (w *Worker) Run(targets []string, handler func([]byte)) {

	w.targets = targets
	w.stat = &Stat{Targets: len(targets), Servers: map[string]any{}}

	w.srvCh = make(chan *Server, w.Workers)
	w.stsCh = make(chan srvMap)
	w.timCh = make(chan time.Time)

	validate(w)
	setDefaultValues(w)

	go listenAndServe(w.Port)
	go w.fetchAndCheck()
	go w.updateStat()
	go w.sendStatistics()

	for s := range w.srvCh {
		go handleServer(w, s, handler)
	}

	// Waiting for last send statistics
	time.Sleep(time.Duration(w.StatInterval) * time.Second)
}

// handleServer processes requests for a specific proxy server
// Parameters:
//   - w: The worker instance managing the proxy servers
//   - s: The server instance to handle requests for
//   - handler: Callback function to process the response body
func handleServer(w *Worker, s *Server, handler func([]byte)) {
	ca := s.Capacity
	qu := make(chan any, ca)

	for {
		if atomic.LoadUint32(&s.Disabled) > 0 {
			break
		}

		targets := w.shift(ca)
		if len(targets) == 0 {
			if w.stat.allTargetsProcessed() {
				w.o.Do(func() {
					close(w.srvCh)
				})
				break
			}

			time.Sleep(time.Second)
			continue
		}

		for _, t := range targets {
			qu <- struct{}{}
			go processTarget(w, t, s, qu, handler)
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

// updateStat processes statistics updates from channels
func (w *Worker) updateStat() {
	for {
		select {
		case d := <-w.stsCh:
			w.stat.addServer(d)
		case d := <-w.timCh:
			w.stat.addTimestamp(d)
		}
	}
}

// sendStatistics periodically broadcasts statistics to connected clients
func (w *Worker) sendStatistics() {
	for {
		w.stat.m.RLock()
		p, _ := json.Marshal(Payload{"stat", w.stat})
		broadcast <- p
		w.stat.m.RUnlock()

		time.Sleep(time.Duration(w.Timeout) * time.Second)
	}
}

// fetchAndCheck periodically fetches and validates proxy servers
// It runs in a separate goroutine and performs the following tasks:
// 1. Fetches proxy lists from configured sources
// 2. Validates each proxy's connectivity and performance
// 3. Updates the server channel with working proxies
// Parameters:
//   - w: Worker
func (w *Worker) fetchAndCheck() {
	ticker := time.NewTicker(time.Duration(w.Interval) * time.Second)
	defer ticker.Stop()

	for {
		proxies := fetchProxies(w.Sources)
		for _, s := range checkProxies(w, proxies) {
			w.srvCh <- s
		}
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
func checkProxies(w *Worker, proxies proxyMap) []*Server {
	var alive []*Server
	var mu sync.Mutex
	var count uint32

	ch := make(chan any, w.Workers)

	if len(proxies) == 0 {
		wlog("no proxies to check")
		return nil
	}

	wlog(fmt.Sprintf("%s strategy was applied", w.Strategy))
	wlog(fmt.Sprintf("checking %d proxies", len(proxies)))

	for u := range proxies {
		ch <- struct{}{}

		go func(u *url.URL) {
			defer func() {
				<-ch
				atomic.AddUint32(&count, 1)
			}()

			s := &Server{
				URL:     u,
				timeout: time.Duration(w.Timeout) * time.Second,
			}

			s.ctx, s.cancel = context.WithCancel(context.Background())
			s.computeCapacity(w.Strategy, w.TestTarget)
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

	// Size of slice header (3 words: pointer, length, capacity)
	headerSize := unsafe.Sizeof(alive)
	// Size of the underlying array (capacity * size of one element)
	dataSize := uintptr(cap(alive)) * unsafe.Sizeof(alive[0])
	totalSize := headerSize + dataSize

	fmt.Printf("Slice header size: %d bytes\n", headerSize)
	fmt.Printf("Underlying array size: %d bytes\n", dataSize)
	fmt.Printf("Total slice size: %d bytes\n", totalSize)

	return alive
}

func (w *Worker) stop() {
	w.o.Do(func() {
		close(w.srvCh)
	})
}

// processTarget processes a target URL using the provided proxy server and returns the response body.
// Parameters:
//   - w: Worker
//   - t: URL to process
//   - s: Server to use for the request
//
// Returns:
//   - []byte: Response body
//   - error: Any error that occurred
func processTarget(
	w *Worker,
	t string,
	s *Server,
	q <-chan any,
	handler func([]byte),
) ([]byte, error) {
	defer func() { <-q }()

	startedAt, sm := s.start()
	if v, _ := sm["disabled"]; v.(uint32) == 0 {
		w.stsCh <- sm
	}

	body, err := request(s.ctx, t, s)
	sm = s.finish(startedAt, err)
	if err != nil {
		w.retrigger(t)
	} else {
		handler(body)
		w.timCh <- time.Now()
	}

	if v, _ := sm["disabled"]; v.(uint32) == 0 {
		w.stsCh <- sm
	}

	return body, err
}
