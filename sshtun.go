package sshtun

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alessio/shellescape"
	"github.com/sa6mwa/sshtun/internal/crand"
	"github.com/sa6mwa/sshtun/tun"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

//go:embed bin/tunreadwriter
var tunreadwriter []byte

var (
	ErrNilPointer       error = errors.New("nil pointer error")
	ErrEmptySshAuthSock error = fmt.Errorf("%s is empty", SSH_AUTH_SOCK)
	ErrNoTunReadWriter  error = errors.New("missing path to remote tunreadwriter (CopyHelperToRemote must come first)")
	ErrUnrecoverable    error = errors.New("unrecoverable")
	ErrMissingContext   error = errors.New("sshtun context value missing, please use sshtun.Context(parent_ctx)")
)

const (
	ROOT                int    = 0
	DEFAULT_CONFIG_FILE string = `~/.config/sshtun/config.json`
	SSH_AUTH_SOCK       string = `SSH_AUTH_SOCK`
	DEV_NET_TUN         string = `/dev/net/tun`
	USR_BIN_SCP         string = `/usr/bin/scp`
)

type PrivateKeyFiles []string

// sshtunKey and sshtun is set in the context passed to Open as a
// context.WithValue. The mutex is used to prevent two tunnels from
// opening at the same time or interfering with switching effective
// uid which affects the main process.
type sshtunKey struct{}
type sshtun struct {
	mutex *sync.Mutex
}

type Tunnels struct {
	Tunnels []*SSHTUN    `json:"tunnels"`
	log     *slog.Logger `json:"-"`
}

type SSHTUN struct {
	Name                   string          `json:"name"`
	Comment                string          `json:"comment,omitempty"`
	Protocol               string          `json:"protocol"`
	LocalNetwork           string          `json:"local_network"`
	LocalTunDevice         string          `json:"local_tun_device"`
	LocalMTU               int             `json:"local_mtu"`
	Remote                 string          `json:"remote"`
	RemoteNetwork          string          `json:"remote_network"`
	RemoteTunDevice        string          `json:"remote_tun_device"`
	RemoteMTU              int             `json:"remote_mtu"`
	RemoteUser             string          `json:"remote_user"`
	UseSSHAgent            bool            `json:"use_ssh_agent"`
	PrivateKeyFiles        PrivateKeyFiles `json:"private_key_files"`
	RemoteUploadDirectory  string          `json:"remote_upload_directory"`
	RemoteSCP              string          `json:"remote_scp"`
	Enable                 bool            `json:"enable"`
	KeepaliveInterval      Duration        `json:"keepalive_interval"`
	KeepaliveMaxErrorCount int             `json:"keepalive_max_error_count"`
	remoteTunReadWriter    string          `json:"-"`
	done                   bool            `json:"-"`
	log                    *slog.Logger    `json:"-"`
}

type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case float64:
		*d = Duration(time.Duration(value))
		return nil
	case string:
		dur, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		*d = Duration(dur)
		return nil
	default:
		return errors.New("invalid duration")
	}
}

// Returns a new SSHTUN struct with defaults. Use this struct to
// further configure the tunneler. logger can be nil if you do not
// want any logging.
func NewSecureShellTunneler(logger *slog.Logger) *SSHTUN {
	cfg := &SSHTUN{
		Name:            "example",
		Protocol:        "tcp4",
		LocalNetwork:    "172.18.0.1/24",
		LocalTunDevice:  "tun0",
		Remote:          "localhost:22",
		RemoteNetwork:   "172.18.0.2/24",
		RemoteTunDevice: "tun0",
		RemoteUser:      "",
		Enable:          false,
		UseSSHAgent:     false,
		PrivateKeyFiles: []string{
			"~/.ssh/id_rsa",
		},
		RemoteSCP:              USR_BIN_SCP,
		KeepaliveInterval:      Duration(2 * time.Minute),
		KeepaliveMaxErrorCount: 5,
		log:                    SetLogger(logger),
	}
	if usr, err := user.Current(); err == nil {
		cfg.RemoteUser = usr.Username
	}
	return cfg
}

