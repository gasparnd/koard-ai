package cli

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

//go:embed templates/skill.md
var skillTemplate string

//go:embed templates/skill-onboarding.md
var skillOnboardingTemplate string

var draftName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func runSkillCreate(args []string, cwd string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprintln(stdout, "Usage: koard skill create <name>\nCreate an inactive draft in .koard/skill-drafts/skills/<name> with its onboarding guide.\nUse a lowercase hyphenated name (at most 64 characters). No options or overwrites.")
		return 0
	}
	if len(args) != 1 || len(args[0]) > 64 || !draftName.MatchString(args[0]) {
		fmt.Fprintln(stderr, "koard skill create: expected one lowercase hyphenated name (at most 64 characters), not a path or option")
		return 2
	}
	if err := createSkillDraft(cwd, args[0]); err != nil {
		fmt.Fprintf(stderr, "koard skill create: %v; inspect any partial draft before retrying\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Draft created: .koard/skill-drafts/skills/%s\nRead README.md there, complete the TODOs, validate and review before manual activation.\nThe draft is not in this project's automatic skill catalog.\n", args[0])
	return 0
}

func createSkillDraft(cwd, name string) error {
	root, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return err
	}
	// Reject existing symlinks under the selected workspace. As with discovery,
	// this is not a sandbox against concurrent hostile directory replacement.
	for _, part := range []string{".koard", "skill-drafts", "skills"} {
		root = filepath.Join(root, part)
		if err := os.Mkdir(root, 0755); err != nil && !os.IsExist(err) {
			return err
		}
		info, err := os.Lstat(root)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("draft parent must be a real directory: %s", root)
		}
	}
	dir := filepath.Join(root, name)
	// Claim a fresh directory; never merge into an existing or interrupted draft.
	if err := os.Mkdir(dir, 0755); err != nil {
		return err
	}
	for _, file := range []struct{ name, body string }{
		{"SKILL.md", skillTemplate}, {"README.md", skillOnboardingTemplate},
	} {
		f, err := os.OpenFile(filepath.Join(dir, file.name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			return err
		}
		_, writeErr := io.WriteString(f, strings.ReplaceAll(file.body, "{{name}}", name))
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
