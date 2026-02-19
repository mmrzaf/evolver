package gitops

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func init() {
	_ = exec.Command("git", "config", "user.name", "repo-evolver").Run()
	_ = exec.Command("git", "config", "user.email", "repo-evolver@users.noreply.github.com").Run()
}

// CheckoutNew creates and checks out a new git branch.
func CheckoutNew(branch string) error {
	cmd := exec.Command("git", "checkout", "-b", branch)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ResetHard resets tracked and untracked files in the repository.
func ResetHard() {
	_ = exec.Command("git", "reset", "--hard").Run()
	_ = exec.Command("git", "clean", "-fd").Run()
}

// HasChanges reports whether the working tree has any changes (staged or unstaged).
func HasChanges() (bool, error) {
	out, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// StageAll stages all changes.
func StageAll() error {
	cmd := exec.Command("git", "add", ".")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// DiffStats returns staged file and line-change counts.
func DiffStats() (files, lines int, err error) {
	if err := StageAll(); err != nil {
		return 0, 0, err
	}
	out, err := exec.Command("git", "diff", "--cached", "--numstat").Output()
	if err != nil {
		return 0, 0, err
	}

	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 3 {
			files++
			add, _ := strconv.Atoi(parts[0])
			del, _ := strconv.Atoi(parts[1])
			lines += add + del
		}
	}
	return files, lines, nil
}

// NewFilesCount returns how many files are staged as newly added.
func NewFilesCount() (int, error) {
	if err := StageAll(); err != nil {
		return 0, err
	}
	out, err := exec.Command("git", "diff", "--cached", "--name-status", "--diff-filter=A").Output()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		count++
	}
	return count, nil
}

// Commit creates a commit from the current working tree.
func Commit(msg string) error {
	if err := StageAll(); err != nil {
		return err
	}
	cmd := exec.Command("git", "commit", "-m", msg)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Push pushes the given target ref to origin.
func Push(target string) error {
	cmd := exec.Command("git", "push", "origin", target)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
