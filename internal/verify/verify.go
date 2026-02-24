package verify

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

// RunCommands executes verification commands for the current repository.
func RunCommands(commands []string) error {
	if len(commands) == 0 {
		commands = inferCommands()
	}
	slog.Info("verification commands prepared", "count", len(commands))

	for i, cmdStr := range commands {
		parts := strings.Fields(cmdStr)
		if len(parts) == 0 {
			continue
		}
		startedAt := time.Now()
		slog.Info("verification command started", "index", i+1, "total", len(commands), "command", cmdStr)
		cmd := exec.Command(parts[0], parts[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			slog.Error("verification command failed", "index", i+1, "total", len(commands), "command", cmdStr, "duration_ms", time.Since(startedAt).Milliseconds(), "error", err)
			return fmt.Errorf("command failed: %s: %w", cmdStr, err)
		}
		slog.Info("verification command succeeded", "index", i+1, "total", len(commands), "command", cmdStr, "duration_ms", time.Since(startedAt).Milliseconds())
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
