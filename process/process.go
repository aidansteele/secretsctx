package process

import (
	"bufio"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"strconv"
	"strings"
)

type Process struct {
	pid int
}

// it's a shame that io.ReaderAt/io.WriterAt expect int64 and not uint64

func New(pid int) *Process {
	return &Process{pid: pid}
}

func (p *Process) Maps() ([]Map, error) {
	mapsFile, err := os.Open(fmt.Sprintf("/proc/%d/maps", p.pid))
	if err != nil {
		return nil, fmt.Errorf("opening maps: %w", err)
	}

	maps := []Map{}

	scan := bufio.NewScanner(mapsFile)
	for scan.Scan() {
		f := strings.Fields(scan.Text())
		addr, permStr, offsetHex, dev, inodeDec, pathname := f[0], f[1], f[2], f[3], f[4], ""
		if len(f) >= 6 {
			pathname = f[5]
		}

		startHex, endHex, _ := strings.Cut(addr, "-")
		start, err := strconv.ParseUint(startHex, 16, 64)
		if err != nil {
			return nil, fmt.Errorf("parsing addr start: %w", err)
		}

		end, err := strconv.ParseUint(endHex, 16, 64)
		if err != nil {
			return nil, fmt.Errorf("parsing addr end: %w", err)
		}

		offset, err := strconv.ParseUint(offsetHex, 16, 64)
		if err != nil {
			return nil, fmt.Errorf("parsing offset: %w", err)
		}

		// TODO: is it base 10 or base 16?
		inode, err := strconv.ParseUint(inodeDec, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parsing inode: %w", err)
		}

		var perms Permission
		if strings.ContainsRune(permStr, 'r') {
			perms |= PermissionRead
		}
		if strings.ContainsRune(permStr, 'w') {
			perms |= PermissionWrite
		}
		if strings.ContainsRune(permStr, 'x') {
			perms |= PermissionExecute
		}
		if strings.ContainsRune(permStr, 's') {
			perms |= PermissionShared
		}
		if strings.ContainsRune(permStr, 'p') {
			perms |= PermissionPrivate
		}

		maps = append(maps, Map{
			Start:       start,
			End:         end,
			Permissions: perms,
			Offset:      offset,
			Device:      dev,
			Inode:       inode,
			Path:        pathname,
		})
	}

	return maps, nil
}

func (p *Process) ReadAt(b []byte, off uint64) (n int, err error) {
	localIov := [1]unix.Iovec{
		{Base: &b[0]},
	}
	localIov[0].SetLen(len(b))
	remoteIov := [1]unix.RemoteIovec{
		{Base: uintptr(off), Len: len(b)},
	}

	n, err = unix.ProcessVMReadv(p.pid, localIov[:], remoteIov[:], 0)
	return n, err
}

func (p *Process) WriteAt(b []byte, off uint64) (n int, err error) {
	localIov := [1]unix.Iovec{
		{Base: &b[0]},
	}
	localIov[0].SetLen(len(b))
	remoteIov := [1]unix.RemoteIovec{
		{Base: uintptr(off), Len: len(b)},
	}

	n, err = unix.ProcessVMWritev(p.pid, localIov[:], remoteIov[:], 0)
	return n, err
}

type Map struct {
	Start       uint64
	End         uint64
	Permissions Permission
	Offset      uint64
	Device      string
	Inode       uint64
	Path        string
}

type Permission uint8

const (
	PermissionRead Permission = 1 << iota
	PermissionWrite
	PermissionExecute
	PermissionShared
	PermissionPrivate
)
