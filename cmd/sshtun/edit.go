package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	DefaultEditors = []string{"/bin/vim", "/usr/bin/vim", "/bin/vi", "/usr/bin/nano"}
)

var (
	ErrNoEditorFound  error = errors.New("no editor found")
	ErrNotATerminal   error = errors.New("os.Stdin is not a terminal")
	ErrEditorNotFound error = errors.New("editor not found")
)

func EditFile(ctx context.Context, pth string, becomeRoot bool) error {
	if !IsUnixTerminal(os.Stdin) {
		return ErrNotATerminal
	}
	executables := []string{}
	envEditor := os.Getenv("EDITOR")
	switch {
	case envEditor != "" && fileExists(envEditor):
		executables = append(executables, envEditor)
	case editor == "":
		executables = append(executables, DefaultEditors...)
	default:
		if !fileExists(editor) {
			return ErrEditorNotFound
		}
		executables = append(executables, editor)
	}
	if len(executables) == 0 {
		return ErrNoEditorFound
	}

	var origEUID int
	if becomeRoot {
		origEUID = syscall.Geteuid()
		if origEUID != 0 {
			if err := syscall.Seteuid(0); err != nil {
				return err
			}
			defer func() {
				syscall.Seteuid(origEUID)
			}()
		}
	}

	tempfile, err := copyFileToTemp(pth)
	if err != nil {
		return err
	}

	if becomeRoot {
		if err := os.Chown(tempfile, syscall.Getuid(), syscall.Getgid()); err != nil {
			return err
		}
	}

	c, cancel := context.WithCancel(ctx)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()
	defer func() {
		os.Remove(tempfile)
		signal.Stop(sigCh)
		close(sigCh)
	}()

	if becomeRoot {
		if err := syscall.Seteuid(origEUID); err != nil {
			return err
		}
	}
	if err := tryExec(c, executables, tempfile); err != nil {
		return err
	}
	if c.Err() != nil {
		return nil
	}
	if becomeRoot {
		if err := syscall.Seteuid(0); err != nil {
			return err
		}
	}
	editedF, err := os.Open(tempfile)
	if err != nil {
		return err
	}
	outputF, err := os.OpenFile(pth, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	defer outputF.Close()
	if _, err := io.Copy(outputF, editedF); err != nil {
		return err
	}
	return nil
}

func fileExists(file string) bool {
	fs, err := os.Stat(file)
	if err != nil {
		return false
	}
	if fs.Mode().IsRegular() {
		return true
	}
	return false
}

func copyFileToTemp(originalFile string) (string, error) {
	o, err := os.Open(originalFile)
	if err != nil {
		return "", err
	}
	defer o.Close()
	n, err := os.CreateTemp("", "edit-*-"+filepath.Base(o.Name()))
	if err != nil {
		return "", err
	}
	tempfile := n.Name()
	remove := true
	defer func() {
		n.Close()
		if remove {
			os.Remove(tempfile)
		}
	}()
	// if err := os.Chown(tempfile, os.Getuid(), os.Getgid()); err != nil {
	// 	return "", err
	// }
	if _, err := io.Copy(n, o); err != nil {
		return "", err
	}
	remove = false
	return tempfile, nil
}

func tryExec(ctx context.Context, executables []string, arg ...string) error {
	for _, executable := range executables {
		cmd := exec.CommandContext(ctx, executable, arg...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			if _, found := err.(*exec.ExitError); found {
				return err
			}
			continue
		}
		return nil
	}
	return ErrNoEditorFound
}

// func rndstr(length int) string {
// 	buf := make([]byte, length)
// 	retries := 50
// 	for i := 0; i < retries; i++ {
// 		if _, err := rand.Read(buf); err != nil {
// 			continue
// 		}
// 		break
// 	}
// 	return hex.EncodeToString(buf)
// }

// IsUnixTerminal is constructed from terminal.IsTerminal() and is only
// reproduced here in order not to import an external dependency.
func IsUnixTerminal(f *os.File) bool {
	type UnixTermios struct {
		Iflag  uint32
		Oflag  uint32
		Cflag  uint32
		Lflag  uint32
		Line   uint8
		Cc     [19]uint8
		Ispeed uint32
		Ospeed uint32
	}
	const TCGETS = 0x5401
	const SYS_IOCTL = 16
	fd := f.Fd()
	var value UnixTermios
	req := TCGETS
	_, _, e1 := syscall.Syscall(SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(unsafe.Pointer(&value)))
	return e1 == 0
}
