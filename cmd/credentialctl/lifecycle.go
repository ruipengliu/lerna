package main

import (
	"context"
	"lerna/adapters/credentialbackups"
	"lerna/adapters/credentialhttp"
	"lerna/credentials"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type renewalConfiguration struct {
	ProviderID, RenewPath, InspectPath string
	AllowLoopbackHTTP, ReplaySafe      bool
}
type managementAction struct{ Command, Operation, Ref, KeyVersion string }

func isLifecycleCommand(command string) bool {
	switch command {
	case "rotate-key", "rotation-step", "backup", "dispose-backup", "retire-key", "renew", "renewal-step":
		return true
	default:
		return false
	}
}
func lifecycleCommand(ctx context.Context, manager *credentials.Lifecycle, token string, cfg configuration, action managementAction) (any, error) {
	switch action.Command {
	case "rotate-key":
		return manager.StartRotation(ctx, token, action.Operation, cfg.Binding)
	case "rotation-step":
		return manager.StepRotation(ctx, token, action.Operation, cfg.Binding)
	case "retire-key":
		if e := manager.RetireKey(ctx, token, action.KeyVersion, cfg.Binding); e != nil {
			return nil, e
		}
		return struct{ KeyVersion, Status string }{action.KeyVersion, "retired"}, nil
	case "backup", "dispose-backup":
		if cfg.BackupDirectory == "" || cfg.ArchiveID == "" {
			return nil, credentials.Invalid
		}
		if action.Command == "backup" {
			if e := os.Mkdir(cfg.BackupDirectory, 0700); e != nil && !os.IsExist(e) {
				return nil, credentials.Unavailable
			}
		}
		archivePath, e := filepath.EvalSymlinks(cfg.BackupDirectory)
		if e != nil {
			return nil, credentials.Unavailable
		}
		keyPath, e := filepath.EvalSymlinks(cfg.KeyDirectory)
		if e != nil {
			return nil, credentials.Unavailable
		}
		archivePath, e = filepath.Abs(archivePath)
		if e != nil {
			return nil, credentials.Invalid
		}
		keyPath, e = filepath.Abs(keyPath)
		if e != nil {
			return nil, credentials.Invalid
		}
		rel, e := filepath.Rel(keyPath, archivePath)
		if e != nil || rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return nil, credentials.Denied
		}
		archive, e := credentialbackups.Open(cfg.BackupDirectory, cfg.ArchiveID)
		if e != nil {
			return nil, e
		}
		defer archive.Close()
		manager, e = manager.WithArchive(archive)
		if e != nil {
			return nil, e
		}
		if action.Command == "backup" {
			return manager.CreateBackup(ctx, token, action.Operation, cfg.Binding)
		}
		return manager.DisposeBackup(ctx, token, action.Operation, cfg.Binding)
	case "renew", "renewal-step":
		if cfg.Renewal == nil {
			return nil, credentials.Invalid
		}
		p, e := credentialhttp.NewRenewal(credentialhttp.RenewalConfig{Binding: cfg.Binding, ProviderID: cfg.Renewal.ProviderID, RenewPath: cfg.Renewal.RenewPath, InspectPath: cfg.Renewal.InspectPath, Timeout: time.Second, AllowLoopbackHTTP: cfg.Renewal.AllowLoopbackHTTP, ReplaySafe: cfg.Renewal.ReplaySafe})
		if e != nil {
			return nil, e
		}
		defer p.Close()
		manager, e = manager.WithProvider(p)
		if e != nil {
			return nil, e
		}
		if action.Command == "renew" {
			return manager.StartRenewal(ctx, token, action.Operation, action.Ref, cfg.Binding)
		}
		return manager.StepRenewal(ctx, token, action.Operation, cfg.Binding)
	default:
		return nil, credentials.Invalid
	}
}
