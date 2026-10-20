package main

import (
	"encoding/base64"
	"unsafe"

	"golang.org/x/sys/windows"
)

func sealMarketToken(token string) (string, error) {
	input := []byte(token)
	if len(input) == 0 {
		return "", nil
	}
	blob := windows.DataBlob{Size: uint32(len(input)), Data: &input[0]}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&blob, nil, nil, 0, nil, 1, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return base64.StdEncoding.EncodeToString(unsafe.Slice(out.Data, out.Size)), nil
}

func openMarketToken(secret string) (string, error) {
	input, err := base64.StdEncoding.DecodeString(secret)
	if err != nil || len(input) == 0 {
		return "", err
	}
	blob := windows.DataBlob{Size: uint32(len(input)), Data: &input[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&blob, nil, nil, 0, nil, 1, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return string(unsafe.Slice(out.Data, out.Size)), nil
}
