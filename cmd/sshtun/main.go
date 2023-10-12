package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/sa6mwa/sshtun"
)

var (
	version              string = "v0.0.0"
	copyright            string = "(c) 2023 SA6MWA https://github.com/sa6mwa/sshtun"
	configJson           string = sshtun.DEFAULT_CONFIG_FILE
	systemdUnit          string = "/etc/systemd/system/sshtun.service"
	systemctl            string = "/usr/bin/systemctl"
	generateConfig       bool   = false
	editConfig           bool   = false
	editSystemdUnit      bool   = false
	installSystemdUnit   bool   = false
	uninstallSystemdUnit bool   = false
	editor               string = ""
	logLevel             string = slog.LevelInfo.String()
)

func main() {
	flag.CommandLine.SetOutput(os.Stderr)
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "sshtun", version, copyright)
		fmt.Fprintln(os.Stderr, "usage:", os.Args[0], "[options]")
		flag.PrintDefaults()
	}
	flag.StringVar(&configJson, "config", configJson, "Configuration `file` as json")
	flag.BoolVar(&generateConfig, "example", generateConfig, "Generate an example configuration if "+configJson+" does not exist")
	flag.BoolVar(&editConfig, "edit", editConfig, "Edit configuration json, implies -example if file does not exist")
	flag.StringVar(&editor, "editor", editor, "Use `path` to edit configuration json or systemd unit")
	flag.StringVar(&systemdUnit, "systemd-unit", systemdUnit, "If issuing -install or -edit-unit, `path` to systemd unit file")
	flag.BoolVar(&editSystemdUnit, "edit-unit", editSystemdUnit, "Edit systemd unit, create a default if file does not exist")
	flag.BoolVar(&installSystemdUnit, "install", installSystemdUnit, "Install sshtun as a systemd service, use -edit-unit to generate an example unit")
	flag.BoolVar(&uninstallSystemdUnit, "uninstall", uninstallSystemdUnit, "Uninstall sshtun as a systemd service and remove unit file")
	flag.StringVar(&systemctl, "systemctl", systemctl, "If issuing -install, `path` to systemctl")
	flag.StringVar(&logLevel, "level", logLevel, fmt.Sprintf("Set log level, can be %s, %s, %s or %s", slog.LevelDebug.String(), slog.LevelInfo.String(), slog.LevelWarn.String(), slog.LevelError.String()))

	flag.Parse()

	logOutput := (io.Writer)(os.Stderr)
	lvl := new(slog.LevelVar)
	switch strings.ToUpper(logLevel) {
	case slog.LevelDebug.String():
		lvl.Set(slog.LevelDebug)
	case slog.LevelInfo.String():
		lvl.Set(slog.LevelInfo)
	case slog.LevelWarn.String():
		lvl.Set(slog.LevelWarn)
	case slog.LevelError.String():
		lvl.Set(slog.LevelError)
	case "OFF":
		lvl.Set(slog.LevelError)
		logOutput = io.Discard
	default:
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("Unsupported log level", "level", logLevel)
		os.Exit(1)
	}

	l := slog.New(slog.NewJSONHandler(logOutput, &slog.HandlerOptions{
		Level: lvl,
	}))

	// Ensure a SETUID u+s execve is running as the calling user, not the effective user.

	if uid, euid := os.Getuid(), os.Geteuid(); uid != euid {
		gid := os.Getgid()
		egid := os.Getegid()
		if err := syscall.Seteuid(uid); err != nil {
			l.Error("Unable to set effective user ID to calling user", "error", err, "uid", uid, "euid", euid, "gid", gid, "egid", egid)
			os.Exit(1)
		}
	}

	configurationFile := sshtun.ResolveTildeSlash(configJson)
	systemdUnitFile := sshtun.ResolveTildeSlash(systemdUnit)

	// -edit

	if editConfig {
		l.Info("Editing configuration", "file", configurationFile)
		if !fileExists(configurationFile) {
			// save a default configJson
			tunnels := sshtun.DefaultConfig(l)
			if err := tunnels.SaveConfig(configurationFile); err != nil {
				l.Error("Unable to save configuration: "+err.Error(), "file", configurationFile, "error", err)
				os.Exit(1)
			}
		}
		// configJson should exist from here
		if err := EditConfig(configurationFile); err != nil {
			l.Error("Failed to edit configuration", "file", configurationFile, "error", err)
			os.Exit(1)
		}
	}

	// -edit-unit

	if editSystemdUnit {
		l.Info("Editing systemd unit", "file", systemdUnitFile)
		if !fileExists(systemdUnitFile) {
			if err := WriteDefaultSystemdUnit(systemdUnitFile, configJson); err != nil {
				l.Error("Unable to write default systemd unit file", "error", err, "file", systemdUnitFile)
				os.Exit(1)
			}
		}
		if err := EditFile(context.Background(), systemdUnitFile, true); err != nil {
			l.Error("Unable to edit systemd unit file", "error", err, "file", systemdUnitFile)
			os.Exit(1)
		}
	}

	// -install

	if installSystemdUnit {
		l.Info("Installing systemd unit", "file", systemdUnitFile, "systemctl", systemctl)
		status, err := InstallSystemdUnit(context.Background(), systemdUnitFile)
		if err != nil {
			l.Error("Unable to install systemd unit file", "error", err, "file", systemdUnitFile)
			os.Exit(1)
		}
		l.Info("Systemd status", "status", string(status), "unit", filepath.Base(systemdUnitFile), "file", systemdUnitFile, "systemctl", systemctl)
	}

	if uninstallSystemdUnit {
		l.Info("Removing (uninstalling) systemd unit", "file", systemdUnitFile, "systemctl", systemctl)
		if err := UninstallSystemdUnit(context.Background(), systemdUnitFile); err != nil {
			l.Error("Unable to uninstall systemd unit file", "error", err, "file", systemdUnitFile)
			os.Exit(1)
		}
	}

	// If in edit or install mode, exit

	if editConfig || editSystemdUnit || installSystemdUnit || uninstallSystemdUnit {
		return
	}

	tunnels, err := sshtun.LoadConfig(configJson, l)
	if err != nil {
		if os.IsNotExist(err) && generateConfig {
			tunnels = sshtun.LoadConfigOrReturnDefault(configJson, l)
			if err := tunnels.SaveConfig(configJson); err != nil {
				l.Error("Unable to save configuration: "+err.Error(), "file", sshtun.ResolveTildeSlash(configJson), "error", err)
			} else {
				l.Info("Saved configuration", "file", sshtun.ResolveTildeSlash(configJson))
			}
			return
		}
		l.Error("Unable to load configuration file: "+err.Error(), "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		defer cancel()
		signalChannel := make(chan os.Signal, 1)
		signal.Notify(signalChannel, syscall.SIGINT, syscall.SIGTERM)
		select {
		case sig := <-signalChannel:
			l.Warn("Caught signal, shutting down", "signal", sig.String())
		case <-ctx.Done():
			l.Warn("Context closed, shutting down")
		}
		signal.Stop(signalChannel)
		close(signalChannel)
	}()

	l.Info("Welcome to sshtun "+version+" "+copyright, "config", configurationFile, "total_tunnels", tunnels.Total(), "enabled_tunnels", tunnels.Enabled())

	if err := tunnels.OpenAll(ctx); err != nil {
		l.Error("Error establishing tunnel(s)", "error", err)
		cancel()
		os.Exit(1)
	}
}
