package commands

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"ktd/internal/store"
)

// Sync runs `ktd sync [--remote <url>]`: optionally set the data dir's git
// remote, then git add -A && commit (skipping if nothing changed) && push.
func Sync(s *store.Store, remote string) error {
	if remote != "" {
		if err := setRemote(s, remote); err != nil {
			return err
		}
	}

	addCmd := exec.Command("git", "add", "-A")
	addCmd.Dir = s.Dir
	if out, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %w: %s", err, out)
	}

	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = s.Dir
	statusOut, err := statusCmd.Output()
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}

	if strings.TrimSpace(string(statusOut)) == "" {
		fmt.Println("Nothing to sync — working tree clean.")
	} else {
		msg := "sync " + time.Now().Format("2006-01-02")
		commitCmd := exec.Command("git", "commit", "-m", msg)
		commitCmd.Dir = s.Dir
		if out, err := commitCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git commit: %w: %s", err, out)
		}
		fmt.Println("Committed: " + msg)
	}

	remoteCmd := exec.Command("git", "remote")
	remoteCmd.Dir = s.Dir
	remotesOut, _ := remoteCmd.Output()
	if strings.TrimSpace(string(remotesOut)) == "" {
		fmt.Println("No remote configured — skipping push. Use `ktd sync --remote <url>` to set one.")
		return nil
	}

	pushCmd := exec.Command("git", "push", "-u", "origin", "HEAD")
	pushCmd.Dir = s.Dir
	if out, err := pushCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git push: %w: %s", err, out)
	}
	fmt.Println("Pushed.")
	return nil
}

func setRemote(s *store.Store, remote string) error {
	getCmd := exec.Command("git", "remote", "get-url", "origin")
	getCmd.Dir = s.Dir
	if err := getCmd.Run(); err != nil {
		addCmd := exec.Command("git", "remote", "add", "origin", remote)
		addCmd.Dir = s.Dir
		if out, err := addCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git remote add: %w: %s", err, out)
		}
		return nil
	}
	setCmd := exec.Command("git", "remote", "set-url", "origin", remote)
	setCmd.Dir = s.Dir
	if out, err := setCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git remote set-url: %w: %s", err, out)
	}
	return nil
}
