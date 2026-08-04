//go:build linux

package services

import (
	"fmt"
	"os"
)

// atomicRename 使用 Linux 的 POSIX 原子重命名替换目标文件。
func atomicRename(src, dst string) error {
	if err := os.Rename(src, dst); err != nil {
		_ = os.Remove(src)
		return fmt.Errorf("原子替换失败 %s -> %s: os.Rename 失败: %w", src, dst, err)
	}
	return nil
}
