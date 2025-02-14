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
	"time"
)

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
	startAt := s.startRequest()
	defer s.finishRequest(startAt)

	_, err := doRequest(b.w.TestTarget, s, b.w.ProxyTimeout, b.w.userAgent())
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

func (b *balancer) removeServer(s *server) {
	b.m.Lock()
	defer b.m.Unlock()

	for i := range b.alive {
		if b.alive[i].url == s.url {
			b.alive = append(b.alive[:i], b.alive[i+1:]...)
			break
		}
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

	computeCapacity(b.w.Parallel, servers)

	fmt.Printf("alive=%d\n", len(servers))
	b.alive = servers
}

func (b *balancer) nextServer() *server {
	b.m.Lock()
	defer b.m.Unlock()

	computeCapacity(b.w.Parallel, b.alive)

	sort.Slice(b.alive, func(i, j int) bool {
		return b.alive[i].weight > b.alive[j].weight
	})

	return bestServer(b.alive)
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
	capacity int32
	latency  int64
	requests int32
	m        sync.RWMutex
}

func (s *server) startRequest() time.Time {
	s.m.Lock()
	defer s.m.Unlock()
	s.requests++
	return time.Now()
}

func (s *server) finishRequest(finishAt time.Time) {
	s.m.Lock()
	defer s.m.Unlock()
	dur := time.Since(finishAt).Milliseconds()
	s.latency = (dur + s.latency) / int64(s.requests)
	s.requests--
}

func (s *server) String() string {
	s.m.RLock()
	defer s.m.RUnlock()

	return fmt.Sprintf("L=%d R=%d C=%d %s", s.latency, s.requests, s.capacity, s.url)
}

//  ██╗  ██╗███████╗██╗     ██████╗ ███████╗██████╗ ███████╗
//  ██║  ██║██╔════╝██║     ██╔══██╗██╔════╝██╔══██╗██╔════╝
//  ███████║█████╗  ██║     ██████╔╝█████╗  ██████╔╝███████╗
//  ██╔══██║██╔══╝  ██║     ██╔═══╝ ██╔══╝  ██╔══██╗╚════██║
//  ██║  ██║███████╗███████╗██║     ███████╗██║  ██║███████║
//  ╚═╝  ╚═╝╚══════╝╚══════╝╚═╝     ╚══════╝╚═╝  ╚═╝╚══════╝
//

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
		s.capacity = int32(math.Round(pct * float64(requests)))
		s.m.Unlock()
	}
}

func bestServer(servers []*server) *server {
	var bs *server

	for _, s := range servers {
		s.m.RLock()
		if s.capacity > s.requests {
			bs = s
			break
		}
		s.m.RUnlock()
	}

	if bs == nil {
		return nil
	}

	defer bs.m.RUnlock()
	return bs
}