func LoadConfig(configJson string, logger *slog.Logger) (*Tunnels, error) {
	f, err := os.Open(ResolveTildeSlash(configJson))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var config Tunnels
	if err := json.NewDecoder(f).Decode(&config); err != nil {
		return nil, err
	}
	for i := range config.Tunnels {
		config.Tunnels[i].log = SetLogger(logger)
		if config.Tunnels[i].RemoteSCP == "" {
			config.Tunnels[i].RemoteSCP = USR_BIN_SCP
		}
	}
	config.log = SetLogger(logger)
	return &config, nil
}

func LoadConfigOrReturnDefault(configJson string, logger *slog.Logger) *Tunnels {
	config, err := LoadConfig(ResolveTildeSlash(configJson), logger)
	if err != nil {
		return DefaultConfig(logger)
	}
	config.log = SetLogger(logger)
	return config
}

func DefaultConfig(logger *slog.Logger) *Tunnels {
	return &Tunnels{
		Tunnels: []*SSHTUN{NewSecureShellTunneler(SetLogger(logger))},
		log:     SetLogger(logger),
	}
}

func LoadAndSave(configJson string, logger *slog.Logger) (*Tunnels, error) {
	t := LoadConfigOrReturnDefault(configJson, logger)
	t.log = SetLogger(logger)
	return t, t.SaveConfig(configJson)
}

func (t *Tunnels) Total() int {
	return len(t.Tunnels)
}

func (t *Tunnels) Enabled() int {
	count := 0
	for _, tunnel := range t.Tunnels {
		if tunnel.Enable {
			count++
		}
	}
	return count
}

func (t *Tunnels) SaveConfig(configJson string) error {
	pth := ResolveTildeSlash(configJson)
	if err := os.MkdirAll(filepath.Dir(pth), 0777); err != nil {
		return err
	}
	f, err := os.OpenFile(pth, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		return err
	}
	defer f.Close()
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(t); err != nil {
		return err
	}
	return nil
}

func (t *Tunnels) OpenAll(ctx context.Context) error {
	var wg sync.WaitGroup
	numberOfTunnels := 0
	ctx = Context(ctx)
	for i := range t.Tunnels {
		tunnel := t.Tunnels[i]
		if !tunnel.Enable {
			t.log.Info("Tunnel not enabled, skipping", "name", tunnel.Name, "remote", tunnel.Remote, "remote_net", tunnel.RemoteNetwork, "local_net", tunnel.LocalNetwork)
			continue
		}
		t.log.Info(fmt.Sprintf("Connecting tunnel %s", tunnel.Name), "name", tunnel.Name, "remote", tunnel.Remote, "remote_net", tunnel.RemoteNetwork, "local_net", tunnel.LocalNetwork)
		numberOfTunnels++
		wg.Add(1)
		go func() {
			for {
				if err := tunnel.Open(ctx); err != nil {
					t.log.Error(err.Error())
					if errors.Is(err, ErrUnrecoverable) {
						wg.Done()
						return
					}
				}
				tmr := time.NewTimer(5 * time.Second)
				defer tmr.Stop()
				select {
				case <-ctx.Done():
					wg.Done()
					return
				case <-tmr.C:
				}
			}
		}()
	}

	if numberOfTunnels == 0 {
		return fmt.Errorf("0 out of %d tunnel(s) marked enabled in configuration", len(t.Tunnels))
	}

	wg.Wait()
	// should never reach here...
	return nil
}

func ResolveTildeSlash(pth string) string {
	if strings.HasPrefix(pth, "~/") {
		dir, err := os.UserHomeDir()
		if err != nil {
			return pth
		}
		return filepath.Join(dir, pth[2:])
	}
	return pth
}

func CreateFile(pth string) (*os.File, error) {
	fullPath := ResolveTildeSlash(pth)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0222); err != nil {
		return nil, err
	}
	return os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0222)
}

// Add receiver functions to implement the flag.Value interface for flag.Var()...

func (k *PrivateKeyFiles) String() string {
	if k == nil {
		return ""
	}
	pkfs := make([]string, len(*k))
	for _, pkf := range *k {
		pkfs = append(pkfs, ResolveTildeSlash(pkf))
	}
	return strings.Join(pkfs, ", ")
}

