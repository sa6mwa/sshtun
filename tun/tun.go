// The tun package creates, reads and writes to/from tun devices.
package tun

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"unsafe"
)

var (
	ErrInvalidAddress error = errors.New("invalid address")
)

const (
	DEV_NET_TUN string = "/dev/net/tun"
)

type TUN struct {
	Name  string
	File  *os.File
	Fd    int
	Ifreq *Ifreq
}

// CreateTUN creates a new tun device with name. If mtu is above 0 it
// attempts to set the MTU. If uid is above 0 it attempts to set the
// owner, same with gid. Returns a TUN which should be closed with
// receiver function Close() when you want to terminate the tunnel.
func CreateTUN(name string, mtu, uid, gid int) (*TUN, error) {
	fd, err := syscall.Open(DEV_NET_TUN, syscall.O_RDWR|syscall.O_CLOEXEC, syscall.IPPROTO_IP)
	//fd, err := unix.Open(DEV_NET_TUN, unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	closeFD := true
	defer func() {
		if closeFD {
			syscall.Close(fd)
		}
	}()

	ifr, err := NewIfreq(name)
	if err != nil {
		syscall.Close(fd)
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	//ifr.SetUint16(syscall.IFF_TUN | syscall.IFF_NO_PI | syscall.IFF_VNET_HDR)
	ifr.SetUint16(syscall.IFF_TUN | syscall.IFF_NO_PI)
	err = IoctlIfreq(fd, syscall.TUNSETIFF, ifr)
	if err != nil {
		return nil, fmt.Errorf("ioctl interface request: %w", err)
	}
	// err = unix.SetNonblock(fd, true)
	// if err != nil {
	// 	return nil, err
	// }
	t := &TUN{
		Name:  ifr.Name(),
		Fd:    fd,
		Ifreq: ifr,
	}

	if mtu > 0 {
		if err := t.SetMTU(mtu); err != nil {
			return nil, err
		}
	}
	if uid > 0 {
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TUNSETOWNER), uintptr(uid))
		if errno != 0 {
			return nil, os.NewSyscallError("ioctl TUNSETOWNER", errno)
		}
	}
	if gid > 0 {
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TUNSETGROUP), uintptr(gid))
		if errno != 0 {
			return nil, os.NewSyscallError("ioctl TUNSETGROUP", errno)
		}
	}
	closeFD = false
	t.File = os.NewFile(uintptr(fd), DEV_NET_TUN)
	return t, nil
}

func (t *TUN) SetMTU(mtu int) error {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)
	t.Ifreq.SetUint32(uint32(mtu))
	if err := IoctlIfreq(fd, syscall.SIOCSIFMTU, t.Ifreq); err != nil {
		return fmt.Errorf("failed to set MTU of TUN device: %w", err)
	}
	return nil
}

func (t *TUN) Close() error {
	e1 := t.File.Close()
	if e1 == nil {
		return nil
	}
	e2 := syscall.Close(t.Fd)
	if e2 != nil {
		return fmt.Errorf("unable to close both TUN os.File and int fd %d: %w: %w", t.Fd, e1, e2)
	}
	return fmt.Errorf("unable to close TUN os.File: %w", e1)
}

func (t *TUN) ConfigureInterface(ipv4_address_with_cidr string) error {
	ipv4, ipnet, err := net.ParseCIDR(ipv4_address_with_cidr)
	if err != nil {
		return err
	}
	ipv4 = ipv4.To4()
	if ipv4 == nil {
		return ErrInvalidAddress
	}

	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, syscall.IPPROTO_IP)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)

	t.Ifreq.Clear()

	*(*syscall.RawSockaddrInet4)(
		unsafe.Pointer(&t.Ifreq.Ifru[:syscall.SizeofSockaddrInet4][0]),
	) = syscall.RawSockaddrInet4{
		Family: syscall.AF_INET,
		Addr:   [4]byte(ipv4),
	}
	if err := IoctlIfreq(fd, syscall.SIOCSIFADDR, t.Ifreq); err != nil {
		return fmt.Errorf("ioctl SIOCSIFADDR: %w", err)
	}

	t.Ifreq.Clear()

	*(*syscall.RawSockaddrInet4)(
		unsafe.Pointer(&t.Ifreq.Ifru[:syscall.SizeofSockaddrInet4][0]),
	) = syscall.RawSockaddrInet4{
		Family: syscall.AF_INET,
		Addr:   [4]byte(ipnet.Mask),
	}
	if err := IoctlIfreq(fd, syscall.SIOCSIFNETMASK, t.Ifreq); err != nil {
		return fmt.Errorf("ioctl SIOCSIFNETMASK: %w", err)
	}

	return nil
}

func (t *TUN) LinkUp() error {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, syscall.IPPROTO_IP)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)

	// Get flags

	t.Ifreq.Clear()

	if err := IoctlIfreq(fd, syscall.SIOCGIFFLAGS, t.Ifreq); err != nil {
		return fmt.Errorf("ioctl SIOCGIFFLAGS: %w", err)
	}

	// Enable broadcast and bring link up

	t.Ifreq.SetUint16(t.Ifreq.Uint16() | syscall.IFF_BROADCAST | syscall.IFF_UP | syscall.IFF_RUNNING)

	if err := IoctlIfreq(fd, syscall.SIOCSIFFLAGS, t.Ifreq); err != nil {
		return fmt.Errorf("ioctl SIOCSIFFLAGS: %w", err)
	}

	return nil
}
