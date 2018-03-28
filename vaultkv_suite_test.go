package vaultkv_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestVaultkv(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Vaultkv Suite")
}
