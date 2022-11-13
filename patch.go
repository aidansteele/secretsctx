package main

import (
	"bytes"
	"fmt"
	"github.com/aidansteele/secretsctx/process"
)

func patchRapid(search, replace []byte) error {
	rapid := process.New(1)

	maps, err := rapid.Maps()
	if err != nil {
		return fmt.Errorf(": %w", err)
	}

	buf := make([]byte, 1<<20) // 1MiB

	for _, mm := range maps {
		// TODO: lambda-spy only checks specific maps, should we be doing that instead?
		// ref: https://github.com/clearvector/lambda-spy/blob/f6c382e3e0b943f62c09b9810a99761343fe411d/src/main.rs#L42-L49
		readwrite := process.PermissionRead | process.PermissionWrite
		if (mm.Permissions & readwrite) != readwrite {
			continue
		}

		offset := mm.Start
		for offset < mm.End {
			buflen := uint64(len(buf))
			if offset+buflen > mm.End {
				buflen = mm.End - offset
			}

			n, err := rapid.ReadAt(buf[:buflen], offset)
			if err != nil {
				return fmt.Errorf("reading memory: %w", err)
			}

			if uint64(n) != buflen {
				return fmt.Errorf("n != buflen: %d != %d", n, buflen)
			}

			index := bytes.Index(buf[:buflen], search)
			if index < 0 {
				offset += buflen
				continue
			}

			found := offset + uint64(index)
			_, err = rapid.WriteAt(replace, found)
			if err != nil {
				return fmt.Errorf(": %w", err)
			}
		}
	}

	return nil
}
