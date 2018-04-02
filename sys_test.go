package vaultkv_test

import (
	. "github.com/cloudfoundry-community/vaultkv"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Sys", func() {
	var vault *Client
	var err error

	BeforeEach(func() {
		StartVault()
		vault = NewTestClient()
	})
	AfterEach(func() {
		StopVault()
	})

	Context("When Vault is uninitialized", func() {
		Describe("SealStatus", func() {
			JustBeforeEach(func() {
				_, err = vault.SealStatus()
			})

			It("should return an ErrUninitialized", func() {
				Expect(err).To(BeAssignableToTypeOf(&ErrUninitialized{}))
			})
		})
	})
})
