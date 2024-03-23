// sshtest is (C) Metarsit Leenayongwut https://medium.com/@metarsit
// Copied from https://medium.com/@metarsit/ssh-is-fun-till-you-need-to-unit-test-it-in-go-f3b3303974ab
package sshtest

import (
	"io"

	"github.com/gliderlabs/ssh"
)

type HoneyPot struct {
	server *ssh.Server
}

func NewHoneyPot(addr string) *HoneyPot {
	return &HoneyPot{
		server: &ssh.Server{
			Addr: addr,
			Handler: func(s ssh.Session) {
				io.WriteString(s, "Honey pot")
			},
			PasswordHandler: func(ctx ssh.Context, password string) bool {
				return true
			},
		},
	}
}

func (h *HoneyPot) ListenAndServe() error {
	return h.server.ListenAndServe()
}

func (h *HoneyPot) Close() error {
	return h.server.Close()
}

func (h *HoneyPot) SetReturnString(str string) {
	h.server.Handler = func(s ssh.Session) {
		io.WriteString(s, str)
	}
}
