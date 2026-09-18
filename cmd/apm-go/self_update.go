package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/apm-go/apm/internal/selfupdate"
	"github.com/apm-go/apm/internal/ux"
	"github.com/spf13/cobra"
)

const (
	selfUpdateAPITimeout      = 30 * time.Second
	selfUpdateDownloadTimeout = 5 * time.Minute
)

func selfUpdateCmd() *cobra.Command {
	var check bool

	cmd := &cobra.Command{
		Use:   "self-update",
		Short: "Update apm-go to the latest release",
		Long: `Update the apm-go binary to the latest stable release from GitHub.

Development builds (version "dev") cannot self-update; use install.sh
or install.ps1 for the initial installation.

Pre-release versions are excluded; only stable releases are considered.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSelfUpdate(check)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "check for updates without applying them")
	return cmd
}

func runSelfUpdate(checkOnly bool) error {
	apiCtx, apiCancel := context.WithTimeout(context.Background(), selfUpdateAPITimeout)
	defer apiCancel()

	spin := ux.Spinner(os.Stderr, "Checking for updates...")

	u := &selfupdate.Updater{
		HTTPClient: &http.Client{Timeout: selfUpdateAPITimeout},
	}

	result, rel, err := u.CheckLatest(apiCtx)
	if err != nil {
		spin.Fail(fmt.Sprintf("Update check failed: %s", err))
		return withSilentExitCode(1, err)
	}

	if result.UpToDate {
		spin.Success(fmt.Sprintf("Already up to date (v%s)", result.Current))
		return nil
	}

	if checkOnly {
		spin.Success(fmt.Sprintf("Update available: v%s -> v%s", result.Current, result.Latest))
		return nil
	}

	spin.Update(fmt.Sprintf("Downloading v%s...", result.Latest))

	dlCtx, dlCancel := context.WithTimeout(context.Background(), selfUpdateDownloadTimeout)
	defer dlCancel()

	u.HTTPClient = &http.Client{Timeout: selfUpdateDownloadTimeout}
	u.OnProgress = func(bytesRead, total int64) {
		if total > 0 {
			pct := float64(bytesRead) / float64(total) * 100
			spin.Update(fmt.Sprintf("Downloading v%s... %s / %s (%.0f%%)",
				result.Latest,
				selfupdate.HumanBytes(bytesRead),
				selfupdate.HumanBytes(total),
				pct))
		} else {
			spin.Update(fmt.Sprintf("Downloading v%s... %s",
				result.Latest,
				selfupdate.HumanBytes(bytesRead)))
		}
	}

	binaryPath, err := u.Apply(dlCtx, rel)
	if err != nil {
		spin.Fail(fmt.Sprintf("Update failed: %s", err))
		return withSilentExitCode(1, err)
	}

	spin.Success(fmt.Sprintf("Updated v%s -> v%s", result.Current, result.Latest))
	ux.Info(os.Stderr, "Binary: %s", binaryPath)

	return nil
}
