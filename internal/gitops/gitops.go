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
	return exec.Command("git", "checkout", "-b", branch).Run()
}

// ResetHard resets tracked and untracked files in the repository.
func ResetHard() {
	_ = exec.Command("git", "reset", "--hard").Run()
	_ = exec.Command("git", "clean", "-fd").Run()
}

// DiffStats returns staged file and line-change counts.
func DiffStats() (files, lines int, err error) {
	_ = exec.Command("git", "add", ".").Run()
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

// Commit creates a commit from the current working tree.
func Commit(msg string) error {
	_ = exec.Command("git", "add", ".").Run()
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
