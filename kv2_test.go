package vaultkv_test

import (
	. "github.com/onsi/ginkgo"
	//. "github.com/onsi/gomega"
	//. "github.com/cloudfoundry-community/vaultkv"
)

var _ = Describe("KVv2", func() {
	JustBeforeEach(func() {
		if parseSemver(currentVaultVersion).LessThan(semver{0, 10, 0}) {
			Skip("This version of Vault does not support KVv2")
		}
	})
})
