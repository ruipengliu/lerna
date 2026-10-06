//go:build !darwin

package egressio

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type FileContent interface {
	BindFileRoot(context.Context, *v1.Caller, *v1.BindFileRootCommand) (*v1.CommandReceipt, error)
	CheckUsable(context.Context, *v1.Caller, *v1.Ref) error
	QueryFileResources(context.Context, *v1.Caller, *v1.Ref) (*v1.FileResources, error)
}

type FileLedger interface {
	QueryFilePublication(context.Context, *v1.Caller, *v1.FileCommit) (*v1.RawObservation, error)
	CheckFileCleanup(context.Context, *v1.Caller, *v1.FileResources) error
}
type Files struct{}

func NewFiles(map[string]string, FileLedger, FileContent) *Files { return &Files{} }
func (*Files) Perform(context.Context, *v1.PhysicalIORequest) (*v1.PhysicalIOResult, error) {
	return nil, command.Fail("FILE_PLATFORM_UNQUALIFIED")
}
func (*Files) PerformChecked(context.Context, *v1.PhysicalIORequest, func(context.Context) error) (*v1.PhysicalIOResult, error) {
	return nil, command.Fail("FILE_PLATFORM_UNQUALIFIED")
}
