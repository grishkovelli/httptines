package httptines

import (
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

var sortProxiesDirection int32

//  ██████╗  █████╗ ██╗      █████╗ ███╗   ██╗ ██████╗███████╗██████╗
//  ██╔══██╗██╔══██╗██║     ██╔══██╗████╗  ██║██╔════╝██╔════╝██╔══██╗
//  ██████╔╝███████║██║     ███████║██╔██╗ ██║██║     █████╗  ██████╔╝
//  ██╔══██╗██╔══██║██║     ██╔══██║██║╚██╗██║██║     ██╔══╝  ██╔══██╗
//  ██████╔╝██║  ██║███████╗██║  ██║██║ ╚████║╚██████╗███████╗██║  ██║
//  ╚═════╝ ╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝╚═╝  ╚═══╝ ╚═════╝╚══════╝╚═╝  ╚═╝
//

type balancer struct {
	alive   []*server
	proxies []*url.URL
	m       sync.RWMutex
	w       *Worker
}

func (b *balancer) fetchProxies() {
	fmt.Println("* Fetching proxies")
	var proxies []*url.URL

	for schema, links := range b.w.Sources {
		for _, link := range links {
			resp, err := http.Get(link)
			if err != nil {
				fmt.Printf("Error fetching proxies from %s: %v\n", link, err)
				continue
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				fmt.Printf("Failed to download proxy list from %s: status %d\n", link, resp.StatusCode)
				continue
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				fmt.Printf("Error reading response body from %s: %v\n", link, err)
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

func (b *balancer) checkProxies() {
	fmt.Printf("Checking %d proxies\n", len(b.proxies))

	var alive []*server
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, proxy := range b.proxies {
		wg.Add(1)
		srv := &server{url: proxy}
		go func(s *server) {
			defer wg.Done()
			if err := b.check(s); err == nil {
				mu.Lock()
				alive = append(alive, s)
				mu.Unlock()
			}
		}(srv)
	}
	wg.Wait()
	b.merge(alive)
}

func (b *balancer) check(s *server) error {
	startAt := s.registerStart()
	defer s.registerFinish(startAt)

	_, err := doRequest(b.w.testTarget, s, b.w.ProxyTimeout, b.w.userAgent())
	return err
}

func (b *balancer) run(runup chan<- struct{}) {
	firstRun := true
	ticker := time.NewTicker(time.Duration(b.w.ProxyInterval) * time.Second)
	defer ticker.Stop()

	for {
		b.fetchProxies()
		b.checkProxies()
		if firstRun {
			firstRun = false
			close(runup)
		}
		<-ticker.C
	}
}

func (b *balancer) merge(s []*server) {
	b.m.Lock()
	defer b.m.Unlock()

	servers := make([]*server, 0, len(s))

	for _, new := range s {
		isNew := true

		for _, current := range b.alive {
			if current.url == new.url {
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
}

func (b *balancer) nextServer() *server {
	b.m.Lock()
	defer b.m.Unlock()
	computeCapacity(b.w.Goroutines, b.alive)
	sortByDirection(b.alive)
	return bestServer(b.alive)
}

func (b *balancer) analyzeServer(s *server) {
	b.m.Lock()
	s.m.Lock()
	defer b.m.Unlock()
	defer s.m.Unlock()

	s.negative++
	i := slices.IndexFunc(b.alive, func(i *server) bool {
		return i.url == s.url
	})

	mult3x := (s.positive == 0 && s.negative >= 3) || (s.positive > 0 && (s.negative/s.positive) >= 3)
	if i >= 0 && mult3x {
		b.alive = append(b.alive[:i], b.alive[i+1:]...)
		return
	}

	switch {
	case s.limit > 0:
		s.limit--
	case s.requests > 0:
		s.limit = int(s.requests - 1)
	default:
		s.limit = 0
	}
}

//  ███████╗███████╗██████╗ ██╗   ██╗███████╗██████╗
//  ██╔════╝██╔════╝██╔══██╗██║   ██║██╔════╝██╔══██╗
//  ███████╗█████╗  ██████╔╝██║   ██║█████╗  ██████╔╝
//  ╚════██║██╔══╝  ██╔══██╗╚██╗ ██╔╝██╔══╝  ██╔══██╗
//  ███████║███████╗██║  ██║ ╚████╔╝ ███████╗██║  ██║
//  ╚══════╝╚══════╝╚═╝  ╚═╝  ╚═══╝  ╚══════╝╚═╝  ╚═╝
//

type server struct {
	url      *url.URL
	weight   float64
	capacity int
	latency  int
	requests int
	limit    int
	positive int
	negative int
	m        sync.RWMutex
}

func (s *server) registerStart() time.Time {
	s.m.Lock()
	defer s.m.Unlock()
	s.requests++
	return time.Now()
}

func (s *server) registerFinish(t time.Time) {
	s.m.Lock()
	defer s.m.Unlock()
	s.latency = int(time.Since(t).Milliseconds())
	s.requests--
}

func (s *server) String() string {
	s.m.RLock()
	defer s.m.RUnlock()

	return fmt.Sprintf("L=%d R=%d C=%d Lim=%d p=%d n=%d %s", s.latency, s.requests, s.capacity, s.limit, s.positive, s.negative, s.url)
}

//  ██╗  ██╗███████╗██╗     ██████╗ ███████╗██████╗ ███████╗
//  ██║  ██║██╔════╝██║     ██╔══██╗██╔════╝██╔══██╗██╔════╝
//  ███████║█████╗  ██║     ██████╔╝█████╗  ██████╔╝███████╗
//  ██╔══██║██╔══╝  ██║     ██╔═══╝ ██╔══╝  ██╔══██╗╚════██║
//  ██║  ██║███████╗███████╗██║     ███████╗██║  ██║███████║
//  ╚═╝  ╚═╝╚══════╝╚══════╝╚═╝     ╚══════╝╚═╝  ╚═╝╚══════╝
//

func sortByDirection(s []*server) {
	if sortProxiesDirection%2 == 0 {
		sort.Slice(s, func(i, j int) bool { return s[i].weight < s[j].weight })
	} else {
		sort.Slice(s, func(i, j int) bool { return s[i].weight > s[j].weight })
	}
	atomic.AddInt32(&sortProxiesDirection, 1)
}

func computWeight(servers []*server) float64 {
	totalWeight := 0.0
	for _, s := range servers {
		s.m.Lock()
		s.weight = 1.0 / float64(s.latency)
		totalWeight += s.weight
		s.m.Unlock()
	}
	return totalWeight
}

func computeCapacity(requests int, servers []*server) {
	totalWeight := computWeight(servers)

	for _, s := range servers {
		s.m.Lock()
		pct := s.weight / totalWeight
		if s.limit > 0 {
			s.capacity = s.limit
		} else {
			s.capacity = int(math.Round(pct * float64(requests)))
		}
		s.m.Unlock()
	}
}

func bestServer(servers []*server) *server {
	var bs *server

	for _, s := range servers {
		s.m.Lock()
		if s.capacity > s.requests {
			s.requests++
			bs = s
			s.m.Unlock()
			break
		}
		s.m.Unlock()
	}

	return bs
}
