package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/koard-ai/koard/internal/core/reviewmode"
)

// configDirEnv overrides the directory that holds the global review mode.
const configDirEnv = "KOARD_CONFIG_DIR"

const reviewMutationLock = ".review-mutation.lock"

const reviewModeUsage = "usage: koard review mode <status|enable|disable> [--scope clone|global]"
const reviewConsentUsage = "usage: koard review consent <status|grant|decline> --workflow <id>"
const reviewRunUsage = "usage: koard review run --workflow <id>"
const reviewStatusUsage = "usage: koard review status --workflow <id>"
const reviewCorrectionUsage = "usage: koard review correction <status|grant|decline> --workflow <id>"
const reviewApplyUsage = "usage: koard review apply --workflow <id>"
const reviewUsage = reviewModeUsage +
	"\n       koard review consent <status|grant|decline> --workflow <id>" +
	"\n       koard review run --workflow <id>" +
	"\n       koard review status --workflow <id>" +
	"\n       koard review correction <status|grant|decline> --workflow <id>" +
	"\n       koard review apply --workflow <id>"

// usageError marks a malformed command line, which exits with code 2.
type usageError struct{ message string }

func (err usageError) Error() string { return err.message }

func runReview(args []string, cwd string, stdout, stderr io.Writer) int {
	if err := review(args, cwd, stdout); err != nil {
		fmt.Fprintf(stderr, "koard review: %v\n", err)
		var usage usageError
		if errors.As(err, &usage) {
			usageText := reviewUsage
			if len(args) > 0 {
				switch args[0] {
				case "mode":
					usageText = reviewModeUsage
				case "consent":
					usageText = reviewConsentUsage
				case "run":
					usageText = reviewRunUsage
				case "status":
					usageText = reviewStatusUsage
				case "correction":
					usageText = reviewCorrectionUsage
				case "apply":
					usageText = reviewApplyUsage
				}
			}
			fmt.Fprintln(stderr, usageText)
			return 2
		}
		return 1
	}
	return 0
}

func review(args []string, cwd string, stdout io.Writer) error {
	if len(args) == 0 {
		return usageError{"expected the 'mode' or 'consent' subcommand"}
	}
	switch args[0] {
	case "mode":
		return reviewMode(args[1:], cwd, stdout)
	case "consent":
		return reviewConsent(args[1:], cwd, stdout)
	case "run":
		return reviewRun(args[1:], cwd, stdout)
	case "status":
		return reviewStatus(args[1:], cwd, stdout)
	case "correction":
		return reviewCorrection(args[1:], cwd, stdout)
	case "apply":
		return reviewApply(args[1:], cwd, stdout)
	default:
		return usageError{fmt.Sprintf("unknown review subcommand %q", args[0])}
	}
}

func reviewMode(args []string, cwd string, stdout io.Writer) error {
	if len(args) == 0 {
		return usageError{"expected status, enable, or disable"}
	}

	action, flags := args[0], args[1:]
	switch action {
	case "status":
		if len(flags) != 0 {
			return usageError{"status takes no arguments"}
		}
		locations, err := reviewLocations(cwd)
		if err != nil {
			return err
		}
		status, err := reviewmode.Read(locations)
		if err != nil {
			return err
		}
		printReviewStatus(stdout, status)
		return nil
	case "enable", "disable":
		scope, err := parseScopeFlags(flags)
		if err != nil {
			return err
		}
		mode := reviewmode.On
		if action == "disable" {
			mode = reviewmode.Off
		}
		return setReviewMode(cwd, scope, mode, stdout)
	default:
		return usageError{fmt.Sprintf("unknown review mode action %q", action)}
	}
}

func parseScopeFlags(flags []string) (reviewmode.Scope, error) {
	scope := reviewmode.ScopeClone
	for index := 0; index < len(flags); index++ {
		var value string
		switch flag := flags[index]; {
		case flag == "--scope":
			if index+1 == len(flags) {
				return "", usageError{"--scope needs a value"}
			}
			index++
			value = flags[index]
		case strings.HasPrefix(flag, "--scope="):
			value = strings.TrimPrefix(flag, "--scope=")
		default:
			return "", usageError{fmt.Sprintf("unexpected argument %q", flag)}
		}
		parsed, err := reviewmode.ParseScope(value)
		if err != nil {
			return "", usageError{err.Error()}
		}
		scope = parsed
	}
	return scope, nil
}

func setReviewMode(cwd string, scope reviewmode.Scope, mode reviewmode.Mode, stdout io.Writer) error {
	locations, err := reviewLocations(cwd)
	if err != nil {
		return err
	}
	return withReviewMutationLock(func() error {
		// Validate both settings before writing, so a corrupt file is reported
		// instead of being hidden behind a successful change.
		if _, err := reviewmode.Read(locations); err != nil {
			return err
		}
		if scope == reviewmode.ScopeClone {
			if err := requireKoardProject(cwd); err != nil {
				return err
			}
		}
		if err := reviewmode.Write(locations.Path(scope), mode); err != nil {
			return err
		}

		status, err := reviewmode.Read(locations)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Review mode set to %s at %s scope.\n", mode, scope)
		printReviewStatus(stdout, status)
		if status.Effective != mode {
			fmt.Fprintf(stdout, "The %s scope keeps review mode %s.\n", status.DecidedBy, status.Effective)
		}
		return nil
	})
}

// withReviewMutationLock gives review-mode changes and consent decisions one
// cross-process serialization point. A stale lock fails closed and requires
// explicit cleanup instead of guessing that no operation is active.
func withReviewMutationLock(action func() error) (result error) {
	directory, err := globalConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create review state directory: %w", err)
	}
	lock := filepath.Join(directory, reviewMutationLock)
	if err := os.Mkdir(lock, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.New("another review mode or consent operation is in progress")
		}
		return fmt.Errorf("lock review state: %w", err)
	}
	defer func() {
		if err := os.Remove(lock); err != nil {
			result = errors.Join(result, fmt.Errorf("unlock review state: %w", err))
		}
	}()
	return action()
}

func printReviewStatus(stdout io.Writer, status reviewmode.Status) {
	fmt.Fprintf(stdout, "Review mode: %s (decided by %s)\n", status.Effective, status.DecidedBy)
	fmt.Fprintf(stdout, "  global: %s\n", describeSetting(status.Global))
	fmt.Fprintf(stdout, "  clone: %s\n", describeSetting(status.Clone))
}

func describeSetting(value *reviewmode.Mode) string {
	if value == nil {
		return "unset"
	}
	return string(*value)
}

func reviewLocations(cwd string) (reviewmode.Locations, error) {
	configDir, err := globalConfigDir()
	if err != nil {
		return reviewmode.Locations{}, err
	}
	return reviewmode.Locations{
		Global: filepath.Join(configDir, reviewmode.FileName),
		Clone:  filepath.Join(cwd, ".koard", "state", reviewmode.FileName),
	}, nil
}

func globalConfigDir() (string, error) {
	if dir := os.Getenv(configDirEnv); dir != "" {
		if !filepath.IsAbs(dir) {
			return "", fmt.Errorf("%s must be an absolute path, got %q", configDirEnv, dir)
		}
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate the user configuration directory: %w; set %s", err, configDirEnv)
	}
	return filepath.Join(base, "koard"), nil
}

func requireKoardProject(cwd string) error {
	info, err := os.Stat(filepath.Join(cwd, ".koard"))
	if err == nil && info.IsDir() {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect Koard project: %w", err)
	}
	return errors.New("clone scope needs a Koard project in the current directory; run 'koard init' or use --scope global")
}