func (k *PrivateKeyFiles) Set(value string) error {
	if k == nil {
		return ErrNilPointer
	}
	*k = append(*k, value)
	return nil
}

func unrecoverable(err error) error {
	return fmt.Errorf("%w: %w", ErrUnrecoverable, err)
}

// Returns a context with an internal sshtun object mainly used for
// synchronization (sync.Mutex).
func Context(ctx context.Context) context.Context {
	var mu sync.Mutex
	ctx = context.WithValue(ctx, sshtunKey{}, sshtun{
		mutex: &mu,
	})
	return ctx
}

// Open is the main function for setting up and connecting both ends
// of the tunnel. Open blocks until tunnel is closed or ctx is
// cancelled. ctx must be initialized via the Context function before
// passed to Open or ErrMissingContext will be returned.
func (s *SSHTUN) Open(ctx context.Context) error {
	v, ok := ctx.Value(sshtunKey{}).(sshtun)
	if !ok {
		return ErrMissingContext
	}

	// Lock mutex and setup a defer conditionally unlocking the mutex
	v.mutex.Lock()

	s.log.Debug("Locked mutex", "name", s.Name)

	unlockOnExit := true
	defer func() {
		if unlockOnExit {
			s.log.Debug("Unlocking mutex", "name", s.Name)
			v.mutex.Unlock()
		}
	}()

	if os.Geteuid() != ROOT {
		s.log.Info(fmt.Sprintf("Switching to uid %d", ROOT), "sudo", "ConfigureInterface", "uid_to", ROOT, "uid_from", os.Geteuid(), "name", s.Name)
	}
	b, err := s.Become(ROOT)
	if err != nil {
		return unrecoverable(err)
	}

	s.log.Info("Creating local TUN device", "tun", s.LocalTunDevice, "name", s.Name)
	localTUN, err := tun.CreateTUN(s.LocalTunDevice, s.LocalMTU, 0, 0)
	if err != nil {
		return unrecoverable(err)
	}
	defer localTUN.Close()
	s.LocalTunDevice = localTUN.Name

	s.log.Info(fmt.Sprintf("Configuring interface %s with address %s and MTU %d", localTUN.Name, s.LocalNetwork, s.LocalMTU), "name", s.Name, "net", s.LocalNetwork, "mtu", s.LocalMTU, "proto", s.Protocol)

	if err := localTUN.ConfigureInterface(s.LocalNetwork); err != nil {
		return unrecoverable(err)
	}

	if os.Geteuid() != b.OriginalUID() {
		s.log.Info("Switching back to original uid", "uid_to", b.OriginalUID(), "uid_from", os.Geteuid(), "name", s.Name)
	}

	if err := b.Unbecome(); err != nil {
		return unrecoverable(err)
	}

	s.log.Info(fmt.Sprintf("Connecting to ssh://%s", s.Remote), "remote", s.Remote, "name", s.Name)

	client, err := s.Dial(ctx)
	if err != nil {
		return err
	}
	openDone := make(chan struct{})
	defer close(openDone)
	go func() {
		select {
		case <-ctx.Done():
		case <-openDone:
		}
		client.Close()
	}()

	// Transfer tunreadwriter to other side

	if err := s.UploadHelperToRemote(client, ""); err != nil {
		return err
	}

	if os.Geteuid() != ROOT {
		s.log.Info(fmt.Sprintf("Switching to uid %d", ROOT), "sudo", "LinkUp", "uid_to", ROOT, "uid_from", os.Geteuid(), "name", s.Name)
	}
	if err := b.Become(ROOT); err != nil {
		return unrecoverable(err)
	}

	s.log.Info("Link up", "local_tun", localTUN.Name, "local_net", s.LocalNetwork, "name", s.Name)

	if err := localTUN.LinkUp(); err != nil {
		return unrecoverable(err)
	}

	if os.Geteuid() != b.OriginalUID() {
		s.log.Info("Switching back to original uid", "uid_to", b.OriginalUID(), "uid_from", os.Geteuid(), "name", s.Name)
	}

	if err := b.Unbecome(); err != nil {
		return unrecoverable(err)
	}

	if s.KeepaliveInterval > 0 {
		s.log.Info("Enabling ssh keep-alive", "keepalive_interval", s.KeepaliveInterval, "keepalive_max_error_count", s.KeepaliveMaxErrorCount, "name", s.Name, "remote", s.Remote, "remote_addr", client.RemoteAddr().String(), "local_addr", client.LocalAddr().String())
		done := make(chan struct{})
		defer close(done)
		go StartKeepalive(client, time.Duration(s.KeepaliveInterval), s.KeepaliveMaxErrorCount, s.log, done)
	}

	s.log.Debug("Unlocking mutex", "name", s.Name)

	// Unlock mutex
	v.mutex.Unlock()
	unlockOnExit = false

	s.log.Info("Starting tunnel", "name", s.Name, "remote", s.Remote, "local_net", s.LocalNetwork, "remote_net", s.RemoteNetwork, "local_tun", s.LocalTunDevice, "remote_tun", s.RemoteTunDevice, "local_mtu", s.LocalMTU, "remote_mtu", s.RemoteMTU)

	if err := s.StartTunneling(client, localTUN); err != nil {
		if ctx.Err() == nil {
			return fmt.Errorf("sshtun.StartTunneling: %w", err)
		}
	}
	s.log.Info("Tunnel closed", "name", s.Name, "remote", s.Remote, "local_net", s.LocalNetwork, "remote_net", s.RemoteNetwork, "local_tun", s.LocalTunDevice, "remote_tun", s.RemoteTunDevice, "local_mtu", s.LocalMTU, "remote_mtu", s.RemoteMTU)
	return nil
}

