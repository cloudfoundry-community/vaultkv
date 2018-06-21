.PHONY: test testlatest

test:
	ginkgo -noisySkippings=false

testlatest:
	VAULTKV_TEST_ONLY_LATEST=true ginkgo -noisySkippings=false
