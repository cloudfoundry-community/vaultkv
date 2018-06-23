package vaultkv_test

import (
	"fmt"

	"github.com/cloudfoundry-community/vaultkv"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	//. "github.com/cloudfoundry-community/vaultkv"
)

var _ = When("the vault is uninitialized", func() {
	type spec struct {
		Name       string
		Setup      func()
		MinVersion *semver
	}
	Specify("Most commands should return ErrUninitialized", func() {
		for _, s := range []spec{
			spec{"Health", func() { err = vault.Health(true) }, nil},
			spec{"EnableSecretsMount", func() { err = vault.EnableSecretsMount("beep", vaultkv.Mount{}) }, nil},
			spec{"SealStatus", func() { _, err = vault.SealStatus() }, nil},
			spec{"Unseal", func() { _, err = vault.Unseal("pLacEhoLdeR=") }, nil},
			spec{"Get", func() { err = vault.Get("secret/sure/whatever", nil) }, nil},
			spec{"Set", func() { err = vault.Set("secret/sure/whatever", map[string]string{"foo": "bar"}) }, nil},
			spec{"Delete", func() { err = vault.Delete("secret/sure/whatever") }, nil},
			spec{"List", func() { _, err = vault.List("secret/sure/whatever") }, nil},
			spec{"V2Get", func() { _, err = vault.V2Get("secret/foo", nil, nil) }, &semver{0, 10, 0}},
			spec{"V2Set", func() { _, err = vault.V2Set("secret/foo", map[string]string{"beep": "boop"}, nil) }, &semver{0, 10, 0}},
			spec{"V2Delete", func() { err = vault.V2Delete("secret/foo", nil) }, &semver{0, 10, 0}},
			spec{"V2Undelete", func() { err = vault.V2Undelete("secret/foo", []uint{1}) }, &semver{0, 10, 0}},
			spec{"V2Destroy", func() { err = vault.V2Destroy("secret/foo", []uint{1}) }, &semver{0, 10, 0}},
			spec{"V2DestroyMetadata", func() { err = vault.V2DestroyMetadata("secret/foo") }, &semver{0, 10, 0}},
			spec{"V2GetMetadata", func() { _, err = vault.V2GetMetadata("secret/foo") }, &semver{0, 10, 0}},
		} {
			if s.MinVersion != nil && parseSemver(currentVaultVersion).LessThan(*s.MinVersion) {
				continue
			}
			(s.Setup)()
			Expect(err).To(HaveOccurred(),
				fmt.Sprintf("`%s' did not produce an error", s.Name))
			Expect(err).To(BeAssignableToTypeOf(&vaultkv.ErrUninitialized{}),
				fmt.Sprintf("`%s' did not make error of type *ErrUninitialized", s.Name))
		}
	})
})

var _ = When("the vault is initialized", func() {
	type spec struct {
		Name       string
		Setup      func()
		MinVersion *semver
	}

	BeforeEach(func() {
		_, err = vault.InitVault(vaultkv.InitConfig{
			Shares:    1,
			Threshold: 1,
		})
		AssertNoError()()
	})

	When("the vault is sealed", func() {
		Specify("Most commands should return ErrSealed", func() {
			for _, s := range []spec{
				spec{"Health", func() { err = vault.Health(true) }, nil},
				spec{"EnableSecretsMount", func() { err = vault.EnableSecretsMount("beep", vaultkv.Mount{}) }, nil},
				spec{"Get", func() { err = vault.Get("secret/sure/whatever", nil) }, nil},
				spec{"Set", func() { err = vault.Set("secret/sure/whatever", map[string]string{"foo": "bar"}) }, nil},
				spec{"Delete", func() { err = vault.Delete("secret/sure/whatever") }, nil},
				spec{"List", func() { _, err = vault.List("secret/sure/whatever") }, nil},
				spec{"V2Get", func() { _, err = vault.V2Get("secret/foo", nil, nil) }, &semver{0, 10, 0}},
				spec{"V2Set", func() { _, err = vault.V2Set("secret/foo", map[string]string{"beep": "boop"}, nil) }, &semver{0, 10, 0}},
				spec{"V2Delete", func() { err = vault.V2Delete("secret/foo", nil) }, &semver{0, 10, 0}},
				spec{"V2Undelete", func() { err = vault.V2Undelete("secret/foo", []uint{1}) }, &semver{0, 10, 0}},
				spec{"V2Destroy", func() { err = vault.V2Destroy("secret/foo", []uint{1}) }, &semver{0, 10, 0}},
				spec{"V2DestroyMetadata", func() { err = vault.V2DestroyMetadata("secret/foo") }, &semver{0, 10, 0}},
				spec{"V2GetMetadata", func() { _, err = vault.V2GetMetadata("secret/foo") }, &semver{0, 10, 0}},
			} {
				if s.MinVersion != nil && parseSemver(currentVaultVersion).LessThan(*s.MinVersion) {
					continue
				}
				(s.Setup)()
				Expect(err).To(HaveOccurred(),
					fmt.Sprintf("`%s' did not produce an error", s.Name))
				Expect(err).To(BeAssignableToTypeOf(&vaultkv.ErrSealed{}),
					fmt.Sprintf("`%s' did not make error of type *ErrSealed", s.Name))
			}
		})
	})
})