func (s *SSHTUN) StartTunneling(client *ssh.Client, localTUN *tun.TUN) error {
	if s.remoteTunReadWriter == "" {
		return ErrNoTunReadWriter
	}

	mtustring := strconv.Itoa(s.RemoteMTU)
	remoteTunReadWriterCommand := fmt.Sprintf(
		"sudo %s -delete -dev %s -net %s -mtu %s",
		shellescape.Quote(s.remoteTunReadWriter),
		shellescape.Quote(s.RemoteTunDevice),
		shellescape.Quote(s.RemoteNetwork),
		shellescape.Quote(mtustring),
	)

	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	remoteIN, err := session.StdinPipe()
	if err != nil {
		return nil
	}
	defer remoteIN.Close()

	remoteOUT, err := session.StdoutPipe()
	if err != nil {
		return err
	}

	// tunreadwriter write errors to stderr
	remoteERR, err := session.StderrPipe()
	if err != nil {
		return err
	}

	s.log.Info(fmt.Sprintf("Starting %s on remote ssh://%s", s.remoteTunReadWriter, s.Remote), "remote_addr", client.RemoteAddr().String(), "remote", s.Remote, "remote_command", remoteTunReadWriterCommand, "name", s.Name)

	if err := session.Start(remoteTunReadWriterCommand); err != nil {
		return err
	}

	go func() {
		if _, err := io.Copy(localTUN.File, remoteOUT); err != nil {
			s.log.Error("io error in remote to local go routine", "error", err)
		}
	}()
	go func() {
		if _, err := io.Copy(remoteIN, localTUN.File); err != nil {
			s.log.Error("io error in local to remote go routine", "error", err)
		}
	}()

	trwERR := func() string {
		var rerr bytes.Buffer
		io.Copy(&rerr, remoteERR)
		if rerr.Len() > 0 {
			return strings.TrimSpace(rerr.String())
		}
		return "no output on stderr"
	}
	if err := session.Wait(); err != nil {
		return fmt.Errorf("%w: %s", err, trwERR())
	}
	return nil
}

