package wlpb

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// toggleSortProxies is an atomic counter used to alternate the sorting direction of proxies
var toggleSortProxies int32

// wlog is a function variable that holds the logging function provided by the user
var wlog func(string)

//  ██████╗  █████╗ ██╗      █████╗ ███╗   ██╗ ██████╗███████╗██████╗
//  ██╔══██╗██╔══██╗██║     ██╔══██╗████╗  ██║██╔════╝██╔════╝██╔══██╗
//  ██████╔╝███████║██║     ███████║██╔██╗ ██║██║     █████╗  ██████╔╝
//  ██╔══██╗██╔══██║██║     ██╔══██║██║╚██╗██║██║     ██╔══╝  ██╔══██╗
//  ██████╔╝██║  ██║███████╗██║  ██║██║ ╚████║╚██████╗███████╗██║  ██║
//  ╚═════╝ ╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝╚═╝  ╚═══╝ ╚═════╝╚══════╝╚═╝  ╚═╝
//

// Balancer represents a proxy load balancer that manages multiple proxy servers
// and distributes requests among them based on their performance and capacity.
type Balancer struct {
	// Requests is the total number of concurrent requests the balancer should handle
	Requests int `json:"requests"`

	// Periodicity is the interval in seconds between proxy checks
	Periodicity int `json:"periodicity"`

	// Sources is a map of proxy schemas to lists of URLs containing proxy addresses
	Sources map[string][]string `json:"sources"`

	// TestURL is the URL used to test proxy connectivity and performance
	TestURL string `json:"testURL"`

	// Timeout is the maximum time in seconds to wait for proxy responses
	Timeout int `json:"timeout"`

	// UserAgent is the User-Agent string to use in proxy requests
	UserAgent string `json:"userAgent"`

	alive    []*Server    // alive contains the list of currently working proxy servers
	proxies  []*url.URL   // proxies contains the list of all proxy URLs, working or not
	positive []time.Time  // positive contains timestamps of successful requests
	m        sync.RWMutex // m is a mutex for protecting concurrent access to balancer data
}

// Run starts the balancer's main operation loop. It periodically fetches and checks proxies
// based on the configured periodicity. The function runs indefinitely until stopped.
// Parameters:
//   - logFunc: A function that takes a string parameter for logging messages
func (b *Balancer) Run(logFunc func(string)) {
	wlog = logFunc

	ticker := time.NewTicker(time.Duration(b.Periodicity) * time.Second)
	defer ticker.Stop()

	for {
		func() {
			defer func() {
				if r := recover(); r != nil {
					wlog(fmt.Sprintf("recovered: %v", r))
				}
			}()
			b.fetchProxies()
			b.checkProxies()
		}()
		<-ticker.C
	}
}

// NextServer returns the most suitable server based on current load balancing criteria.
// It uses thread-safe operations to compute capacity and sort available servers.
// Returns:
//   - *Server: The best available server, or nil if no servers are available
func (b *Balancer) NextServer() *Server {
	b.m.Lock()
	defer b.m.Unlock()

	for _, s := range b.alive {
		s.m.Lock()
	}

	computeCapacity(b.Requests, b.alive)
	sortAliveProxies(b.alive)
	bs := bestServer(b.alive)

	for _, s := range b.alive {
		s.m.Unlock()
	}

	return bs
}

// Request performs an HTTP request through a proxy server
// Parameters:
//   - target: The target URL to request
//   - agent: The User-Agent string to use in the request
//
// Returns:
//   - []byte: The response body
//   - error: Any error that occurred during the request
//   - bool: Whether a proxy was available to make the request
func (b *Balancer) Request(target, agent string) ([]byte, error, bool) {
	s := b.NextServer()
	if s == nil {
		return nil, nil, false
	}

	body, err := makeRequest(target, agent, b.Timeout, s)
	if err == nil {
		// b.updatePositive()
	}

	return body, err, true
}

// MarshalJSON implements custom JSON serialization for the Balancer type
// Returns:
//   - []byte: JSON representation of the Balancer
//   - error: Any error during marshaling
func (b *Balancer) MarshalJSON() ([]byte, error) {
	b.m.RLock()
	defer b.m.RUnlock()

	type Alias Balancer

	return json.Marshal(&struct {
		Alive    []*Server `json:"alive"`
		Proxies  int       `json:"proxies"`
		RPM      int       `json:"rpm"`
		Positive int       `json:"positive"`
		*Alias
	}{
		Alive:    b.alive,
		Proxies:  len(b.proxies),
		RPM:      b.rpm(),
		Positive: len(b.positive),
		Alias:    (*Alias)(b),
	})
}

