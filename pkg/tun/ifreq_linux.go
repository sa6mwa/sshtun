// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux
// +build linux

package tun

import (
	"bytes"
	"os"
	"syscall"
	"unsafe"
)

const (
	SizeofPtr  = 0x8
	SizeofLong = 0x8
)

// Helpers for dealing with ifreq since it contains a union and thus requires a
// lot of unsafe.Pointer casts to use properly.

// An Ifreq is (not a) type-safe wrapper around the raw ifreq
// struct. An Ifreq contains an interface name and a union of
// arbitrary data which can be accessed using the Ifreq's methods. To
// create an Ifreq, use the NewIfreq function.
//
// Use the Name method to access the stored interface name. The union data
// fields can be get and set using the following methods:
//   - Uint16/SetUint16: flags
//   - Uint32/SetUint32: ifindex, metric, mtu
type Ifreq struct {
	Ifrn [syscall.IFNAMSIZ]byte
	Ifru [24]byte
}

// IoctlIfreq performs an ioctl using an Ifreq structure for input and/or
// output. See the netdevice(7) man page for details.
func IoctlIfreq(fd int, req uint, value *Ifreq) error {
	// It is possible we will add more fields to *Ifreq itself later to prevent
	// misuse, so pass the raw *ifreq directly.
	return ioctlPtr(fd, req, unsafe.Pointer(value))
}

func ioctlPtr(fd int, req uint, arg unsafe.Pointer) (err error) {
	_, _, e1 := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(arg))
	if e1 != 0 {
		err = os.NewSyscallError("ioctl", e1)
	}
	return
}

// NewIfreq creates an Ifreq with the input network interface name after
// validating the name does not exceed IFNAMSIZ-1 (trailing NULL required)
// bytes.
func NewIfreq(name string) (*Ifreq, error) {
	// Leave room for terminating NULL byte.
	if len(name) >= syscall.IFNAMSIZ {
		return nil, syscall.EINVAL
	}

	var ifr Ifreq
	copy(ifr.Ifrn[:], name)

	return &ifr, nil
}

// TODO(mdlayher): get/set methods for hardware address sockaddr, char array, etc.

// Name returns the interface name associated with the Ifreq.
func (ifr *Ifreq) Name() string {
	return ByteSliceToString(ifr.Ifrn[:])
}

// ByteSliceToString returns a string form of the text represented by the slice s, with a terminating NUL and any
// bytes after the NUL removed.
func ByteSliceToString(s []byte) string {
	if i := bytes.IndexByte(s, 0); i != -1 {
		s = s[:i]
	}
	return string(s)
}

// According to netdevice(7), only AF_INET addresses are returned for numerous
// sockaddr ioctls. For convenience, we expose these as Inet4Addr since the Port
// field and other data is always empty.

// Inet4Addr returns the Ifreq union data from an embedded sockaddr as a C
// in_addr/Go []byte (4-byte IPv4 address) value. If the sockaddr family is not
// AF_INET, an error is returned.
func (ifr *Ifreq) Inet4Addr() ([]byte, error) {
	raw := *(*syscall.RawSockaddrInet4)(unsafe.Pointer(&ifr.Ifru[:syscall.SizeofSockaddrInet4][0]))
	if raw.Family != syscall.AF_INET {
		// Cannot safely interpret raw.Addr bytes as an IPv4 address.
		return nil, syscall.EINVAL
	}

	return raw.Addr[:], nil
}

// SetInet4Addr sets a C in_addr/Go []byte (4-byte IPv4 address) value in an
// embedded sockaddr within the Ifreq's union data. v must be 4 bytes in length
// or an error will be returned.
func (ifr *Ifreq) SetInet4Addr(v []byte) error {
	if len(v) != 4 {
		return syscall.EINVAL
	}

	var addr [4]byte
	copy(addr[:], v)

	ifr.Clear()
	*(*syscall.RawSockaddrInet4)(
		unsafe.Pointer(&ifr.Ifru[:syscall.SizeofSockaddrInet4][0]),
	) = syscall.RawSockaddrInet4{
		// Always set IP family as ioctls would require it anyway.
		Family: syscall.AF_INET,
		Addr:   addr,
	}

	return nil
}

// Uint16 returns the Ifreq union data as a C short/Go uint16 value.
func (ifr *Ifreq) Uint16() uint16 {
	return *(*uint16)(unsafe.Pointer(&ifr.Ifru[:2][0]))
}

// SetUint16 sets a C short/Go uint16 value as the Ifreq's union data.
func (ifr *Ifreq) SetUint16(v uint16) {
	ifr.Clear()
	*(*uint16)(unsafe.Pointer(&ifr.Ifru[:2][0])) = v
}

// Uint32 returns the Ifreq union data as a C int/Go uint32 value.
func (ifr *Ifreq) Uint32() uint32 {
	return *(*uint32)(unsafe.Pointer(&ifr.Ifru[:4][0]))
}

// SetUint32 sets a C int/Go uint32 value as the Ifreq's union data.
func (ifr *Ifreq) SetUint32(v uint32) {
	ifr.Clear()
	*(*uint32)(unsafe.Pointer(&ifr.Ifru[:4][0])) = v
}

// clear zeroes the ifreq's union field to prevent trailing garbage data from
// being sent to the kernel if an ifreq is reused.
func (ifr *Ifreq) Clear() {
	for i := range ifr.Ifru {
		ifr.Ifru[i] = 0
	}
}

// TODO(mdlayher): export as IfreqData? For now we can provide helpers such as
// IoctlGetEthtoolDrvinfo which use these APIs under the hood.

// An ifreqData is an Ifreq which carries pointer data. To produce an ifreqData,
// use the Ifreq.withData method.
type ifreqData struct {
	name [syscall.IFNAMSIZ]byte
	// A type separate from ifreq is required in order to comply with the
	// unsafe.Pointer rules since the "pointer-ness" of data would not be
	// preserved if it were cast into the byte array of a raw ifreq.
	data unsafe.Pointer
	// Pad to the same size as ifreq.
	_ [len(Ifreq{}.Ifru) - SizeofPtr]byte
}

// withData produces an ifreqData with the pointer p set for ioctls which require
// arbitrary pointer data.
func (ifr Ifreq) withData(p unsafe.Pointer) ifreqData {
	return ifreqData{
		name: ifr.Ifrn,
		data: p,
	}
}
