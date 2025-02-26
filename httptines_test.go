package httptines

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestHttptines(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "httptines")
}

var _ = Describe("Worker", func() {
	var worker *Worker

	Describe("userAgent", func() {
		Context("with a single user agent", func() {
			BeforeEach(func() {
				worker = &Worker{
					UserAgents: []string{"Googlebot"},
				}
			})

			It("should return the configured user agent", func() {
				Expect(worker.userAgent()).To(Equal("Googlebot"))
			})
		})

		Context("with multiple user agents", func() {
			BeforeEach(func() {
				worker = &Worker{
					UserAgents: []string{"Googlebot", "Bingbot", "DuckDuckBot"},
				}
			})

			It("should return one of the configured user agents", func() {
				agent := worker.userAgent()
				Expect(worker.UserAgents).To(ContainElement(agent))
			})
		})
	})

	Describe("randTimeSleep", func() {
		BeforeEach(func() {
			worker = &Worker{
				MinTimeSleep: 10,
				MaxTimeSleep: 30,
			}
		})

		It("should return duration within configured bounds", func() {
			for i := 0; i < 100; i++ {
				duration := worker.randTimeSleep()
				Expect(duration).To(And(
					BeNumerically(">=", 10*time.Millisecond),
					BeNumerically("<=", 30*time.Millisecond),
				))
			}
		})
	})
})

var _ = Describe("Stat", func() {
	var stat *Stat

	Describe("markWaiting", func() {
		BeforeEach(func() {
			stat = &Stat{}
		})

		Context("when marking a new target", func() {
			It("should add the target to waiting list", func() {
				stat.markWaiting("http://example.com")
				Expect(stat.waiting).To(ContainElement("http://example.com"))
				Expect(stat.Waiting).To(Equal(1))
			})
		})

		Context("when marking a duplicate target", func() {
			BeforeEach(func() {
				stat.markWaiting("http://example.com")
			})

			It("should not add duplicate entries", func() {
				stat.markWaiting("http://example.com")
				Expect(stat.waiting).To(HaveLen(1))
				Expect(stat.Waiting).To(Equal(1))
			})
		})
	})

	Describe("unmarkWaiting", func() {
		BeforeEach(func() {
			stat = &Stat{}
			stat.markWaiting("http://example.com")
			stat.markWaiting("http://example.org")
		})

		Context("when unmarking an existing target", func() {
			It("should remove the target from waiting list", func() {
				stat.unmarkWaiting("http://example.com")
				Expect(stat.waiting).NotTo(ContainElement("http://example.com"))
				Expect(stat.waiting).To(ContainElement("http://example.org"))
				Expect(stat.Waiting).To(Equal(1))
			})
		})

		Context("when unmarking a non-existing target", func() {
			It("should not modify the waiting list", func() {
				stat.unmarkWaiting("http://nonexistent.com")
				Expect(stat.waiting).To(HaveLen(2))
				Expect(stat.Waiting).To(Equal(2))
			})
		})
	})
})
