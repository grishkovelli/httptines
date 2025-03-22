package httptines

import (
	"context"
	"math"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

// Server represents a proxy server with its current state and performance metrics
type Server struct {
	// URL is the proxy server's URL
	URL *url.URL `json:"url"`
	// Disabled indicates whether the server is disabled (1) or enabled (0)
	Disabled uint32 `json:"disabled"`
	// Capacity represents the maximum number of concurrent requests this server can handle
	Capacity int `json:"capacity"`
	// Latency is the last measured response time in milliseconds
	Latency int `json:"latency"`
	// Requests is the current number of active requests being processed
	Requests int `json:"requests"`
	// Positive is the count of successful requests processed by this server
	Positive int `json:"positive"`
	// Negative is the count of failed requests processed by this server
	Negative int `json:"negative"`

	// Last five
	l5 [5]bool
	// Last five index
	l5i int
	// Timeout specifies the request timeout in seconds
	timeout time.Duration
	// m is a mutex for protecting concurrent access to server data
	m sync.RWMutex
	// ctx is the context for managing server lifecycle
	ctx context.Context
	// cancel is the function to cancel the server's context
	cancel context.CancelFunc
}

// Start marks the beginning of a request and returns the start time
// Returns:
//   - time.Time: The timestamp when the request started
func (s *Server) start() (time.Time, srvMap) {
	s.m.Lock()
	defer s.m.Unlock()

	s.Requests++

	return time.Now(), s.toMap()
}

// finish records the completion of a request
// Parameters:
//   - startedAt: The timestamp when the request started
//   - err: Any error that occurred during the request
func (s *Server) finish(startedAt time.Time, err error) srvMap {
	s.m.Lock()
	defer s.m.Unlock()

	s.Latency = int(time.Since(startedAt).Milliseconds())
	s.Requests--

	if err == nil {
		s.Positive++
		s.l5[s.l5i] = true
	} else {
		s.Negative++
		s.l5[s.l5i] = false
	}

	if s.l5i == 4 {
		s.l5i = 0
	} else {
		s.l5i++
	}

	if s.fiveFailInRow() {
		s.disable()
	}

	return s.toMap()
}

// disable disables the server and cancels its context
func (s *Server) disable() {
	atomic.AddUint32(&s.Disabled, 1)
	s.cancel()
}

// toMap converts server statistics to a map
// Returns:
//   - srvMap: Server statistics as a map
func (s *Server) toMap() srvMap {
	return srvMap{
		"url":        s.URL.String(),
		"disabled":   s.Disabled,
		"latency":    s.Latency,
		"capacity":   s.Capacity,
		"requests":   s.Requests,
		"positive":   s.Positive,
		"negative":   s.Negative,
		"efficiency": s.efficiency(),
	}
}

// efficiency calculates the server's success rate
// Returns:
//   - float64: Success rate as a percentage
func (s *Server) efficiency() float64 {
	total := s.Positive + s.Negative
	if total == 0 {
		return 0
	}
	return math.Round(float64(s.Positive*100) / float64(total))
}

// fiveFailInRow checks if the server has failed five times in a row
// Returns:
//   - bool: True if server has failed five times in a row
func (s *Server) fiveFailInRow() bool {
	for i := range s.l5 {
		if s.l5[i] {
			return false
		}
	}
	return true
}

// computeCapacity determines the server's capacity based on the configured strategy
// Parameters:
//   - target: URL to test capacity against
func (s *Server) computeCapacity(strategy, target string) {
	if strategy == "minimal" {
		s.minimalCapacity(target)
	} else {
		s.autoAdjustCapacity(target)
	}
}

// autoAdjustCapacity automatically determines optimal server capacity
// Parameters:
//   - target: URL to test capacity against
func (s *Server) autoAdjustCapacity(target string) {
	wg := sync.WaitGroup{}
	capacity := uint32(1)
	stop := uint32(0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for {
		for i := uint32(0); i < capacity; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				if _, err := request(ctx, target, s); err != nil {
					atomic.AddUint32(&stop, 1)
				}
			}()
		}
		wg.Wait()

		if atomic.LoadUint32(&stop) > 0 {
			if atomic.LoadUint32(&capacity) == 1 {
				atomic.StoreUint32(&capacity, 0)
			}
			break
		}

		atomic.AddUint32(&capacity, 1)
	}

	s.Capacity = int(capacity)
}

// minimalCapacity sets minimal server capacity
// Parameters:
//   - target: URL to test capacity against
func (s *Server) minimalCapacity(target string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := request(ctx, target, s); err == nil {
		s.Capacity = 1
	}
}
