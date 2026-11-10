package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/bodgit/sevenzip"
	"github.com/nwaples/rardecode/v2"
	"github.com/ulikunitz/xz"
)

func customArchiveName(name string) bool {
	for _, suffix := range []string{
		".zip", ".7z", ".rar", ".tar", ".tar.gz", ".tgz", ".tar.bz2", ".tbz2", ".tar.xz", ".txz",
	} {
		if strings.HasSuffix(strings.ToLower(name), suffix) {
			return true
		}
	}
	return false
}

type customArchiveFiles struct {
	files map[string][]byte
	seen  map[string]bool
	total int64
	count int
}

func (a *customArchiveFiles) add(name string, mode fs.FileMode, size int64, reader io.Reader) error {
	a.count++
	if a.count > 20000 {
		return errors.New(tr("в архиве слишком много файлов"))
	}
	name = strings.ReplaceAll(name, "\\", "/")
	for strings.HasPrefix(name, "./") {
		name = strings.TrimPrefix(name, "./")
	}
	if mode.IsDir() && (name == "" || name == ".") {
		return nil
	}
	if err := customPath(name); err != nil {
		return err
	}
	if mode.Type() != 0 && !mode.IsDir() {
		return errors.New(tr("ссылки и специальные файлы внутри архива запрещены"))
	}
	if mode.IsDir() {
		return nil
	}
	low := strings.ToLower(name)
	if a.seen[low] {
		return errors.New(tr("в архиве повторяются имена файлов"))
	}
	a.seen[low] = true
	remaining := int64(customUnpackLimit) - a.total
	if size < 0 || size > remaining {
		return errors.New(tr("распакованный архив слишком большой"))
	}
	body, err := io.ReadAll(io.LimitReader(reader, remaining+1))
	if err != nil {
		return fmt.Errorf(tr("не удалось прочитать %s: %w"), name, err)
	}
	if int64(len(body)) > remaining {
		return errors.New(tr("распакованный архив слишком большой"))
	}
	a.total += int64(len(body))
	a.files[name] = body
	return nil
}

func readCustomArchive(data []byte) (map[string][]byte, error) {
	if len(data) > customZipLimit {
		return nil, errors.New(tr("архив слишком большой"))
	}
	a := &customArchiveFiles{files: map[string][]byte{}, seen: map[string]bool{}}
	var err error
	switch {
	case bytes.HasPrefix(data, []byte("PK")):
		err = a.zip(data)
	case bytes.HasPrefix(data, []byte("7z\xbc\xaf\x27\x1c")):
		err = a.sevenZip(data)
	case bytes.HasPrefix(data, []byte("Rar!\x1a\x07")):
		err = a.rar(data)
	default:
		err = a.tar(data)
	}
	if err != nil {
		return nil, fmt.Errorf(tr("не удалось распаковать архив: %w"), err)
	}
	return a.files, nil
}

func (a *customArchiveFiles) zip(data []byte) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		if f.UncompressedSize64 > customUnpackLimit {
			return errors.New(tr("распакованный архив слишком большой"))
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		err = a.add(f.Name, f.Mode(), int64(f.UncompressedSize64), r)
		r.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *customArchiveFiles) sevenZip(data []byte) error {
	zr, err := sevenzip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		if f.UncompressedSize > customUnpackLimit {
			return errors.New(tr("распакованный архив слишком большой"))
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		err = a.add(f.Name, f.Mode(), int64(f.UncompressedSize), r)
		r.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *customArchiveFiles) rar(data []byte) error {
	r, err := rardecode.NewReader(bytes.NewReader(data), rardecode.MaxDictionarySize(customUnpackLimit))
	if err != nil {
		return err
	}
	for {
		f, err := r.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if f.Encrypted || f.HeaderEncrypted {
			return errors.New(tr("архив защищён паролем"))
		}
		if f.LinkType != rardecode.LinkTypeNone {
			return errors.New(tr("ссылки внутри архива запрещены"))
		}
		if err := a.add(f.Name, f.Mode(), f.UnPackedSize, r); err != nil {
			return err
		}
	}
}

func (a *customArchiveFiles) tar(data []byte) error {
	var reader io.Reader = bytes.NewReader(data)
	switch {
	case bytes.HasPrefix(data, []byte{0x1f, 0x8b}):
		r, err := gzip.NewReader(reader)
		if err != nil {
			return err
		}
		defer r.Close()
		reader = r
	case bytes.HasPrefix(data, []byte("BZh")):
		reader = bzip2.NewReader(reader)
	case bytes.HasPrefix(data, []byte("\xfd7zXZ\x00")):
		r, err := (xz.ReaderConfig{DictCap: customUnpackLimit}).NewReader(reader)
		if err != nil {
			return err
		}
		reader = r
	}
	limited := &io.LimitedReader{R: reader, N: customUnpackLimit + (16 << 20)}
	tarReader := tar.NewReader(limited)
	for {
		f, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			if limited.N <= 0 {
				return errors.New(tr("распакованный архив слишком большой"))
			}
			_, err = io.Copy(io.Discard, limited)
			if limited.N <= 0 {
				return errors.New(tr("распакованный архив слишком большой"))
			}
			return err
		}
		if err != nil {
			return err
		}
		switch f.Typeflag {
		case tar.TypeDir, tar.TypeReg, tar.TypeRegA:
		default:
			return errors.New(tr("ссылки и специальные файлы внутри TAR запрещены"))
		}
		if err := a.add(f.Name, f.FileInfo().Mode(), f.Size, tarReader); err != nil {
			return err
		}
	}
}
