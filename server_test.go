package httptines

import (
	"context"
	"net/url"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Server", func() {
	var (
		serverURL *url.URL
		server    *Server
	)

	BeforeEach(func() {
		serverURL, _ = url.Parse("http://test-server.com")

		ctx, cancel := context.WithCancel(context.Background())
		server = &Server{
			URL:    serverURL,
			ctx:    ctx,
			cancel: cancel,
		}

		// go updateStat()
	})

	Describe("start()", func() {
		It("increments the request counter", func() {
			server.start()
			Expect(server.Requests).To(Equal(1))
		})

		It("returns current time", func() {
			t, _ := server.start()
			Expect(t).To(BeTemporally("~", time.Now(), time.Second))
		})
	})

	Describe("finish()", func() {
		When("successful request", func() {
			It("updates statistics", func() {
				startTime := time.Now().Add(-100 * time.Millisecond)
				server.finish(startTime, nil)

				Expect(server.Requests).To(Equal(-1))
				Expect(server.Positive).To(Equal(1))
				Expect(server.Negative).To(Equal(0))
				Expect(server.Latency).To(BeNumerically("~", 100, 10))
			})
		})

		When("failed request", func() {
			It("updates statistics", func() {
				startTime := time.Now().Add(-100 * time.Millisecond)
				server.finish(startTime, context.Canceled)

				Expect(server.Requests).To(Equal(-1))
				Expect(server.Positive).To(Equal(0))
				Expect(server.Negative).To(Equal(1))
				Expect(server.Latency).To(BeNumerically("~", 100, 10))
			})
		})
	})

	Describe("efficiency()", func() {
		When("no requests", func() {
			It("returns 0", func() {
				server.Positive = 0
				server.Negative = 0
				Expect(server.efficiency()).To(Equal(0.0))
			})
		})

		It("returns efficiency eq 80", func() {
			server.Positive = 80
			server.Negative = 20
			Expect(server.efficiency()).To(Equal(80.0))
		})
	})

	Describe("fiveFailInRow()", func() {
		When("5 consecutive failures", func() {
			It("returns true", func() {
				server.l5 = [5]bool{false, false, false, false, false}
				Expect(server.fiveFailInRow()).To(BeTrue())
			})
		})

		When("less than 5 failures", func() {
			It("return false", func() {
				server.l5 = [5]bool{false, false, false, false, true}
				Expect(server.fiveFailInRow()).To(BeFalse())
			})
		})
	})

	Describe("disable()", func() {
		It("sets disabled flag", func() {
			server.disable()
			Expect(server.Disabled).To(Equal(uint32(1)))
		})
	})

	Describe("toMap()", func() {
		It("should convert server stats to map", func() {
			server.Positive = 10
			server.Negative = 2
			server.Latency = 100
			server.Requests = 3
			server.Capacity = 5

			result := server.toMap()
			Expect(result).To(HaveKeyWithValue("url", server.URL.String()))
			Expect(result).To(HaveKeyWithValue("disabled", uint32(0)))
			Expect(result).To(HaveKeyWithValue("latency", 100))
			Expect(result).To(HaveKeyWithValue("capacity", 5))
			Expect(result).To(HaveKeyWithValue("requests", 3))
			Expect(result).To(HaveKeyWithValue("positive", 10))
			Expect(result).To(HaveKeyWithValue("negative", 2))
			Expect(result).To(HaveKeyWithValue("efficiency", 83.0))
		})
	})
})
