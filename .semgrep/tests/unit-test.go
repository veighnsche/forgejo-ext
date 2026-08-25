package unit

import (
	"testing"

	"forgejo.org/modules/testhelper"
)

// ruleid:unit-test-missing-setup-function
func TestA(t *testing.T) {
	true
}

// ok:unit-test-missing-setup-function
func TestB(t *testing.T) {
	testhelper.Setup(t)
}

// ok:unit-test-missing-setup-function
func TestC(t *testing.T) {
	type SetupOptions struct{}
	options := SetupOptions{}
	testhelper.Setup(t, options)
}
