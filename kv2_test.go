package vaultkv_test

import (
	"fmt"
	"time"

	"github.com/cloudfoundry-community/vaultkv"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("KVv2", func() {
	const testMountName = "beep"
	BeforeEach(func() {
		if parseSemver(currentVaultVersion).LessThan(semver{0, 10, 0}) {
			Skip("This version of Vault does not support KVv2")
		} else {
			InitAndUnsealVault()
			err = vault.EnableSecretsMount(testMountName, vaultkv.Mount{
				Type:    vaultkv.MountTypeKV,
				Options: vaultkv.KVMountOptions{}.WithVersion(2),
			})

			AssertNoError()()
		}
	})

	Describe("V2Set", func() {
		var testSetPath string
		var testSetValues map[string]interface{}
		var testSetOptions *vaultkv.V2SetOpts
		var testVersionOutput vaultkv.V2Version
		BeforeEach(func() {
			testSetPath = fmt.Sprintf("%s/boop", testMountName)
		})

		JustBeforeEach(func() {
			testVersionOutput, err = vault.V2Set(testSetPath, testSetValues, testSetOptions)
		})

		AfterEach(func() {
			testSetValues = nil
			testSetOptions = nil
			testVersionOutput = vaultkv.V2Version{}
		})

		Context("With a nil input", func() {
			BeforeEach(func() {
				testSetValues = nil
			})

			It("should write nil to the key", func() {
				By("not erroring")
				Expect(err).NotTo(HaveOccurred())

				By("returning the proper version output")
				Expect(testVersionOutput.Version).To(BeEquivalentTo(1))
			})

			Describe("V2Get", func() {
				When("outputting into a pointer", func() {
					var testGetOutput *map[string]interface{}
					JustBeforeEach(func() {
						testGetOutput = &map[string]interface{}{}
						_, err = vault.V2Get(testSetPath, &testGetOutput, nil)
					})

					It("should populate the pointer properly", func() {
						By("not erroring")
						Expect(err).NotTo(HaveOccurred())

						By("setting the pointer to nil")
						Expect(testGetOutput).To(BeNil())
					})
				})

				When("outputting into a map", func() {
					var testGetOutput map[string]interface{}
					JustBeforeEach(func() {
						testGetOutput = map[string]interface{}{}
						_, err = vault.V2Get(testSetPath, &testGetOutput, nil)
					})

					It("should populate the map properly", func() {
						By("not erroring")
						Expect(err).NotTo(HaveOccurred())

						By("leaving the map empty")
						Expect(testGetOutput).To(BeEmpty())
					})
				})
			})
		})

		Context("With a non-empty map input", func() {
			BeforeEach(func() {
				testSetValues = map[string]interface{}{"foo": "bar"}
			})

			It("should write the proper values to the key", func() {
				By("not erroring")
				Expect(err).NotTo(HaveOccurred())

				By("returning the proper version output")
				Expect(testVersionOutput.Version).To(BeEquivalentTo(1))
			})

			Describe("V2Get", func() {
				var testGetOutput map[string]interface{}
				var testGetVersionOutput vaultkv.V2Version
				JustBeforeEach(func() {
					testGetOutput = map[string]interface{}{}
					testGetVersionOutput, err = vault.V2Get(testSetPath, &testGetOutput, nil)
				})

				It("should get the undeleted key", func() {
					By("not erroring")
					Expect(err).NotTo(HaveOccurred())

					By("returning the same version info as the Set")
					Expect(testGetVersionOutput).To(Equal(testVersionOutput))

					By("returning the same values that were set")
					Expect(testGetOutput).To(Equal(testSetValues))
				})
			})

			Describe("V2Delete", func() {
				var testDeleteOptions *vaultkv.V2DeleteOpts
				JustBeforeEach(func() {
					err = vault.V2Delete(testSetPath, testDeleteOptions)
				})
				Context("Not specifying a version to delete", func() {
					BeforeEach(func() {
						testDeleteOptions = nil
					})

					It("should not err", AssertNoError())

					Describe("V2Get", func() {
						JustBeforeEach(func() {
							_, err = vault.V2Get(testSetPath, nil, nil)
						})

						It("should return ErrNotFound", AssertErrorOfType(&vaultkv.ErrNotFound{}))
					})

					Describe("V2GetMetadata", func() {
						var testMetadataOutput vaultkv.V2Metadata
						JustBeforeEach(func() {
							testMetadataOutput, err = vault.V2GetMetadata(testSetPath)
						})

						It("should return metadata reflecting the delete", func() {
							By("not erroring")
							Expect(err).NotTo(HaveOccurred())

							By("having the latest version be 1")
							Expect(testMetadataOutput.CurrentVersion).To(BeEquivalentTo(1))

							By("marking creation as at the correct time")
							Expect(time.Since(testMetadataOutput.CreatedAt) < time.Second*5).To(BeTrue())

							By("having the correct number of versions")
							Expect(testMetadataOutput.Versions).To(HaveLen(1))

							By("having a version numbered '1'")
							version, versionErr := testMetadataOutput.Version(1)
							Expect(versionErr).NotTo(HaveOccurred())

							By("marking version 1 as having been deleted")
							Expect(version.DeletedAt).ToNot(BeNil())

							By("marking version deletion as at the correct time")
							Expect(time.Since(*version.DeletedAt) < time.Second*5).To(BeTrue())

							By("marking version creation as at the correct time")
							Expect(time.Since(version.CreatedAt) < time.Second*5).To(BeTrue())
						})
					})

					Describe("V2Undelete", func() {
						JustBeforeEach(func() {
							err = vault.V2Undelete(testSetPath, []uint{testVersionOutput.Version})
						})

						It("should undelete the key", func() {
							By("not erroring")
							AssertNoError()()

							By("V2Get finding the undeleted key")
							testGetOutput := map[string]interface{}{}
							var testGetVersionOutput vaultkv.V2Version
							testGetVersionOutput, err = vault.V2Get(testSetPath, &testGetOutput, nil)
							AssertNoError()()

							By("V2Get returning the V2Set's original version info")
							Expect(testGetVersionOutput).To(Equal(testVersionOutput))

							By("V2Get returning the originally set values")
							Expect(testGetOutput).To(Equal(testSetValues))
						})

						Describe("V2GetMetadata", func() {
							var testMetadataOutput vaultkv.V2Metadata
							JustBeforeEach(func() {
								testMetadataOutput, err = vault.V2GetMetadata(testSetPath)
							})

							It("should return metadata reflecting the undelete", func() {
								By("not erroring")
								Expect(err).NotTo(HaveOccurred())

								By("having the current version be 1")
								Expect(testMetadataOutput.CurrentVersion).To(BeEquivalentTo(1))

								By("marking creation as at the correct time")
								Expect(time.Since(testMetadataOutput.CreatedAt) < time.Second*5).To(BeTrue())

								By("having the correct number of versions")
								Expect(testMetadataOutput.Versions).To(HaveLen(1))

								By("having a version numbered '1'")
								version, versionErr := testMetadataOutput.Version(1)
								Expect(versionErr).NotTo(HaveOccurred())

								By("having version 1 not marked as deleted")
								Expect(version.DeletedAt).To(BeNil())

								By("marking version creation as at the correct time")
								Expect(time.Since(version.CreatedAt) < time.Second*5).To(BeTrue())
							})
						})
					})

				})

				Context("Specifying a version to delete", func() {
					When("the version exists", func() {
						BeforeEach(func() {
							testDeleteOptions = &vaultkv.V2DeleteOpts{
								Versions: []uint{1},
							}
						})

						It("should delete the specified version", func() {
							By("not erroring")
							AssertNoError()()

							By("V2Get being unable to find it")
							_, err = vault.V2Get(testSetPath, nil, nil)
							AssertErrorOfType(&vaultkv.ErrNotFound{})()
						})

						Context("and then deleting it again", func() {
							JustBeforeEach(func() {
								err = vault.V2Delete(testSetPath, testDeleteOptions)
							})

							It("should not err", AssertNoError())
						})
					})

					When("the version does not exist", func() {
						BeforeEach(func() {
							testDeleteOptions = &vaultkv.V2DeleteOpts{
								Versions: []uint{12},
							}
						})

						It("should not err", AssertNoError())
					})
				})
			})

			Describe("V2Destroy", func() {
				When("the version exists and it is the only version", func() {
					JustBeforeEach(func() {
						err = vault.V2Destroy(testSetPath, []uint{1})
					})

					It("should delete the metadata", func() {
						By("not erroring")
						AssertNoError()()

						By("V2Get being unable to find the key")
						_, err = vault.V2Get(testSetPath, nil, nil)
						AssertErrorOfType(&vaultkv.ErrNotFound{})

						By("V2GetMetadata being unable to find the key")
						_, err = vault.V2GetMetadata(testSetPath)
						AssertErrorOfType(&vaultkv.ErrNotFound{})
					})
				})

				When("the version does not exist", func() {
					JustBeforeEach(func() {
						err = vault.V2Destroy(testSetPath, []uint{12})
					})

					It("should not delete anything", func() {
						By("not erroring")
						AssertNoError()()

						By("V2Get being able to find the key")
						_, err = vault.V2Get(testSetPath, nil, nil)
						AssertNoError()()

						By("V2GetMetadata being able to find the key")
						var meta vaultkv.V2Metadata
						meta, err = vault.V2GetMetadata(testSetPath)
						AssertNoError()()

						By("V2GetMetadata reporting that version 1 still exists")
						_, err = meta.Version(1)
						AssertNoError()()
					})
				})

				When("the path does not exist", func() {
					JustBeforeEach(func() {
						err = vault.V2Destroy(testSetPath+"abcd", []uint{12})
					})

					It("should not err", AssertNoError())
				})
			})

			Describe("V2DestroyMetadata", func() {
				JustBeforeEach(func() {
					err = vault.V2DestroyMetadata(testSetPath)
				})

				It("should delete the metadata", func() {
					By("not erroring")
					AssertNoError()()

					By("V2Get being unable to find the key")
					_, err = vault.V2Get(testSetPath, nil, nil)
					AssertErrorOfType(&vaultkv.ErrNotFound{})

					By("V2GetMetadata being unable to find the key")
					_, err = vault.V2GetMetadata(testSetPath)
					AssertErrorOfType(&vaultkv.ErrNotFound{})
				})
			})

			Context("When there are two versions written", func() {
				var testSet2Values map[string]interface{}
				BeforeEach(func() {
					testSet2Values = map[string]interface{}{"wee": "woo"}
				})
				JustBeforeEach(func() {
					testVersionOutput, err = vault.V2Set(testSetPath, testSet2Values, nil)
					AssertNoError()()
				})

				Describe("V2Get", func() {
					var testGet2Options *vaultkv.V2GetOpts
					var testGet2Output map[string]interface{}
					var testGet2Version vaultkv.V2Version
					JustBeforeEach(func() {
						testGet2Output = map[string]interface{}{}
						testGet2Version, err = vault.V2Get(testSetPath, &testGet2Output, testGet2Options)
					})

					When("there are no options specified", func() {
						BeforeEach(func() {
							testGet2Options = nil
						})

						It("should get the latest version", func() {
							By("not erroring")
							AssertNoError()()

							By("having the retrieved value match what was put in second")
							Expect(testGet2Output).To(BeEquivalentTo(testSet2Values))

							By("having the returned version be 2")
							Expect(testGet2Version.Version).To(BeEquivalentTo(2))
						})
					})

					When("the version specified is `0'", func() {
						BeforeEach(func() {
							testGet2Options = &vaultkv.V2GetOpts{Version: 0}
						})

						It("should get the latest version", func() {
							By("not erroring")
							AssertNoError()()

							By("having the retrieved value match what was put in second")
							Expect(testGet2Output).To(BeEquivalentTo(testSet2Values))

							By("having the returned version be 2")
							Expect(testGet2Version.Version).To(BeEquivalentTo(2))
						})
					})

					When("the version specified is `1'", func() {
						BeforeEach(func() {
							testGet2Options = &vaultkv.V2GetOpts{Version: 1}
						})
						It("should get version 1", func() {
							By("not erroring")
							AssertNoError()()

							By("having the retrieved value match what was put in first")
							Expect(testGet2Output).To(BeEquivalentTo(testSetValues))

							By("having the returned version be 1")
							Expect(testGet2Version.Version).To(BeEquivalentTo(1))
						})
					})

					When("the version specified is `12'", func() {
						BeforeEach(func() {
							testGet2Options = &vaultkv.V2GetOpts{Version: 12}
						})
						It("should err properly", func() {
							By("return ErrNotFound")
							AssertErrorOfType(&vaultkv.ErrNotFound{})
						})
					})
				})
			})
		})

		When("Check and Set is set to 0", func() {
			BeforeEach(func() {
				testSetOptions = vaultkv.V2SetOpts{}.WithCAS(0)
			})
			Context("and the key does not yet exist", func() {
				It("should write the values", func() {
					By("not erroring")
					AssertNoError()()

					By("returning proper metadata")
					Expect(testVersionOutput.Version).To(BeEquivalentTo(1))
				})
			})

			Context("and the key already exists", func() {
				BeforeEach(func() {
					var meta vaultkv.V2Version
					meta, err = vault.V2Set(testSetPath, testSetValues, nil)
					AssertNoError()()
					Expect(meta.Version).To(BeEquivalentTo(1))
				})

				It("should return ErrBadRequest", AssertErrorOfType(&vaultkv.ErrBadRequest{}))
			})
		})
	})
})
