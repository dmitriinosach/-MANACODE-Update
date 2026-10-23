package main

import (
	"bytes"
	"compress/flate"
	"crypto/aes"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"io"
)

var errPassword = errors.New("архив не открылся паролем — нужна свежая обновлялка")

func winzipCTR(key []byte, data []byte) error {
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	var counter [16]byte
	var stream [16]byte
	for off := 0; off < len(data); off += 16 {
		for i := 0; i < 16; i++ {
			counter[i]++
			if counter[i] != 0 {
				break
			}
		}
		block.Encrypt(stream[:], counter[:])
		end := off + 16
		if end > len(data) {
			end = len(data)
		}
		for i := off; i < end; i++ {
			data[i] ^= stream[i-off]
		}
	}
	return nil
}

func unzipFirstAES(zip []byte, password string) (string, []byte, error) {
	if len(zip) < 30 || binary.LittleEndian.Uint32(zip) != 0x04034b50 {
		return "", nil, errors.New("скачался не архив")
	}
	method := binary.LittleEndian.Uint16(zip[8:])
	csize := int(binary.LittleEndian.Uint32(zip[18:]))
	nlen := int(binary.LittleEndian.Uint16(zip[26:]))
	elen := int(binary.LittleEndian.Uint16(zip[28:]))
	start := 30 + nlen + elen
	if method != 99 || start+csize > len(zip) || csize < 28 {
		return "", nil, errors.New("архив повреждён")
	}
	name := string(zip[30 : 30+nlen])
	data := zip[start : start+csize]
	salt, verify := data[:16], data[16:18]
	body := append([]byte(nil), data[18:len(data)-10]...)
	tag := data[len(data)-10:]
	k, err := pbkdf2.Key(sha1.New, password, salt, 1000, 66)
	if err != nil {
		return "", nil, err
	}
	if !bytes.Equal(k[64:66], verify) {
		return "", nil, errPassword
	}
	mac := hmac.New(sha1.New, k[32:64])
	mac.Write(body)
	if !hmac.Equal(mac.Sum(nil)[:10], tag) {
		return "", nil, errors.New("архив повреждён при скачивании")
	}
	if err := winzipCTR(k[:32], body); err != nil {
		return "", nil, err
	}
	plain, err := io.ReadAll(flate.NewReader(bytes.NewReader(body)))
	if err != nil {
		return "", nil, errors.New("архив не распаковался")
	}
	return name, plain, nil
}
