package harness

import (
	"errors"
	"io"
	"os"
)

// 原子替换只负责准确字节持久化；校验、容量和原身份锁仍归各 Journal。
func writeDurableFile(root *os.Root, temporary, name string, body []byte) (err error) {
	f, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() {
		removed := root.Remove(temporary)
		if !errors.Is(removed, os.ErrNotExist) {
			err = errors.Join(err, removed)
		}
	}()
	n, err := f.Write(body)
	if err == nil && n != len(body) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = root.Rename(temporary, name); err != nil {
		return err
	}
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}
