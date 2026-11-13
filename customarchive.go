package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bodgit/sevenzip"
	"github.com/nwaples/rardecode/v2"
	"github.com/ulikunitz/xz"
)

const (
	customDictLimit = 128 << 20
	customDiskSpare = 256 << 20
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

func customDiskFull() error {
	return errors.New(tr("на диске не хватает места для архива"))
}

func customDiskBudget(dir string) (int64, error) {
	free, err := diskFree(dir)
	if err != nil {
		return 0, err
	}
	return free - customDiskSpare, nil
}

type customArchiveFiles struct {
	dst    string
	files  map[string]string
	seen   map[string]bool
	budget int64
	total  int64
	count  int
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
	remaining := a.budget - a.total
	if size > remaining {
		return customDiskFull()
	}
	dst := filepath.Join(a.dst, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(reader, max(remaining, 0)+1))
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	a.total += n
	if err != nil {
		return fmt.Errorf(tr("не удалось прочитать %s: %w"), name, err)
	}
	if n > remaining {
		return customDiskFull()
	}
	a.files[name] = dst
	return nil
}

func readCustomArchive(src, dst string) (map[string]string, error) {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return nil, err
	}
	budget, err := customDiskBudget(dst)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 8)
	n, _ := io.ReadFull(f, buf)
	head := string(buf[:n])
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	a := &customArchiveFiles{dst: dst, files: map[string]string{}, seen: map[string]bool{}, budget: budget}
	switch {
	case strings.HasPrefix(head, "PK"):
		err = a.zip(f, st.Size())
	case strings.HasPrefix(head, "7z\xbc\xaf\x27\x1c"):
		err = a.sevenZip(f, st.Size())
	case strings.HasPrefix(head, "Rar!\x1a\x07"):
		err = a.rar(f)
	default:
		err = a.tar(f, head)
	}
	if err != nil {
		return nil, fmt.Errorf(tr("не удалось распаковать архив: %w"), err)
	}
	return a.files, nil
}

func (a *customArchiveFiles) declared(sizes []uint64) error {
	var sum uint64
	for _, s := range sizes {
		sum += s
		if a.budget < 0 || sum > uint64(a.budget) {
			return customDiskFull()
		}
	}
	return nil
}

func (a *customArchiveFiles) zip(src io.ReaderAt, size int64) error {
	zr, err := zip.NewReader(src, size)
	if err != nil {
		return err
	}
	sizes := make([]uint64, 0, len(zr.File))
	for _, f := range zr.File {
		sizes = append(sizes, f.UncompressedSize64)
	}
	if err := a.declared(sizes); err != nil {
		return err
	}
	for _, f := range zr.File {
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

func (a *customArchiveFiles) sevenZip(src io.ReaderAt, size int64) error {
	zr, err := sevenzip.NewReader(src, size)
	if err != nil {
		return err
	}
	sizes := make([]uint64, 0, len(zr.File))
	for _, f := range zr.File {
		sizes = append(sizes, f.UncompressedSize)
	}
	if err := a.declared(sizes); err != nil {
		return err
	}
	for _, f := range zr.File {
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

func (a *customArchiveFiles) rar(src io.Reader) error {
	r, err := rardecode.NewReader(bufio.NewReader(src), rardecode.MaxDictionarySize(customDictLimit))
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

func (a *customArchiveFiles) tar(src io.Reader, head string) error {
	var reader io.Reader = bufio.NewReader(src)
	switch {
	case strings.HasPrefix(head, "\x1f\x8b"):
		r, err := gzip.NewReader(reader)
		if err != nil {
			return err
		}
		defer r.Close()
		reader = r
	case strings.HasPrefix(head, "BZh"):
		reader = bzip2.NewReader(reader)
	case strings.HasPrefix(head, "\xfd7zXZ\x00"):
		r, err := (xz.ReaderConfig{DictCap: customDictLimit}).NewReader(reader)
		if err != nil {
			return err
		}
		reader = r
	}
	tarReader := tar.NewReader(reader)
	for {
		f, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return nil
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
