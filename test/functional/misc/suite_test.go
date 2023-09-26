package misc

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestLogForwarding(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "[Functional][Misc] Suite")
}
