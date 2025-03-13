package httptines

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("UserAgent", func() {
	Describe("get()", func() {
		It("returns a non-empty user agent string", func() {
			result := ua.get()
			Expect(result).To(Not(BeEmpty()))
		})

		It("returns a string from the predefined list", func() {
			result := ua.get()
			Expect(ua.agents).To(ContainElement(result))
		})

		It("returns different user agents on multiple calls", func() {
			first := ua.get()
			second := ua.get()
			third := ua.get()

			Expect(first == second && second == third && first == third).To(BeFalse())
		})
	})
})
