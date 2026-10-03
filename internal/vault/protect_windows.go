//go:build windows

package vault

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

func protect(data []byte, decrypt bool) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty protected value")
	}
	input := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var output windows.DataBlob
	var err error
	if decrypt {
		err = windows.CryptUnprotectData(&input, nil, nil, 0, nil, 1, &output)
	} else {
		err = windows.CryptProtectData(&input, nil, nil, 0, nil, 1, &output)
	}
	if err != nil {
		return nil, errors.New("windows credential protection failed")
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(output.Data)))
	if output.Size > 65536 {
		return nil, errors.New("protected credential exceeds limit")
	}
	return append([]byte(nil), unsafe.Slice(output.Data, int(output.Size))...), nil
}
func seal(data []byte) ([]byte, error)   { return protect(data, false) }
func unseal(data []byte) ([]byte, error) { return protect(data, true) }
