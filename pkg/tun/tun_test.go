package tun

import (
	"errors"
	"os"
	"syscall"
	"testing"
)

var tunName string = "unittest"

func newTUN(t *testing.T) *TUN {
	uid, gid := os.Getuid(), os.Getgid()
	tunnel, err := CreateTUN(tunName, 0, uid, gid)
	if err != nil && errors.Is(err, syscall.EPERM) {
		t.Skipf("Permission denied, skipping test (try sudo go test...): %v", err)
	} else if err != nil {
		t.Fatal(err)
	}
	return tunnel
}

func closeTUN(t *testing.T, tunnel *TUN) {
	if err := tunnel.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateTUN(t *testing.T) {
	tunnel := newTUN(t)
	defer closeTUN(t, tunnel)

	if tunnel.Name == "" {
		t.Errorf("TUN.Name is empty, expected a name")
	}
	if tunnel.Name != tunName {
		t.Logf("Warning: expected TUN.Name to be %q, but got %q", tunName, tunnel.Name)
	}
	if tunnel.Fd <= 0 {
		t.Errorf("Expected TUN.Fd to be above 0, but got %d", tunnel.Fd)
	}
	if tunnel.File == nil {
		t.Error("Expected TUN.File to be non-nil")
	}
}

func TestTUN_SetMTU(t *testing.T) {
	tunnel := newTUN(t)
	defer closeTUN(t, tunnel)

	if err := tunnel.SetMTU(1500); err != nil {
		t.Fatal(err)
	}
}

func TestTUN_Close(t *testing.T) {
	tunnel := newTUN(t)
	if err := tunnel.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tunnel.Close(); err == nil {
		t.Fatal("Expected TUN.Close to fail")
	}
}

func TestTUN_ConfigureInterface(t *testing.T) {
	tunnel := newTUN(t)
	defer closeTUN(t, tunnel)
	if err := tunnel.ConfigureInterface("192.168.99.185/29"); err != nil {
		t.Fatal(err)
	}
	if err := tunnel.ConfigureInterface("192.168.99.186/29"); err != nil {
		t.Fatal(err)
	}
	if err := tunnel.ConfigureInterface("192.168.99.256/29"); err == nil {
		t.Fatal("Expected TUN.ConfigureInterface to fail")
	}
}

func TestTUN_LinkUp(t *testing.T) {
	tunnel := newTUN(t)
	defer closeTUN(t, tunnel)
	if err := tunnel.ConfigureInterface("192.168.99.185/29"); err != nil {
		t.Fatal(err)
	}
	if err := tunnel.LinkUp(); err != nil {
		t.Fatal(err)
	}
}
