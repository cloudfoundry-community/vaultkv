package vaultkv_test

import (
	. "github.com/onsi/ginkgo"
	//	. "github.com/onsi/gomega"
)

var _ = Describe("KVv2", func() {
	if parseSemver(currentVaultVersion).LessThan(semver{0, 10, 0}) {
		kvv2OldTests()
	} else {
		kvv2NewTests()
	}
})

func kvv2OldTests() {

}

func kvv2NewTests() {

}
