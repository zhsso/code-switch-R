//go:build linux

package services

import (
	"fmt"
	"os"
	"path/filepath"
)

// atomicRename 使用 Linux 的 POSIX 原子重命名替换目标文件。
func atomicRename(src, dst string) error {
	if err := os.Rename(src, dst); err != nil {
		_ = os.Remove(src)
		return fmt.Errorf("原子替换失败 %s -> %s: os.Rename 失败: %w", src, dst, err)
	}
	dir, err := os.Open(filepath.Dir(dst))
	if err != nil {
		return fmt.Errorf("打开父目录以同步重命名 %s: %w", dst, err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("同步父目录以持久化重命名 %s: %w", dst, err)
	}
	return nil
}
