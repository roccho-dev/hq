package worker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const WorkspaceApprovalLockFilename = "approvals.lock"

type workspaceApprovalLock struct {
	file *os.File
}

func acquireWorkspaceApprovalLock(workspaceRoot string) (*workspaceApprovalLock, error) {
	path := filepath.Join(filepath.Clean(workspaceRoot), ".hq", WorkspaceApprovalLockFilename)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create approval lock directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open approval lock: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure approval lock: %w", err)
	}
	if err := lockWorkspaceApprovalFile(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("acquire approval lock: %w", err)
	}
	return &workspaceApprovalLock{file: file}, nil
}

func (lock *workspaceApprovalLock) release() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	unlockErr := unlockWorkspaceApprovalFile(lock.file)
	closeErr := lock.file.Close()
	lock.file = nil
	return errors.Join(unlockErr, closeErr)
}