// fetchProxies downloads and parses proxy lists from configured sources.
// It updates the internal proxies slice with new proxy URLs.
func (b *Balancer) fetchProxies() {
	var proxies []*url.URL

	wlog("fetching proxies")

	for schema, links := range b.Sources {
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

				if proxy, err := url.Parse(schema + "://" + host); err == nil && !slices.Contains(proxies, proxy) {
					proxies = append(proxies, proxy)
				}
			}
		}
	}

	b.m.Lock()
	b.proxies = proxies
	b.m.Unlock()
}

// checkProxies tests all proxies against the configured test URL.
// It updates the internal alive servers list with working proxies.
func (b *Balancer) checkProxies() {
	var alive []*Server
	var wg sync.WaitGroup
	var mu sync.Mutex

	if len(b.proxies) == 0 {
		wlog("no proxies to check")
		return
	}

	wlog(fmt.Sprintf("checking %d proxies", len(b.proxies)))
	for _, proxy := range b.proxies {
		wg.Add(1)
		srv := &Server{URL: proxy}
		go func(s *Server) {
			defer wg.Done()
			if _, err := makeRequest(b.TestURL, b.UserAgent, b.Timeout, s); err == nil {
				mu.Lock()
				alive = append(alive, s)
				mu.Unlock()
			}
		}(srv)
	}
	wg.Wait()

	b.merge(alive)
}

// merge combines new working proxies with existing ones while preserving state.
// Parameters:
//   - s: Slice of new servers to merge with existing ones
func (b *Balancer) merge(s []*Server) {
	b.m.Lock()
	defer b.m.Unlock()

	servers := make([]*Server, 0, len(s))

	for _, new := range s {
		isNew := true

		for _, current := range b.alive {
			if current.URL == new.URL {
				isNew = false
				servers = append(servers, current)
				break
			}
		}

		if isNew {
			servers = append(servers, new)
		}
	}

	b.alive = servers
	wlog(fmt.Sprintf("merged %d alive proxies", len(servers)))
}

// updatePositive records a successful request timestamp. It used to calculate the requests per minute and total positive requests.
func (b *Balancer) updatePositive() {
	b.m.Lock()
	b.positive = append(b.positive, time.Now())
	b.m.Unlock()
}

// rpm calculates the current requests per minute based on successful requests
// Returns:
//   - int: Number of successful requests in the last minute
func (b *Balancer) rpm() int {
	b.m.RLock()
	defer b.m.RUnlock()

	rpm, lastMinute := 0, time.Now().Add(-60*time.Second)
	for i := len(b.positive) - 1; i > 0; i-- {
		if b.positive[i].Compare(lastMinute) >= 0 {
			rpm++
		} else {
			break
		}
	}
	return rpm
}

//  ███████╗███████╗██████╗ ██╗   ██╗███████╗██████╗
//  ██╔════╝██╔════╝██╔══██╗██║   ██║██╔════╝██╔══██╗
//  ███████╗█████╗  ██████╔╝██║   ██║█████╗  ██████╔╝
//  ╚════██║██╔══╝  ██╔══██╗╚██╗ ██╔╝██╔══╝  ██╔══██╗
//  ███████║███████╗██║  ██║ ╚████╔╝ ███████╗██║  ██║
//  ╚══════╝╚══════╝╚═╝  ╚═╝  ╚═══╝  ╚══════╝╚═╝  ╚═╝
//

// Server represents a proxy server with its current state and performance metrics
type Server struct {
	// URL is the proxy server's URL
	URL *url.URL `json:"url"`

	// Weight is the server's computed weight based on performance
	Weight float64 `json:"-"`

	// Capacity is the maximum number of concurrent requests this server can handle
	Capacity int `json:"capacity"`

	// Latency is the last measured response time in milliseconds
	Latency int `json:"latency"`

	// Requests is the current number of active requests
	Requests int `json:"requests"`

	// Limit is the maximum number of requests allowed (set after failures)
	Limit int `json:"limit"`

	// Positive is the count of successful requests
	Positive int `json:"positive"`

	// Negative is the count of failed requests
	Negative int `json:"negative"`

	// m is a mutex for protecting concurrent access to server data
	m sync.RWMutex
}

// MarshalJSON implements custom JSON serialization for the Server type
// Returns:
//   - []byte: JSON representation of the Server
//   - error: Any error during marshaling
func (s *Server) MarshalJSON() ([]byte, error) {
	s.m.RLock()
	defer s.m.RUnlock()

	type Alias Server
	return json.Marshal(&struct {
		URL         string  `json:"url"`
		NegativePct float64 `json:"negativePct"`
		*Alias
	}{
		URL:         s.URL.Host,
		NegativePct: s.negativePct(),
		Alias:       (*Alias)(s),
	})
}