func (s *SSHTUN) UploadHelperToRemote(client *ssh.Client, remoteDirectory string) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	if remoteDirectory == "" {
		remoteDirectory = "/tmp"
	}
	randomFilename := fmt.Sprintf("tunreadwriter-%s-%d", time.Now().UTC().Format("20060102T150405"), crand.Int63())
	size := len(tunreadwriter)
	f := bytes.NewReader(tunreadwriter)

	completeFilename := filepath.Join(remoteDirectory, randomFilename)

	s.log.Info(fmt.Sprintf("Uploading tunreadwriter as %s to ssh://%s", completeFilename, s.Remote), "name", s.Name, "tunreadwriter", completeFilename, "size", size)

	remoteOUT, err := session.StdoutPipe()
	if err != nil {
		return err
	}
	remoteERR, err := session.StderrPipe()
	if err != nil {
		return err
	}
	errCh := make(chan error)
	go func() {
		defer close(errCh)
		remoteIN, err := session.StdinPipe()
		if err != nil {
			errCh <- err
			return
		}
		defer remoteIN.Close()
		fmt.Fprintf(remoteIN, "C0755 %d %s\n", size, randomFilename)
		io.CopyN(remoteIN, f, int64(size))
		fmt.Fprint(remoteIN, "\x00")
	}()

	combinedOut := func() string {
		var rout bytes.Buffer
		var rerr bytes.Buffer
		io.Copy(&rout, remoteOUT)
		io.Copy(&rerr, remoteERR)
		var combinedOut string
		if rout.Len() > 0 && rerr.Len() > 0 {
			combinedOut = rout.String() + " " + rerr.String()
		} else if rout.Len() > 0 {
			combinedOut = rout.String()
		} else if rerr.Len() > 0 {
			combinedOut = rerr.String()
		} else {
			combinedOut = "no output from command"
		}
		return strings.TrimSpace(combinedOut)
	}

	if err := session.Run(s.RemoteSCP + " -t " + remoteDirectory); err != nil {
		output := combinedOut()
		return fmt.Errorf("%w: %s", err, output)
	}

	if err := <-errCh; err != nil {
		output := combinedOut()
		return fmt.Errorf("%w: %s", err, output)
	}
	s.remoteTunReadWriter = filepath.Join(remoteDirectory, randomFilename)

	return nil
}

func sshrun(client *ssh.Client, cmd string) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	return session.Run(cmd)
}

