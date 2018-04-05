package vaultkv_test

import (
	"fmt"

	"github.com/cloudfoundry-community/vaultkv"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

//TODO: Add tests for when the token is wrong / missing

var _ = Describe("Sys", func() {
	var vault *vaultkv.Client
	var err error

	var AssertNoError = func() func() {
		return func() {
			Expect(err).NotTo(HaveOccurred())
		}
	}

	var AssertErrorOfType = func(t interface{}) func() {
		return func() {
			Expect(err).To(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(t))
		}
	}

	//Uses SealStatus to get seal state
	var AssertStatusSealed = func(expected bool) func() {
		return func() {
			state, err := vault.SealStatus()
			Expect(err).NotTo(HaveOccurred())
			Expect(state).ToNot(BeNil())
			Expect(state.Sealed).To(Equal(expected))
		}
	}

	BeforeEach(func() {
		StartVault(currentVaultVersion)
		vault = NewTestClient()
	})

	AfterEach(StopVault)

	Describe("SealStatus", func() {
		JustBeforeEach(func() {
			_, err = vault.SealStatus()
		})

		When("Vault is uninitialized", func() {
			It("should return an ErrUninitialized", AssertErrorOfType(&vaultkv.ErrUninitialized{}))
		})
	})

	Describe("Initialization", func() {
		var output *vaultkv.InitVaultOutput
		var input vaultkv.InitVaultInput
		JustBeforeEach(func() {
			output, err = vault.InitVault(input)
		})

		var AssertHasRootToken = func() func() {
			return func() {
				Expect(output).ToNot(BeNil())
				Expect(output.RootToken).ToNot(BeEmpty())
			}
		}

		var AssertHasUnsealKeys = func(numKeys int) func() {
			return func() {
				Expect(output).ToNot(BeNil())
				Expect(output.Keys).To(HaveLen(numKeys))
				Expect(output.KeysBase64).To(HaveLen(numKeys))
			}
		}

		var AssertInitializationStatus = func(expected bool) func() {
			return func() {
				actual, err := vault.IsInitialized()
				Expect(err).NotTo(HaveOccurred())
				Expect(actual).To(Equal(expected))
			}
		}

		When("the Vault is not initialized", func() {
			When("there's only one secret share", func() {
				BeforeEach(func() {
					input = vaultkv.InitVaultInput{
						Shares:    1,
						Threshold: 1,
					}
				})

				It("should not err", AssertNoError())
				It("should return a root token", AssertHasRootToken())
				It("should have one unseal key", AssertHasUnsealKeys(1))
				It("should be initialized", AssertInitializationStatus(true))
			})

			When("there are multiple secret shares", func() {
				BeforeEach(func() {
					input = vaultkv.InitVaultInput{
						Shares:    3,
						Threshold: 2,
					}
				})

				It("should not err", AssertNoError())
				It("should return a root token", AssertHasRootToken())
				It("should have three unseal keys", AssertHasUnsealKeys(3))
				It("should be initialized", AssertInitializationStatus(true))
			})

			When("0 secret shares are requested", func() {
				BeforeEach(func() {
					input = vaultkv.InitVaultInput{
						Shares:    0,
						Threshold: 0,
					}

					It("should return an ErrBadRequest", AssertErrorOfType(&vaultkv.ErrBadRequest{}))
					It("should be initialized", AssertInitializationStatus(false))
				})
			})

			When("the threshold is larger than the number of shares", func() {
				BeforeEach(func() {
					input = vaultkv.InitVaultInput{
						Shares:    3,
						Threshold: 4,
					}

					It("should return an ErrBadRequest", AssertErrorOfType(&vaultkv.ErrBadRequest{}))
					It("should be initialized", AssertInitializationStatus(false))
				})
			})
		})

		When("the Vault has already been initialized", func() {
			BeforeEach(func() {
				_, err = vault.InitVault(vaultkv.InitVaultInput{
					Shares:    1,
					Threshold: 1,
				})
				Expect(err).NotTo(HaveOccurred())
			})

			When("an otherwise legitimate init request is made", func() {
				BeforeEach(func() {
					input = vaultkv.InitVaultInput{
						Shares:    1,
						Threshold: 1,
					}
				})

				It("should return an ErrBadRequest", AssertErrorOfType(&vaultkv.ErrBadRequest{}))
				It("should be initialized", AssertInitializationStatus(true))
			})
		})
	})

	Describe("Unseal", func() {
		var output *vaultkv.SealState
		var unsealKey string

		BeforeEach(func() {
			unsealKey = "pLacEhoLdeR="
		})
		JustBeforeEach(func() {
			output, err = vault.Unseal(unsealKey)
		})

		var AssertSealed = func(expected bool) func() {
			return func() {
				Expect(output).ToNot(BeNil())
				Expect(output.Sealed).To(Equal(expected))
			}
		}

		var AssertProgressIs = func(expected int) func() {
			return func() {
				state, err := vault.SealStatus()
				Expect(err).NotTo(HaveOccurred())
				Expect(state.Progress).To(Equal(expected))
			}
		}

		When("the vault is uninitialized", func() {
			It("should return an ErrUninitialized", AssertErrorOfType(&vaultkv.ErrUninitialized{}))
		})

		When("the vault is initialized", func() {
			var initOut *vaultkv.InitVaultOutput
			Context("with one share", func() {
				BeforeEach(func() {
					initOut, err = vault.InitVault(vaultkv.InitVaultInput{
						Shares:    1,
						Threshold: 1,
					})
				})

				When("unseal key is correct", func() {
					BeforeEach(func() {
						unsealKey = initOut.Keys[0]
					})

					It("should not return an error", AssertNoError())
					Specify("Unseal should return that Vault is unsealed", AssertSealed(false))
					Specify("SealStatus should return that the Vault is unsealed", AssertStatusSealed(false))
				})

				When("the unseal key is wrong", func() {
					BeforeEach(func() {
						unsealKey = initOut.Keys[0]
						replacementChar := "a"
						if unsealKey[0] == 'a' {
							replacementChar = "b"
						}

						unsealKey = fmt.Sprintf("%s%s", replacementChar, unsealKey[1:])
					})

					It("should return an ErrBadRequest", AssertErrorOfType(&vaultkv.ErrBadRequest{}))
					Specify("SealStatus should return that the Vault is still sealed", AssertStatusSealed(true))
				})

				When("the unseal key is improperly formatted", func() {
					It("should return an ErrBadRequest", AssertErrorOfType(&vaultkv.ErrBadRequest{}))
					Specify("SealStatus should return that the Vault is still sealed", AssertStatusSealed(true))
				})
			})

			Context("with a threshold greater than one", func() {
				BeforeEach(func() {
					initOut, err = vault.InitVault(vaultkv.InitVaultInput{
						Shares:    3,
						Threshold: 3,
					})
				})

				When("the unseal key is improperly formatted", func() {
					It("should return an ErrBadRequest", AssertErrorOfType(&vaultkv.ErrBadRequest{}))
					It("should not have increased the progress count", AssertProgressIs(0))
					Specify("SealStatus should return that the Vault is still sealed", AssertStatusSealed(true))
				})

				When("the unseal key is correct", func() {
					BeforeEach(func() {
						unsealKey = initOut.Keys[0]
					})

					It("should not return an error", AssertNoError())
					It("should increase the progress count", AssertProgressIs(1))
					Specify("Unseal should return that the vault is still sealed", AssertSealed(true))
					Specify("SealStatus should return that the Vault is still sealed", AssertStatusSealed(true))
				})
			})
		})
	})

	Describe("Seal", func() {
		JustBeforeEach(func() {
			err = vault.Seal()
		})

		When("the vault is not initialized", func() {
			It("should not return an error", AssertNoError())
		})

		When("the vault is initialized", func() {
			var initOut *vaultkv.InitVaultOutput
			BeforeEach(func() {
				initOut, err = vault.InitVault(vaultkv.InitVaultInput{
					Shares:    1,
					Threshold: 1,
				})
			})
			When("the vault is already sealed", func() {
				It("should not return an error", AssertNoError())
				Specify("The vault should be sealed", AssertStatusSealed(true))
			})

			When("the vault is unsealed", func() {
				BeforeEach(func() {
					sealState, err := vault.Unseal(initOut.Keys[0])
					Expect(err).NotTo(HaveOccurred())
					Expect(sealState).NotTo(BeNil())
					Expect(sealState.Sealed).To(BeFalse())
				})
				It("should not return an error", AssertNoError())
				Specify("The vault should be sealed", AssertStatusSealed(true))

				Context("but the user is not authenticated", func() {
					BeforeEach(func() {
						vault.AuthToken = ""
					})

					It("should return ErrForbidden", AssertErrorOfType(&vaultkv.ErrForbidden{}))
					Specify("The vault should remain unsealed", AssertStatusSealed(false))
				})
			})

		})
	})

	Describe("Health", func() {
		JustBeforeEach(func() {
			err = vault.Health(true)
		})

		When("the vault is not initialized", func() {
			It("should return ErrUninitialized", AssertErrorOfType(&vaultkv.ErrUninitialized{}))
		})

		When("the vault is initialized", func() {
			var initOut *vaultkv.InitVaultOutput
			BeforeEach(func() {
				initOut, err = vault.InitVault(vaultkv.InitVaultInput{
					Shares:    1,
					Threshold: 1,
				})
			})

			When("the vault is sealed", func() {
				It("should return ErrSealed", AssertErrorOfType(&vaultkv.ErrSealed{}))
			})

			When("the vault is unsealed", func() {
				BeforeEach(func() {
					sealState, err := vault.Unseal(initOut.Keys[0])
					Expect(err).NotTo(HaveOccurred())
					Expect(sealState).NotTo(BeNil())
					Expect(sealState.Sealed).To(BeFalse())
				})

				It("should not return an error", AssertNoError())
			})
		})
	})
})
