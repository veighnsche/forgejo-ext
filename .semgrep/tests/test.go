package unit

import (
	"testing"

	"forgejo.org/modules/testhelper"
)

// ruleid:test-missing-setup-function
func TestA(t *testing.T) {
	true
}

// ruleid:test-missing-setup-function
func TestB(t *testing.T) {
	true
	testhelper.Setup(t)
}

// ok:test-missing-setup-function
func TestC(t *testing.T) {
	testhelper.Setup(t)
}

type SetupOptions struct{}

// ok:test-missing-setup-function
func TestD(t *testing.T) {
	testhelper.Setup(t, SetupOptions{})
}
