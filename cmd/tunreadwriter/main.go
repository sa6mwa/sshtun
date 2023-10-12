package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"strings"
	"syscall"

	"github.com/sa6mwa/sshtun/pkg/tun"
)

var (
	mtu          int
	device       string
	network      string
	username     string
	groupname    string
	uid          int
	gid          int
	deleteMyself bool
)

func main() {
	flag.IntVar(&mtu, "mtu", 0, "`MTU` of created tun device, 0 means the kernel default, usually 1500")
	flag.StringVar(&device, "dev", "tun0", "`TUN` device to read from and write to stdout, write to and read from stdin")
	flag.StringVar(&network, "net", "172.16.0.3/24", "Network address with CIDR to assign to the tun device")
	flag.StringVar(&username, "user", "", "Set owner of created tun device to `username`")
	flag.StringVar(&groupname, "group", "", "Set group of created tun device to `groupname`")
	flag.BoolVar(&deleteMyself, "delete", false, "Delete myself when exiting")
	flag.Parse()
	if err := tunreadwriter(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func tunreadwriter() error {
	if deleteMyself {
		defer func() {
			os.Remove(os.Args[0])
		}()
	}

	device = strings.TrimSpace(device)
	if device == "" {
		return errors.New("missing device name")
	}

	network = strings.TrimSpace(network)
	if network == "" {
		return errors.New("missing network address")
	}

	if username != "" {
		usr, err := user.Lookup(username)
		if err != nil {
			return err
		}
		uid, err = strconv.Atoi(usr.Uid)
		if err != nil {
			return err
		}
		gid, err = strconv.Atoi(usr.Gid)
		if err != nil {
			return err
		}
	}
	if groupname != "" {
		grp, err := user.LookupGroup(groupname)
		if err != nil {
			return err
		}
		gid, err = strconv.Atoi(grp.Gid)
		if err != nil {
			return err
		}
	}

	localTUN, err := tun.CreateTUN(device, mtu, 0, 0)
	if err != nil {
		return err
	}
	defer localTUN.Close()

	if err := localTUN.ConfigureInterface(network); err != nil {
		return err
	}

	if err := localTUN.LinkUp(); err != nil {
		return err
	}

	// Read from TUN device, write to stdout
	fromTUNdone := make(chan struct{})
	go func() {
		defer close(fromTUNdone)
		if _, err := io.Copy(os.Stdout, localTUN.File); err != nil {
			fmt.Fprint(os.Stderr, "io error from "+localTUN.Name+" to stdout:", err)
		}
	}()

	fromSTDINdone := make(chan struct{})
	go func() {
		defer close(fromSTDINdone)
		// Read from stdin, write to TUN device
		if _, err := io.Copy(localTUN.File, os.Stdin); err != nil {
			fmt.Fprint(os.Stderr, "io error from stdin to "+localTUN.Name+":", err)
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		sigCh := make(chan os.Signal, 1)
		defer close(sigCh)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		select {
		case sig := <-sigCh:
			fmt.Fprintln(os.Stderr, "Caught signal", sig.String())
		case <-fromTUNdone:
		case <-fromSTDINdone:
		}
		signal.Stop(sigCh)
	}()

	<-done
	return nil
}
