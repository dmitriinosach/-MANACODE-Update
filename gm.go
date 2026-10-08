package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	gmKeyFile  = "gm.txt"
	gmDataFile = "Gm.lua"
)

type gmInfo struct {
	V      int    `json:"v"`
	Zip    string `json:"zip"`
	ZipLen int64  `json:"zipBytes"`
	KeyID  string `json:"keyId"`
	Baked  string `json:"baked"`
}

func gmKey(dir string) string {
	var data []byte
	for _, d := range []string{dir, appHome(), filepath.Dir(dir)} {
		if d == "" {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(d, gmKeyFile)); err == nil {
			data = b
			break
		}
	}
	if data == nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if k := strings.TrimSpace(strings.TrimPrefix(line, string([]byte{0xEF, 0xBB, 0xBF}))); k != "" {
			return k
		}
	}
	return ""
}

func gmKeyID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])[:8]
}

func gmLocalBaked(dir string) string {
	m := reBaked.FindStringSubmatch(tail(filepath.Join(dataDir(dir), gmDataFile), 4096))
	if m == nil {
		return ""
	}
	return m[1]
}

func dropGM(dir string) {
	_ = os.Remove(filepath.Join(dataDir(dir), gmDataFile))
}

func syncGM(dir string, gm *gmInfo, cnt *counter) (string, error) {
	key := gmKey(dir)
	if key == "" {
		dropGM(dir)
		return "", nil
	}
	if gm == nil || gm.Zip == "" {
		return "ГМ-ключ есть, но на сервере ГМ-данных пока нет", nil
	}
	if gm.KeyID != "" && gm.KeyID != gmKeyID(key) {
		dropGM(dir)
		return "", fmt.Errorf("ключ ГМ устарел — возьмите новый gm.txt")
	}
	if local := gmLocalBaked(dir); local != "" && local == gm.Baked {
		return "ГМ-данные от " + bakedRU(local), nil
	}
	cnt.file.Store("ГМ-данные")
	resp, err := request(gm.Zip)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	zip, err := io.ReadAll(&countingReader{r: resp.Body, cnt: cnt})
	if err != nil {
		return "", fmt.Errorf("оборвалось скачивание ГМ-данных")
	}
	_, data, err := unzipFirstAES(zip, key)
	if err != nil {
		dropGM(dir)
		return "", fmt.Errorf("ключ ГМ не подошёл")
	}
	s := strings.TrimSpace(string(data))
	if !strings.HasPrefix(s, "PlayerRaidsGM") || !strings.HasSuffix(s, "}") {
		return "", fmt.Errorf("в архиве ГМ не тот файл")
	}
	if err := writeFile(filepath.Join(dataDir(dir), gmDataFile), data); err != nil {
		return "", err
	}
	return "ГМ-данные от " + bakedRU(gm.Baked), nil
}
