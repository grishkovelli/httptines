package httptines

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestWorker(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "httptines")
}

var _ = Describe("Worker", func() {
	var w *Worker

	BeforeEach(func() {
		w = &Worker{
			Interval:     120,
			Port:         8080,
			Workers:      1000,
			StatInterval: 2,
			Strategy:     "minimal",
			Timeout:      10,
			Sources: proxySrc{
				"http":  {"http://test-proxy-list.com/http"},
				"https": {"http://test-proxy-list.com/https"},
			},
		}

		cfg = config{
			interval: w.Interval,
			Workers:  w.Workers,
			sources:  w.Sources,
			strategy: w.Strategy,
			timeout:  w.Timeout,
		}
	})

	Describe("shift()", func() {
		When("targets is empty", func() {
			It("returns empty slice", func() {
				w.targets = []string{}
				result := w.shift(5)
				Expect(result).To(BeEmpty())
			})
		})

		When("n is greater than available targets", func() {
			It("returns all targets", func() {
				w.targets = []string{"http://test1.com", "http://test2.com"}
				result := w.shift(5)
				Expect(result).To(Equal([]string{"http://test1.com", "http://test2.com"}))
				Expect(w.targets).To(BeEmpty())
			})
		})

		It("returns n targets", func() {
			w.targets = []string{"http://test1.com", "http://test2.com", "http://test3.com"}
			result := w.shift(2)
			Expect(result).To(Equal([]string{"http://test1.com", "http://test2.com"}))
			Expect(w.targets).To(Equal([]string{"http://test3.com"}))
		})
	})

	Describe("size()", func() {
		It("returns 0 for empty targets", func() {
			w.targets = []string{}
			Expect(w.size()).To(Equal(0))
		})

		It("returns correct size", func() {
			w.targets = []string{"http://test1.com", "http://test2.com", "http://test3.com"}
			Expect(w.size()).To(Equal(3))
		})
	})

	Describe("retrigger()", func() {
		It("appends URL to targets", func() {
			w.targets = []string{"http://test1.com"}
			w.retrigger("http://test2.com")
			Expect(w.targets).To(Equal([]string{"http://test1.com", "http://test2.com"}))
		})
	})

	Describe("checkProxies()", func() {
		var (
			target   *httptest.Server
			proxy    *httptest.Server
			proxyURL *url.URL
		)

		BeforeEach(func() {
			target = mockHTTPServer("")
			proxy, proxyURL = mockProxyServer(0)
		})

		AfterEach(func() {
			target.Close()
			proxy.Close()
		})

		It("should process valid proxy URLs", func() {
			cfg.testTarget = target.URL
			proxies := proxyMap{proxyURL: true}

			checkProxies(proxies)
			srv := <-srvsCh

			Expect(srv.URL).To(Equal(proxyURL))
		})
	})

	Describe("handleServer()", func() {
		var (
			proxy    *httptest.Server
			proxyURL *url.URL
			srv      *Server
			target   *httptest.Server
		)

		BeforeEach(func() {
			proxy, proxyURL = mockProxyServer(50)
			target = mockHTTPServer("good")
			w.targets = []string{target.URL, target.URL, target.URL}

			srv = &Server{URL: proxyURL, Capacity: 1}
			srv.ctx, srv.cancel = context.WithCancel(context.Background())
		})

		AfterEach(func() {
			target.Close()
			proxy.Close()
		})

		It("handles all targets", func() {
			Expect(w.size()).To(Equal(3))

			result := []string{}
			go handleServer(w, srv, func(b []byte) {
				result = append(result, string(b))
			})

			time.Sleep(500 * time.Millisecond) // Give goroutine time to process

			Expect(w.size()).To(Equal(0))
			Expect(result).To(Equal([]string{"good", "good", "good"}))
		})
	})
})

// Helpers

func mockHTTPServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.Write([]byte(body))
	}))
}

func mockProxyServer(delay time.Duration) (*httptest.Server, *url.URL) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, err := http.Get(r.URL.String()) // Forward the request to the target
		if err != nil {
			http.Error(w, "Proxy Error", http.StatusBadGateway)
			return
		}

		time.Sleep(delay * time.Millisecond)

		// Copy the response from the target
		body, _ := io.ReadAll(resp.Body)
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		defer resp.Body.Close()
	}))
	u, _ := url.Parse(s.URL)
	return s, u
}
