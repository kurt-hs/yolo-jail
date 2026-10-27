// Package claudesecurity adapts only Claude Code's measured macOS Keychain operations.
package claudesecurity

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
	"golang.org/x/text/unicode/norm"
)

const (
	EnvVar       = "YOLO_CLAUDE_SECURITY_COMPAT"
	Verb         = "claude-security"
	AccountMode  = "account"
	ViewMode     = "view"
	SecurityPath = "/usr/bin/security"
)

// ViewService returns Claude's credential service for its exact secure-storage directory.
func ViewService(dir string) string {
	sum := sha256.Sum256([]byte(norm.NFC.String(dir)))
	return "Claude Code-credentials-" + hex.EncodeToString(sum[:4])
}

// ShimContent renders the PATH carrier for the hidden yolo dispatcher. Every inserted value is one
// single-quoted shell word; credentials and command input are never interpolated into this script.
func ShimContent(yolo, mode, service string) string {
	args := []string{yolo, "internal", Verb, mode}
	if service != "" {
		args = append(args, "--service", service)
	}
	return "#!/bin/sh\nexec " + shquote.Join(args) + ` -- "$@"` + "\n"
}

// Main is yolo internal's production entry point.
func Main(args []string) int {
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: yolo internal %s <account|view> [--service <name>] -- <security args...>\n", Verb)
		return 2
	}
	mode := args[0]
	if mode != AccountMode && mode != ViewMode {
		fmt.Fprintf(os.Stderr, "yolo internal %s: unknown mode\n", Verb)
		return 2
	}
	service := ""
	at := 1
	for at < len(args) && args[at] != "--" {
		if args[at] == "--service" && at+1 < len(args) {
			service = args[at+1]
			at += 2
			continue
		}
		fmt.Fprintf(os.Stderr, "yolo internal %s: invalid argument\n", Verb)
		return 2
	}
	if at == len(args) || at+1 == len(args) {
		fmt.Fprintf(os.Stderr, "yolo internal %s: missing -- or security arguments\n", Verb)
		return 2
	}
	return Run(mode, service, args[at+1:], os.Stdin, os.Stdout, os.Stderr, SecurityPath, runSecurity)
}

type commandRunner func(path string, args []string, stdin io.Reader, stdout, stderr io.Writer) int

// Run filters only the measured command/service pair, and forwards every other argument and
// interactive input byte to the real security command.
func Run(mode, service string, args []string, stdin io.Reader, stdout, stderr io.Writer, path string, runner commandRunner) int {
	if mode != AccountMode && mode != ViewMode {
		return 2
	}
	if i := commandIndex(args); i >= 0 {
		if code, blocked := refusal(mode, service, args[i:]); blocked {
			writeRefusal(stderr, code)
			return code
		}
	}
	if interactiveInvocation(args) {
		return runInteractive(mode, service, args, stdin, stdout, stderr, path, runner)
	}
	return runner(path, args, stdin, stdout, stderr)
}

func interactiveInvocation(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		recognized, help, interactive := globalOption(arg)
		if recognized {
			if help {
				return false
			}
			if interactive {
				return true
			}
			continue
		}
		return false
	}
	return false
}

func runInteractive(mode, service string, args []string, stdin io.Reader, stdout, stderr io.Writer, path string, runner commandRunner) int {
	reader := bufio.NewReader(stdin)
	var pw *io.PipeWriter
	var done chan int
	start := func() {
		pr, writer := io.Pipe()
		pw = writer
		done = make(chan int, 1)
		go func() {
			code := runner(path, args, pr, stdout, stderr)
			_ = pr.Close()
			done <- code
		}()
	}
	blockedCode := 0
	lastBlocked := false
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) != 0 {
			words, ok := parseWords(bytes.TrimSuffix(bytes.TrimSuffix(line, []byte("\n")), []byte("\r")))
			code, blocked := 0, false
			if ok {
				if i := commandIndex(words); i >= 0 {
					code, blocked = refusal(mode, service, words[i:])
				}
			}
			if blocked {
				blockedCode = code
				lastBlocked = true
				writeRefusal(stderr, code)
			} else {
				lastBlocked = false
				if pw == nil {
					start()
				}
				if _, werr := pw.Write(line); werr != nil {
					_ = pw.CloseWithError(werr)
					return <-done
				}
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				if pw == nil {
					if lastBlocked {
						return blockedCode
					}
					return 1
				}
				_ = pw.CloseWithError(err)
				childCode := <-done
				if lastBlocked {
					return blockedCode
				}
				return childCode
			}
			break
		}
	}
	if pw == nil {
		if lastBlocked {
			return blockedCode
		}
		start()
	}
	_ = pw.Close()
	childCode := <-done
	if lastBlocked {
		return blockedCode
	}
	return childCode
}

