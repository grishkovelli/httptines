package httptines

import (
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Stat", func() {
	BeforeEach(func() {
		stat = &Stat{
			Targets: 100,
			Servers: make(map[string]any),
		}
	})

	Describe("addServer()", func() {
		It("adds server statistics", func() {
			serverData := map[string]any{
				"url":      "http://test-server.com",
				"latency":  100,
				"requests": 10,
			}
			stat.addServer(serverData)
			Expect(stat.Servers).To(HaveKeyWithValue("http://test-server.com", serverData))
		})

		It("updates existing server statistics", func() {
			initialData := map[string]any{
				"url":      "http://test-server.com",
				"latency":  100,
				"requests": 10,
			}
			stat.addServer(initialData)

			updatedData := map[string]any{
				"url":      "http://test-server.com",
				"latency":  200,
				"requests": 20,
			}
			stat.addServer(updatedData)

			Expect(stat.Servers).To(HaveKeyWithValue("http://test-server.com", updatedData))
		})
	})

	Describe("removeServer()", func() {
		It("removes existing server", func() {
			serverData := map[string]any{
				"url": "http://test-server.com",
			}
			stat.addServer(serverData)
			stat.removeServer("http://test-server.com")
			Expect(stat.Servers).NotTo(HaveKey("http://test-server.com"))
		})
	})

	Describe("addTimestamp()", func() {
		It("adds timestamp to the list", func() {
			testTime := time.Now()
			stat.addTimestamp(testTime)
			Expect(stat.timestamps).To(ContainElement(testTime))
		})

		It("maintains order of timestamps", func() {
			time1 := time.Now().Add(2 * time.Second)
			time2 := time.Now().Add(time.Second)
			time3 := time.Now()

			stat.addTimestamp(time1)
			stat.addTimestamp(time2)
			stat.addTimestamp(time3)

			Expect(stat.timestamps).To(Equal([]time.Time{time1, time2, time3}))
		})
	})

	Describe("rpm()", func() {
		When("no timestamps", func() {
			It("returns 0", func() {
				Expect(stat.rpm()).To(Equal(0))
			})
		})

		It("returns count requests within last minute", func() {
			now := time.Now()
			stat.addTimestamp(now.Add(-2 * time.Minute))
			stat.addTimestamp(now.Add(-30 * time.Second))
			stat.addTimestamp(now)

			Expect(stat.rpm()).To(Equal(2))
		})
	})

	Describe("MarshalJSON()", func() {
		It("marshals statistics to JSON", func() {
			now := time.Now()
			stat.addTimestamp(now.Add(-30 * time.Second))
			stat.addTimestamp(now)
			stat.addServer(map[string]any{
				"url": "http://test-server.com",
			})

			data, err := json.Marshal(stat)
			Expect(err).NotTo(HaveOccurred())

			var result map[string]interface{}
			err = json.Unmarshal(data, &result)
			Expect(err).NotTo(HaveOccurred())

			Expect(result).To(HaveKeyWithValue("targets", float64(100)))
			Expect(result).To(HaveKeyWithValue("rpm", float64(2)))
			Expect(result).To(HaveKeyWithValue("processed", float64(2)))
			Expect(result).To(HaveKey("servers"))
		})
	})

	Describe("updateStat()", func() {
		BeforeEach(func() {
			go updateStat()
		})

		It("adds server to stat", func() {
			serverData := map[string]any{
				"url": "http://test-server.com",
			}

			stsCh <- serverData

			// Give goroutine time to process
			time.Sleep(100 * time.Millisecond)
			Expect(stat.Servers).To(HaveKeyWithValue("http://test-server.com", serverData))
		})

		It("adds timestamp to stat", func() {
			testTime := time.Now()

			timCh <- testTime

			// Give goroutine time to process
			time.Sleep(100 * time.Millisecond)
			Expect(stat.timestamps).To(ContainElement(testTime))
		})
	})
})
