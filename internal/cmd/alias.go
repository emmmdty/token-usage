package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/emmmdty/token-usage/internal/i18n"
	"github.com/spf13/cobra"
)

// goos is a variable so tests can simulate platform-specific behavior.
var goos = runtime.GOOS

// rcFileFor maps a $SHELL value to the rc file the alias lives in.
func rcFileFor(shell, home string) string {
	if strings.Contains(shell, "zsh") {
		return filepath.Join(home, ".zshrc")
	}
	return filepath.Join(home, ".bashrc")
}

// checkAliasPlatform rejects the alias commands where they cannot do the
// right thing (no bash/zsh rc-file convention).
func checkAliasPlatform() error {
	if goos == "windows" {
		return fmt.Errorf("%s", i18n.T("error.alias.windows"))
	}
	return nil
}

var aliasCmd = &cobra.Command{
	Use:   "alias",
	Short: "Manage shell aliases",
}

var aliasInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install shell alias",
	Long: `Install the 'tu' shell alias for token-usage.

Detects your shell from $SHELL: zsh writes to ~/.zshrc, anything else to
~/.bashrc. Re-running replaces an existing 'tu' alias after confirmation.
Restart your terminal or re-source the rc file afterwards.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := checkAliasPlatform(); err != nil {
			return err
		}
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		rcFile := rcFileFor(os.Getenv("SHELL"), homeDir)

		if aliasExists(rcFile, "tu") {
			fmt.Printf("%s", i18n.T("output.alias.already_exists", rcFile)+"\n")
			fmt.Print(i18n.T("output.alias.overwrite") + " (y/N): ")

			reader := bufio.NewReader(os.Stdin)
			response, _ := reader.ReadString('\n')
			response = strings.TrimSpace(response)

			if response != "y" && response != "Y" {
				fmt.Println(i18n.T("output.alias.cancelled"))
				return nil
			}
		}

		alias := "\n# token-usage alias\nalias tu='token-usage'\n"

		f, err := os.OpenFile(rcFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()

		if _, err := f.WriteString(alias); err != nil {
			return err
		}

		fmt.Printf("%s", i18n.T("output.alias.added", rcFile)+"\n")
		fmt.Println(i18n.T("output.alias.restart", rcFile))

		return nil
	},
}

var aliasUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall shell alias",
	Long: `Remove the 'tu' shell alias (and its marker comment) from your rc file.

Detects your shell from $SHELL: zsh writes to ~/.zshrc, anything else to
~/.bashrc. Does nothing when the alias is not present.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := checkAliasPlatform(); err != nil {
			return err
		}
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		rcFile := rcFileFor(os.Getenv("SHELL"), homeDir)

		if !aliasExists(rcFile, "tu") {
			fmt.Printf("%s", i18n.T("output.alias.not_found", rcFile)+"\n")
			return nil
		}

		if err := removeAlias(rcFile, "tu"); err != nil {
			return err
		}

		fmt.Printf("%s", i18n.T("output.alias.removed", rcFile)+"\n")
		fmt.Println(i18n.T("output.alias.restart", rcFile))
		return nil
	},
}

func aliasExists(rcFile, alias string) bool {
	file, err := os.Open(rcFile)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "alias "+alias+"=") {
			return true
		}
	}

	return false
}

func removeAlias(rcFile, alias string) error {
	info, err := os.Stat(rcFile)
	if err != nil {
		return err
	}
	perm := info.Mode().Perm()

	data, err := os.ReadFile(rcFile)
	if err != nil {
		return err
	}

	// Back up the original content before touching a user file.
	if err := os.WriteFile(rcFile+".tu-bak", data, perm); err != nil {
		return fmt.Errorf("failed to write backup %s.tu-bak: %w", rcFile, err)
	}

	// Preserve the original line ending style.
	newline := "\n"
	if strings.Contains(string(data), "\r\n") {
		newline = "\r\n"
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	aliasLine := "alias " + alias + "="
	var result []string
	for i, line := range lines {
		// Only remove lines this tool installed: the alias must point at
		// token-usage, so a user's own "alias tu=..." is never touched.
		isToolAlias := strings.Contains(line, aliasLine) && strings.Contains(line, "token-usage")
		if isToolAlias {
			if i > 0 && len(result) > 0 && strings.TrimSpace(result[len(result)-1]) == "# token-usage alias" {
				result = result[:len(result)-1]
			}
			continue
		}
		result = append(result, line)
	}

	content := strings.Join(result, newline)
	if content != "" && !strings.HasSuffix(content, newline) {
		content += newline
	}

	return os.WriteFile(rcFile, []byte(content), perm)
}

func init() {
	aliasCmd.AddCommand(aliasInstallCmd)
	aliasCmd.AddCommand(aliasUninstallCmd)
	rootCmd.AddCommand(aliasCmd)
}
