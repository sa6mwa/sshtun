package sshtun

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"testing"
)

func TestNewSecureShellTunneler(t *testing.T) {
	tn := NewSecureShellTunneler(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if tn == nil {
		t.Fatal("Expected function to return non-nil pointer")
	}
}

func TestLoadConfig(t *testing.T) {
	cnf, err := newConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(cnf)
	_, err = LoadConfig(cnf, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfigOrReturnDefault(t *testing.T) {
	cnf := LoadConfigOrReturnDefault("", nil)
	if cnf == nil {
		t.Fatal("Expected non-nil Tunnels config")
	}
}

func TestDefaultConfig(t *testing.T) {
	cnf := DefaultConfig(nil)
	if cnf == nil {
		t.Fatal("Expected non-nil Tunnels config")
	}
}

func TestLoadAndSave(t *testing.T) {
	cnf, err := newConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(cnf)
	_, err = LoadAndSave(cnf, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestTunnels_SaveConfig(t *testing.T) {
	cnf, err := newConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(cnf)
	t1, err := LoadConfig(cnf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := t1.SaveConfig(cnf); err != nil {
		t.Fatal(err)
	}
	t2, err := LoadConfig(cnf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := compareTunnels(t1, t2); err != nil {
		t.Error(err)
	}
}

func compareTunnels(t1 *Tunnels, t2 *Tunnels) error {
	if t1 == nil || t2 == nil {
		return errors.New("nil tunnels provided")
	}
	if t1.Tunnels == nil || t2.Tunnels == nil {
		return errors.New("nil tunnel configs")
	}
	if e, g := len(t1.Tunnels), len(t2.Tunnels); e != g {
		return fmt.Errorf("t1 contains %d tunnel(s), but t2 contains %d tunnel(s)", e, g)
	}
	for i := range t1.Tunnels {
		v1 := reflect.ValueOf(*t1.Tunnels[i])
		v2 := reflect.ValueOf(*t2.Tunnels[i])
		for i := 0; i < v1.NumField(); i++ {
			switch v1.Type().Field(i).Type.Name() {
			case "string", "int", "bool":
				n1 := v1.Type().Field(i).Name
				n2 := v2.Type().Field(i).Name
				if n1 != n2 {
					return fmt.Errorf("%s does not match %s", n2, n1)
				}
				if v1.Field(i).CanInterface() {
					if expected, got := v1.Field(i).Interface(), v2.Field(i).Interface(); expected != got {
						return fmt.Errorf("expected %v, but got %v for field name %s", expected, got, n1)
					}
				}
			}
		}
	}
	return nil
}

func tunnels() *Tunnels {
	tn := NewSecureShellTunneler(nil)
	return &Tunnels{
		Tunnels: []*SSHTUN{tn},
	}
}

func newConfigFile() (string, error) {
	f, err := os.CreateTemp("", "sshtun-config-unit-test-*")
	if err != nil {
		return "", err
	}
	keepFile := false
	defer func() {
		f.Close()
		if !keepFile {
			os.Remove(f.Name())
		}
	}()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "\t")
	if err := enc.Encode(tunnels()); err != nil {
		return "", err
	}
	keepFile = true
	return f.Name(), nil
}