func refusal(mode, service string, command []string) (int, bool) {
	if len(command) == 0 {
		return 0, false
	}
	name := command[0]
	if mode == AccountMode {
		return 50, name == "add-generic-password"
	}
	if mode != ViewMode || service == "" {
		return 0, false
	}
	if name != "find-generic-password" && name != "delete-generic-password" && name != "add-generic-password" {
		return 0, false
	}
	if serviceOption(command) != service {
		return 0, false
	}
	if name == "find-generic-password" || name == "delete-generic-password" {
		return 44, true
	}
	return 50, true
}

// serviceOption follows option arity, so an -s token consumed as another option's value is not
// mistaken for the service; the last actual -s option is the effective one.
func serviceOption(command []string) string {
	if len(command) == 0 {
		return ""
	}
	service := ""
	for i := 1; i < len(command); i++ {
		arg := command[i]
		if arg == "--" {
			break
		}
		if arg == "-s" {
			if i+1 < len(command) {
				service = command[i+1]
				i++
			} else {
				service = ""
			}
			continue
		}
		if strings.HasPrefix(arg, "-s=") {
			service = strings.TrimPrefix(arg, "-s=")
			continue
		}
		if securityOptionTakesValue(command[0], arg) && i+1 < len(command) {
			i++
		}
	}
	return service
}

// securityOptionTakesValue is command-specific because some spellings take arguments for one
// subcommand but are switches for another (notably -w).
func securityOptionTakesValue(command, option string) bool {
	switch command {
	case "add-generic-password":
		switch option {
		case "-a", "-c", "-D", "-G", "-j", "-l", "-t", "-T", "-w", "-X":
			return true
		}
	case "find-generic-password":
		switch option {
		case "-a", "-c", "-C", "-D", "-G", "-j", "-l", "-t", "-T":
			return true
		}
	case "delete-generic-password":
		return option == "-a"
	}
	return false
}

// commandIndex accepts only the first command-position token after recognized security globals;
// an unknown option or command ends inspection rather than searching its operands.
func commandIndex(args []string) int {
	for i := 0; i < len(args); {
		arg := args[i]
		if arg == "--" {
			if i+1 < len(args) && securityCommand(args[i+1]) {
				return i + 1
			}
			return -1
		}
		recognized, help, _ := globalOption(arg)
		if recognized {
			if help {
				return -1
			}
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return -1
		}
		if securityCommand(arg) {
			return i
		}
		return -1
	}
	return -1
}

func securityCommand(arg string) bool {
	switch arg {
	case "add-generic-password", "find-generic-password", "delete-generic-password":
		return true
	default:
		return false
	}
}

func globalOption(arg string) (recognized, help, interactive bool) {
	if len(arg) < 2 || arg[0] != '-' || arg == "--" {
		return false, false, false
	}
	for _, option := range arg[1:] {
		switch option {
		case 'h':
			help = true
		case 'i':
			interactive = true
		case 'l', 'q', 'v':
		default:
			return false, false, false
		}
	}
	return true, help, interactive
}

// parseWords recognizes quoting and escaping, but performs no expansions or evaluation. Invalid or
// unterminated input is left for Apple's command to parse unchanged.
func parseWords(line []byte) ([]string, bool) {
	var words []string
	var word strings.Builder
	var quote byte
	started, escaped := false, false
	for _, b := range line {
		if escaped {
			word.WriteByte(b)
			escaped, started = false, true
			continue
		}
		if quote == '\'' {
			if b == '\'' {
				quote = 0
			} else {
				word.WriteByte(b)
			}
			started = true
			continue
		}
		if quote == '"' {
			if b == '"' {
				quote = 0
			} else if b == '\\' {
				escaped = true
			} else {
				word.WriteByte(b)
			}
			started = true
			continue
		}
		if b == '\\' {
			escaped, started = true, true
			continue
		}
		if b == '\'' || b == '"' {
			quote, started = b, true
			continue
		}
		if b == ' ' || b == '\t' || b == '\r' {
			if started {
				words = append(words, word.String())
				word.Reset()
				started = false
			}
			continue
		}
		word.WriteByte(b)
		started = true
	}
	if quote != 0 || escaped {
		return nil, false
	}
	if started {
		words = append(words, word.String())
	}
	return words, true
}

func writeRefusal(w io.Writer, code int) {
	if code == 44 {
		_, _ = io.WriteString(w, "security: SecKeychainSearchCopyNext: The specified item could not be found in the keychain.\n")
		return
	}
	_, _ = io.WriteString(w, "security: SecKeychainItemCreateFromContent: A default keychain could not be found.\n")
}

func runSecurity(path string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	cmd := exec.Command(path, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				return 128 + int(status.Signal())
			}
			return exit.ExitCode()
		}
		return 127
	}
	return 0
}
