package platform

import (
	"errors"
	"fmt"
	"os"
)

// daemonLogMaxSize bounds nyist-daemon.log. At most one rotated file is kept.
const daemonLogMaxSize int64 = 1 << 20 // 1 MiB

// rotateLogFile renames a log that exceeds maxSize to path+".1", replacing an
// older rotated file. Renaming keeps the inode of the old log alive for any
// process that already has it open, so existing descriptors are not disturbed.
// It refuses symlinks and never truncates the live file.
func rotateLogFile(path string, maxSize int64) error {
	if maxSize <= 0 {
		return errors.New("日志大小上限必须为正数")
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("日志文件是符号链接，拒绝轮转")
	}
	if !info.Mode().IsRegular() {
		return errors.New("日志路径不是普通文件，拒绝轮转")
	}
	if info.Size() <= maxSize {
		return nil
	}
	rotated := path + ".1"
	if err := os.Rename(path, rotated); err != nil {
		// Another starting daemon may have rotated it first. Starting fresh is
		// still correct and safe in that case.
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("无法轮转日志: %w", err)
	}
	// Rename preserves the inode mode; normalize when an older install left a
	// broader mode behind. Reopen with O_NOFOLLOW so a raced-in symlink cannot
	// redirect the chmod, and O_APPEND so the rotated contents are untouched.
	f, err := openAppendNoFollow(rotated)
	if err != nil {
		return fmt.Errorf("无法修正轮转日志权限: %w", err)
	}
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return fmt.Errorf("无法修正轮转日志权限: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("无法修正轮转日志权限: %w", err)
	}
	return nil
}

// openDaemonLog rotates if needed, then opens the live log for append without
// following symlinks. Rotation and opening are serialized by a small lock file
// so two simultaneous daemon starts cannot rotate each other's fresh log.
// O_APPEND keeps concurrent writers from overwriting each other's records.
func openDaemonLog(path string) (*os.File, error) {
	var log *os.File
	err := withLogLock(path, func() error {
		if err := rotateLogFile(path, daemonLogMaxSize); err != nil {
			return err
		}
		f, err := openAppendNoFollow(path)
		if err != nil {
			return err
		}
		// New files are already created 0600; enforce it for logs left by
		// older versions as well.
		if err := f.Chmod(0600); err != nil {
			f.Close()
			return err
		}
		log = f
		return nil
	})
	if err != nil {
		return nil, err
	}
	return log, nil
}
