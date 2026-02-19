package verify

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// RunCommands executes verification commands for the current repository.
func RunCommands(commands []string) error {
	if len(commands) == 0 {
		commands = inferCommands()
	}

	for _, cmdStr := range commands {
		parts := strings.Fields(cmdStr)
		if len(parts) == 0 {
			continue
		}
		cmd := exec.Command(parts[0], parts[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("command failed: %s", cmdStr)
		}
	}
	return nil
}

func inferCommands() []string {
	if _, err := os.Stat("go.mod"); err == nil {
		return []string{"go test ./..."}
	}
	if _, err := os.Stat("package.json"); err == nil {
		return []string{"npm test"}
	}
	return []string{}
}
