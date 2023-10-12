package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
)

var defaultSystemdUnit string = `[Unit]
Description=sshtun
After=network.target

[Service]
ExecStart=%s
Restart=on-failure
RestartSec=5s
WorkingDirectory=/tmp
StandardOutput=journal
StandardError=journal
User=%s
Group=%s

[Install]
WantedBy=multi-user.target
`

func InstallSystemdUnit(ctx context.Context, pth string) ([]byte, error) {
	origEUID := syscall.Geteuid()
	if origEUID != 0 {
		if err := syscall.Seteuid(0); err != nil {
			return nil, fmt.Errorf("unable to seteuid 0: %w", err)
		}
		defer func() {
			syscall.Seteuid(origEUID)
		}()
	}
	if out, err := exec.CommandContext(ctx, systemctl, "enable", filepath.Base(systemdUnit)).CombinedOutput(); err != nil {
		if out != nil && len(out) > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil, err
	}
	if out, err := exec.CommandContext(ctx, systemctl, "restart", filepath.Base(systemdUnit)).CombinedOutput(); err != nil {
		if out != nil && len(out) > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil, err
	}
	out, err := exec.CommandContext(ctx, systemctl, "status", filepath.Base(systemdUnit)).CombinedOutput()
	if err != nil {
		if out != nil && len(out) > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil, err
	}
	return out, nil
}

func UninstallSystemdUnit(ctx context.Context, pth string) error {
	origEUID := syscall.Geteuid()
	if origEUID != 0 {
		if err := syscall.Seteuid(0); err != nil {
			return fmt.Errorf("unable to seteuid 0: %w", err)
		}
		defer func() {
			syscall.Seteuid(origEUID)
		}()
	}

	if out, err := exec.CommandContext(ctx, systemctl, "stop", filepath.Base(systemdUnit)).CombinedOutput(); err != nil {
		if out != nil && len(out) > 0 {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		return err
	}
	if out, err := exec.CommandContext(ctx, systemctl, "disable", filepath.Base(systemdUnit)).CombinedOutput(); err != nil {
		if out != nil && len(out) > 0 {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		return err
	}
	if err := os.Remove(systemdUnit); err != nil {
		return err
	}
	return nil
}

func WriteDefaultSystemdUnit(unitFile, configJson string) error {
	origEUID := syscall.Geteuid()
	if origEUID != 0 {
		if err := syscall.Seteuid(0); err != nil {
			return fmt.Errorf("unable to seteuid 0: %w", err)
		}
		defer func() {
			syscall.Seteuid(origEUID)
		}()
	}
	absolutePath, err := filepath.Abs(os.Args[0])
	if err != nil {
		return err
	}
	args := []string{}
	gotConfig := false
	for _, arg := range os.Args[1:] {
		switch arg {
		case "-install", "-edit-unit", "-edit", "-example":
		case "-config":
			gotConfig = true
		default:
			args = append(args, arg)
		}
	}
	if !gotConfig {
		args = append(args, "-config", configJson)
	}
	cmd := fmt.Sprintf("%s %s", absolutePath, strings.Join(args, " "))
	u, err := user.Current()
	if err != nil {
		return err
	}
	g, err := user.LookupGroupId(u.Gid)
	if err != nil {
		return err
	}
	if err := os.WriteFile(unitFile, []byte(fmt.Sprintf(defaultSystemdUnit, cmd, u.Username, g.Name)), 0644); err != nil {
		return fmt.Errorf("unable to write systemd unit file %s: %w", unitFile, err)
	}
	return nil
}
