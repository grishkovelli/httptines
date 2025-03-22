package httptines

import (
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Stat", func() {
	var w *Worker

	BeforeEach(func() {
		w = &Worker{
			stat: &Stat{
				Targets: 100,
				Servers: map[string]srvMap{},
			},
			stsCh: make(chan srvMap),
			timCh: make(chan time.Time),
		}
	})

	Describe("addServer()", func() {
		It("adds server statistics", func() {
			serverData := srvMap{
				"url":      "http://test-server.com",
				"latency":  100,
				"requests": 10,
			}
			w.stat.addServer(serverData)
			Expect(w.stat.Servers).To(HaveKeyWithValue("http://test-server.com", serverData))
		})

		It("updates existing server statistics", func() {
			initialData := srvMap{
				"url":      "http://test-server.com",
				"latency":  100,
				"requests": 10,
			}
			w.stat.addServer(initialData)

			updatedData := srvMap{
				"url":      "http://test-server.com",
				"latency":  200,
				"requests": 20,
			}
			w.stat.addServer(updatedData)

			Expect(w.stat.Servers).To(HaveKeyWithValue("http://test-server.com", updatedData))
		})
	})

	Describe("addTimestamp()", func() {
		It("adds timestamp to the list", func() {
			testTime := time.Now()
			w.stat.addTimestamp(testTime)
			Expect(w.stat.timestamps).To(ContainElement(testTime))
		})

		It("maintains order of timestamps", func() {
			time1 := time.Now().Add(2 * time.Second)
			time2 := time.Now().Add(time.Second)
			time3 := time.Now()

			w.stat.addTimestamp(time1)
			w.stat.addTimestamp(time2)
			w.stat.addTimestamp(time3)

			Expect(w.stat.timestamps).To(Equal([]time.Time{time1, time2, time3}))
		})
	})

	Describe("rpm()", func() {
		When("no timestamps", func() {
			It("returns 0", func() {
				Expect(w.stat.rpm()).To(Equal(0))
			})
		})

		It("returns count requests within last minute", func() {
			now := time.Now()
			w.stat.addTimestamp(now.Add(-2 * time.Minute))
			w.stat.addTimestamp(now.Add(-30 * time.Second))
			w.stat.addTimestamp(now)

			Expect(w.stat.rpm()).To(Equal(2))
		})
	})

	Describe("MarshalJSON()", func() {
		It("marshals statistics to JSON", func() {
			now := time.Now()
			w.stat.addTimestamp(now.Add(-30 * time.Second))
			w.stat.addTimestamp(now)
			w.stat.addServer(srvMap{
				"url": "http://test-server.com",
			})

			data, err := json.Marshal(w.stat)
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
			go w.updateStat()
		})

		It("adds server to stat", func() {
			serverData := srvMap{
				"url": "http://test-server.com",
			}

			w.stsCh <- serverData

			// Give goroutine time to process
			time.Sleep(200 * time.Millisecond)
			Expect(w.stat.Servers).To(HaveKeyWithValue("http://test-server.com", serverData))
		})

		It("adds timestamp to stat", func() {
			testTime := time.Now()

			w.timCh <- testTime

			// Give goroutine time to process
			time.Sleep(200 * time.Millisecond)
			Expect(w.stat.timestamps).To(ContainElement(testTime))
		})
	})
})