// Dial connects to ssh-agent (if s.UseSSHAgent is true), retrieves
// signers or privatekeys from key files and ssh.Dials SSHTUN.Remote
// using s.Protocol. Returns an ssh.Client or error. The ssh.Client
// must be Closed when done.
func (s *SSHTUN) Dial(ctx context.Context) (*ssh.Client, error) {
	signers := make([]ssh.Signer, 0)
	if s.UseSSHAgent && os.Getenv(SSH_AUTH_SOCK) != "" {
		sock, err := net.Dial("unix", os.Getenv(SSH_AUTH_SOCK))
		if err != nil {
			return nil, err
		}
		agent := agent.NewClient(sock)
		signers, err = agent.Signers()
		if err != nil {
			return nil, err
		}
	} else if s.UseSSHAgent && os.Getenv(SSH_AUTH_SOCK) == "" {
		return nil, ErrEmptySshAuthSock
	} else {
		for _, pk := range s.PrivateKeyFiles {
			pemBytes, err := os.ReadFile(ResolveTildeSlash(pk))
			if err != nil {
				return nil, err
			}
			signer, err := ssh.ParsePrivateKey(pemBytes)
			if err != nil {
				return nil, err
			}
			signers = append(signers, signer)
		}
	}
	auths := []ssh.AuthMethod{ssh.PublicKeys(signers...)}
	cfg := &ssh.ClientConfig{
		User:            s.RemoteUser,
		Auth:            auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}
	cfg.SetDefaults()

	// Use a DialContext dialer and use ssh.NewClientConn to establish a
	// ssh.NewClientConn and ssh.NewClient.

	d := net.Dialer{Timeout: cfg.Timeout}
	conn, err := d.DialContext(ctx, s.Protocol, s.Remote)
	if err != nil {
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, s.Remote, cfg)
	if err != nil {
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}

type Became struct {
	originalUID int
	becameUID   int
	logger      *slog.Logger
	st          *SSHTUN
}

func (s *SSHTUN) Become(uid int) (*Became, error) {
	s.log.Debug(fmt.Sprintf("Before Become(%d)", uid), "uid", os.Getuid(), "gid", os.Getgid(), "euid", os.Geteuid(), "egid", os.Getegid())
	became := &Became{
		originalUID: syscall.Geteuid(),
		logger:      s.log,
		st:          s,
	}
	if became.originalUID != uid {
		errmsg := "unable to change to uid 0 (perhaps missing setuid mode on executable? chown 0:0 sshtun; chmod 4755 sshtun)"
		if err := syscall.Seteuid(uid); err != nil {
			return nil, fmt.Errorf(errmsg+": %w", err)
		}
	}
	became.becameUID = syscall.Geteuid()
	s.log.Debug(fmt.Sprintf("After Become(%d)", uid), "uid", os.Getuid(), "gid", os.Getgid(), "euid", os.Geteuid(), "egid", os.Getegid())
	return became, nil
}

func (b *Became) Unbecome() error {
	b.logger.Debug("Before Unbecome()", "uid", os.Getuid(), "gid", os.Getgid(), "euid", os.Geteuid(), "egid", os.Getegid())
	if syscall.Geteuid() != b.originalUID {
		if err := syscall.Seteuid(b.originalUID); err != nil {
			return err
		}
	}
	b.becameUID = syscall.Geteuid()
	b.logger.Debug("After Unbecome()", "uid", os.Getuid(), "gid", os.Getgid(), "euid", os.Geteuid(), "egid", os.Getegid())
	return nil
}

func (b *Became) Become(uid int) error {
	c, err := b.st.Become(uid)
	if err != nil {
		return err
	}
	b.becameUID = c.becameUID
	return nil
}

func (b *Became) OriginalUID() int {
	return b.originalUID
}

// StartKeepalive borrowed from github.com/scylladb/go-sshtools,
// Copyright (c) Michał Matczuk <michal@scylladb.com>
// https://github.com/scylladb/go-sshtools
//
// StartKeepalive starts sending server keepalive messages until done channel
// is closed.
func StartKeepalive(client *ssh.Client, interval time.Duration, countMax int, logger *slog.Logger, done <-chan struct{}) {
	logger = SetLogger(logger)
	t := time.NewTicker(interval)
	defer t.Stop()
	n := 0
	for {
		select {
		case <-t.C:
			logger.Debug("Sending keepalive message", "local_addr", client.LocalAddr().String(), "remote_addr", client.RemoteAddr().String())
			if err := serverAliveCheck(client); err != nil {
				n++
				if n >= countMax {
					logger.Error("Keepalive check failed too many times", "count", n, "local_addr", client.LocalAddr().String(), "remote_addr", client.RemoteAddr().String())
					client.Close()
					return
				}
			} else {
				n = 0
			}
		case <-done:
			return
		}
	}
}

func serverAliveCheck(client *ssh.Client) (err error) {
	// This is ported version of Open SSH client server_alive_check function
	// see: https://github.com/openssh/openssh-portable/blob/b5e412a8993ad17b9e1141c78408df15d3d987e1/clientloop.c#L482
	_, _, err = client.SendRequest("keepalive@openssh.com", true, nil)
	return
}

// NilSlogger implements a no-operation slog.Handler
type NilSlogger struct {
	*nilslogger
}

type nilslogger struct{}

func (ns *NilSlogger) Enabled(_ context.Context, _ slog.Level) bool {
	return false
}

func (ns *NilSlogger) Handle(_ context.Context, _ slog.Record) error {
	return nil
}

func (ns *NilSlogger) WithAttrs(_ []slog.Attr) slog.Handler {
	return &NilSlogger{nilslogger: &nilslogger{}}
}

func (ns *NilSlogger) WithGroup(_ string) slog.Handler {
	return &NilSlogger{nilslogger: &nilslogger{}}
}

// SetLogger returns s if not nil or an slog.New(NilSlogger)
// no-operation slog.Handler if s is nil.
func SetLogger(s *slog.Logger) *slog.Logger {
	if s == nil {
		return slog.New(&NilSlogger{&nilslogger{}})
	}
	return s
}