// RegisterStart marks the beginning of a request and returns the start time
// Returns:
//   - time.Time: The timestamp when the request started
func (s *Server) RegisterStart() time.Time {
	s.m.Lock()
	s.Requests++
	s.m.Unlock()
	return time.Now()
}

// RegisterFinish records the completion of a request
// Parameters:
//   - startedAt: The timestamp when the request started
//   - err: Any error that occurred during the request
func (s *Server) RegisterFinish(startedAt time.Time, err error) {
	s.m.Lock()
	if err == nil {
		s.Positive++
	} else {
		s.Negative++
		s.Limit = s.Requests - 1
	}
	s.Requests--
	s.Latency = int(time.Since(startedAt).Milliseconds())
	s.m.Unlock()
}

// totalRequests returns the total number of requests handled by the server
// Returns:
//   - int: Sum of positive and negative requests
func (s *Server) totalRequests() int {
	return s.Positive + s.Negative
}

// negativePct calculates the percentage of failed requests
// Returns:
//   - float64: Percentage of negative requests rounded to nearest integer
func (s *Server) negativePct() float64 {
	n := float64(s.Negative)
	r := float64(s.totalRequests())
	return math.Round(n / r * 100)
}

//  ██╗  ██╗███████╗██╗     ██████╗ ███████╗██████╗ ███████╗
//  ██║  ██║██╔════╝██║     ██╔══██╗██╔════╝██╔══██╗██╔════╝
//  ███████║█████╗  ██║     ██████╔╝█████╗  ██████╔╝███████╗
//  ██╔══██║██╔══╝  ██║     ██╔═══╝ ██╔══╝  ██╔══██╗╚════██║
//  ██║  ██║███████╗███████╗██║     ███████╗██║  ██║███████║
//  ╚═╝  ╚═╝╚══════╝╚══════╝╚═╝     ╚══════╝╚═╝  ╚═╝╚══════╝
//

// sortAliveProxies sorts the servers slice based on their weights.
// It alternates between ascending and descending order.
// Parameters:
//   - s: Slice of servers to sort
func sortAliveProxies(s []*Server) {
	if toggleSortProxies%2 == 0 {
		sort.Slice(s, func(i, j int) bool { return s[i].Weight > s[j].Weight })
	} else {
		sort.Slice(s, func(i, j int) bool { return s[i].Weight < s[j].Weight })
	}
	atomic.AddInt32(&toggleSortProxies, 1)
}

// computeWeight calculates weights for all servers based on their latency
// Parameters:
//   - servers: Slice of servers to compute weights for
//
// Returns:
//   - float64: Total weight of all servers
func computeWeight(servers []*Server) float64 {
	totalWeight := 0.0
	for _, s := range servers {
		s.Weight = 1.0 / float64(s.Latency)
		totalWeight += s.Weight
	}
	return totalWeight
}

// computeCapacity distributes the total request capacity among servers
// Parameters:
//   - requests: Total number of requests to distribute
//   - servers: Slice of servers to distribute capacity to
func computeCapacity(requests int, servers []*Server) {
	totalWeight := computeWeight(servers)

	for _, s := range servers {
		pct := s.Weight / totalWeight
		if s.Limit > 0 {
			s.Capacity = s.Limit
		} else {
			s.Capacity = int(math.Max(math.Round(pct*float64(requests)), 1))
		}
	}
}

// bestServer selects the first available server with remaining capacity
// Parameters:
//   - servers: Slice of servers to choose from
//
// Returns:
//   - *Server: Best available server or nil if none available
func bestServer(servers []*Server) *Server {
	var bs *Server
	for _, s := range servers {
		if s.Capacity > s.Requests {
			bs = s
			break
		}
	}
	return bs
}

// defaultClient creates an HTTP client configured with a proxy
// Parameters:
//   - proxy: URL of the proxy to use
//   - timeout: Request timeout in seconds
//
// Returns:
//   - *http.Client: Configured HTTP client
func defaultClient(proxy *url.URL, timeout int) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxy),
		},
		Timeout: time.Duration(timeout) * time.Second,
	}
}

// makeRequest performs an HTTP request through a proxy server
// Parameters:
//   - target: Target URL to request
//   - agent: User-Agent string to use
//   - timeout: Request timeout in seconds
//   - s: Server to use as proxy
//
// Returns:
//   - []byte: Response body
//   - error: Any error that occurred
func makeRequest(target string, agent string, timeout int, s *Server) ([]byte, error) {
	var startedAt time.Time
	var err error

	defer func() { s.RegisterFinish(startedAt, err) }()

	startedAt = s.RegisterStart()
	req, err := http.NewRequest("GET", target, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", agent)
	client := defaultClient(s.URL, timeout)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		return nil, err
	}

	b, err := io.ReadAll(resp.Body)
	return b, err
}
