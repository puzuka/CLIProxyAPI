// Package browser provides cross-platform functionality for opening URLs in the default web browser.
// It abstracts the underlying operating system commands and provides a simple interface.
package browser

import (
	"fmt"
	"os/exec"
	"runtime"
	"sync"

	log "github.com/sirupsen/logrus"
	"github.com/skratchdot/open-golang/open"
)

// incognito mode bookkeeping. The current implementation does not yet open a separate
// incognito window — these stubs exist so callers (e.g. Kiro OAuth flow) can express
// the preference without requiring a different OS command. If true, the caller may want
// to instruct the user to open the URL manually in a private window.
var (
	browserMutex       sync.Mutex
	incognitoMode      bool
	lastBrowserProcess *exec.Cmd
)

// SetIncognitoMode enables or disables incognito/private browsing preference for
// subsequent OAuth flows. The current implementation only records the flag — the
// actual browser invocation still uses the system default. This may be extended in
// the future to launch the browser with --incognito flags where supported.
func SetIncognitoMode(enabled bool) {
	browserMutex.Lock()
	defer browserMutex.Unlock()
	incognitoMode = enabled
}

// IsIncognitoMode returns whether incognito mode preference is set.
func IsIncognitoMode() bool {
	browserMutex.Lock()
	defer browserMutex.Unlock()
	return incognitoMode
}

// CloseBrowser attempts to terminate the last spawned browser process. When no process
// has been launched (or the browser was opened via the system handler that did not
// return a *exec.Cmd), this is a no-op and returns nil.
func CloseBrowser() error {
	browserMutex.Lock()
	defer browserMutex.Unlock()

	if lastBrowserProcess == nil || lastBrowserProcess.Process == nil {
		return nil
	}

	err := lastBrowserProcess.Process.Kill()
	lastBrowserProcess = nil
	return err
}

// OpenURL opens the specified URL in the default web browser.
// It first attempts to use a platform-agnostic library and falls back to
// platform-specific commands if that fails.
//
// Parameters:
//   - url: The URL to open.
//
// Returns:
//   - An error if the URL cannot be opened, otherwise nil.
func OpenURL(url string) error {
	fmt.Printf("Attempting to open URL in browser: %s\n", url)

	// Try using the open-golang library first
	err := open.Run(url)
	if err == nil {
		log.Debug("Successfully opened URL using open-golang library")
		return nil
	}

	log.Debugf("open-golang failed: %v, trying platform-specific commands", err)

	// Fallback to platform-specific commands
	return openURLPlatformSpecific(url)
}

// openURLPlatformSpecific is a helper function that opens a URL using OS-specific commands.
// This serves as a fallback mechanism for OpenURL.
//
// Parameters:
//   - url: The URL to open.
//
// Returns:
//   - An error if the URL cannot be opened, otherwise nil.
func openURLPlatformSpecific(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin": // macOS
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "linux":
		// Try common Linux browsers in order of preference
		browsers := []string{"xdg-open", "x-www-browser", "www-browser", "firefox", "chromium", "google-chrome"}
		for _, browser := range browsers {
			if _, err := exec.LookPath(browser); err == nil {
				cmd = exec.Command(browser, url)
				break
			}
		}
		if cmd == nil {
			return fmt.Errorf("no suitable browser found on Linux system")
		}
	default:
		return fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}

	log.Debugf("Running command: %s %v", cmd.Path, cmd.Args[1:])
	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to start browser command: %w", err)
	}

	log.Debug("Successfully opened URL using platform-specific command")
	return nil
}

// IsAvailable checks if the system has a command available to open a web browser.
// It verifies the presence of necessary commands for the current operating system.
//
// Returns:
//   - true if a browser can be opened, false otherwise.
func IsAvailable() bool {
	// First check if open-golang can work
	testErr := open.Run("about:blank")
	if testErr == nil {
		return true
	}

	// Check platform-specific commands
	switch runtime.GOOS {
	case "darwin":
		_, err := exec.LookPath("open")
		return err == nil
	case "windows":
		_, err := exec.LookPath("rundll32")
		return err == nil
	case "linux":
		browsers := []string{"xdg-open", "x-www-browser", "www-browser", "firefox", "chromium", "google-chrome"}
		for _, browser := range browsers {
			if _, err := exec.LookPath(browser); err == nil {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// GetPlatformInfo returns a map containing details about the current platform's
// browser opening capabilities, including the OS, architecture, and available commands.
//
// Returns:
//   - A map with platform-specific browser support information.
func GetPlatformInfo() map[string]interface{} {
	info := map[string]interface{}{
		"os":        runtime.GOOS,
		"arch":      runtime.GOARCH,
		"available": IsAvailable(),
	}

	switch runtime.GOOS {
	case "darwin":
		info["default_command"] = "open"
	case "windows":
		info["default_command"] = "rundll32"
	case "linux":
		browsers := []string{"xdg-open", "x-www-browser", "www-browser", "firefox", "chromium", "google-chrome"}
		var availableBrowsers []string
		for _, browser := range browsers {
			if _, err := exec.LookPath(browser); err == nil {
				availableBrowsers = append(availableBrowsers, browser)
			}
		}
		info["available_browsers"] = availableBrowsers
		if len(availableBrowsers) > 0 {
			info["default_command"] = availableBrowsers[0]
		}
	}

	return info
}
