package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Local struct {
	root string
}

func NewLocal(root string) (*Local, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(root, 0700); err != nil {
		return nil, err
	}
	return &Local{root: root}, nil
}

func (s *Local) path(key string) (string, error) {
	if !validKey(key) {
		return "", errors.New("invalid media object key")
	}
	return filepath.Join(s.root, filepath.FromSlash(key)), nil
}

func (s *Local) Put(ctx context.Context, key string, file *os.File, size int64) error {
	path, err := s.path(key)
	if err != nil {
		return err
	}
	if size < 0 {
		return errors.New("negative media size")
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() != size {
		return fmt.Errorf("media size changed before storage")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".media-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if err := temp.Chmod(0600); err != nil {
		return err
	}
	buffer := make([]byte, 64<<10)
	var written int64
	for written < size {
		if err := ctx.Err(); err != nil {
			return err
		}
		want := int64(len(buffer))
		if size-written < want {
			want = size - written
		}
		n, readErr := file.Read(buffer[:want])
		if n > 0 {
			if _, err := temp.Write(buffer[:n]); err != nil {
				return err
			}
			written += int64(n)
		}
		if readErr != nil {
			return readErr
		}
		if n == 0 {
			return io.ErrNoProgress
		}
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

type sectionReadCloser struct {
	io.Reader
	io.Closer
}

func (s *Local) Open(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := s.path(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if length < 0 {
		return f, nil
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if offset < 0 || length < 0 || offset > info.Size() || length > info.Size()-offset {
		f.Close()
		return nil, errors.New("media range exceeds file size")
	}
	return sectionReadCloser{Reader: io.NewSectionReader(f, offset, length), Closer: f}, nil
}

func (s *Local) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
