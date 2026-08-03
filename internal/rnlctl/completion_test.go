package rnlctl

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestCommandSpecIsValidAndContainsExpectedCommands(t *testing.T) {
	spec := rnlctlCommandSpec()
	if err := validateCommandSpec(spec); err != nil {
		t.Fatalf("validateCommandSpec() error = %v", err)
	}

	wantPaths := map[string]bool{
		"install": false, "upgrade": false, "overview": false, "config show": false,
		"config set": false, "secret set": false, "logs": false,
		"completion": false,
	}
	visitCompletionCommands(spec, nil, func(path []string, _ commandSpec) {
		joined := strings.Join(path, " ")
		if _, exists := wantPaths[joined]; exists {
			wantPaths[joined] = true
		}
	})
	for path, found := range wantPaths {
		if !found {
			t.Errorf("completion spec is missing %q", path)
		}
	}
}

func TestConfigurationCompletionCandidatesFollowKeyMetadata(t *testing.T) {
	want := map[string][]string{
		"config get": {
			"NODE_PORT", "NODE_BIND_ADDR", "LOW_MEMORY", "BODY_LIMIT_MB",
			"GOMEMLIMIT", "DISABLE_HASHED_SET_CHECK",
		},
		"config set": {
			"NODE_PORT=", "NODE_BIND_ADDR=", "LOW_MEMORY=", "BODY_LIMIT_MB=",
			"GOMEMLIMIT=", "DISABLE_HASHED_SET_CHECK=",
		},
		"config unset": {
			"NODE_BIND_ADDR", "LOW_MEMORY", "BODY_LIMIT_MB", "GOMEMLIMIT",
			"DISABLE_HASHED_SET_CHECK",
		},
	}

	found := make(map[string][]string, len(want))
	visitCompletionCommands(rnlctlCommandSpec(), nil, func(path []string, command commandSpec) {
		joined := strings.Join(path, " ")
		if _, expected := want[joined]; !expected {
			return
		}
		for _, candidate := range command.Arguments {
			found[joined] = append(found[joined], candidate.Value)
			if strings.Contains(candidate.Value, "SECRET") || strings.Contains(candidate.Value, "INTERNAL_") {
				t.Errorf("%s completion exposes private key %q", joined, candidate.Value)
			}
		}
	})

	for path, candidates := range want {
		if got := found[path]; !reflect.DeepEqual(got, candidates) {
			t.Errorf("%s candidates = %q, want %q", path, got, candidates)
		}
	}
}

func TestCompletionOptionShellPatternsKeepInlineWildcardUnquoted(t *testing.T) {
	options := []commandOptionSpec{{Long: "lines", Short: "n", Value: commandValueWord}}
	if got, want := strings.Join(completionOptionShellPatterns(options, false), "|"), "'--lines'|'-n'"; got != want {
		t.Fatalf("completionOptionShellPatterns(false) = %q, want %q", got, want)
	}
	if got, want := strings.Join(completionOptionShellPatterns(options, true), "|"), "'--lines='*|'-n='*"; got != want {
		t.Fatalf("completionOptionShellPatterns(true) = %q, want %q", got, want)
	}
}

func TestRunCompletionRendersSupportedShells(t *testing.T) {
	for _, test := range []struct {
		shell      string
		registered string
	}{
		{shell: "bash", registered: "complete -F _rnlctl_completion rnlctl"},
		{shell: "zsh", registered: "#compdef rnlctl"},
		{shell: "fish", registered: "complete -c rnlctl"},
	} {
		t.Run(test.shell, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			application := New(Options{Stdout: &stdout, Stderr: &stderr})
			if code := application.runCompletion([]string{test.shell}); code != 0 {
				t.Fatalf("runCompletion(%q) = %d, stderr = %q", test.shell, code, stderr.String())
			}
			output := stdout.String()
			for _, required := range []string{
				test.registered, "config", "overview", "NODE_PORT", "core-errors",
				"quiet", "no-color", "progress", "auto", "plain", "never", "dry-run", "since",
			} {
				if !strings.Contains(output, required) {
					t.Errorf("%s completion is missing %q", test.shell, required)
				}
			}
			for _, forbidden := range []string{"SECRET_KEY", "INTERNAL_REST_TOKEN", "secret.key"} {
				if strings.Contains(output, forbidden) {
					t.Errorf("%s completion exposes forbidden text %q", test.shell, forbidden)
				}
			}
		})
	}
}

func TestRunCompletionRejectsInvalidArguments(t *testing.T) {
	completionUsage := usageForCommand("completion")
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "missing", want: "requires exactly one shell"},
		{name: "extra", args: []string{"bash", "extra"}, want: "requires exactly one shell"},
		{name: "unknown", args: []string{"powershell"}, want: "unsupported completion shell"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			application := New(Options{Stdout: &stdout, Stderr: &stderr})
			if code := application.runCompletion(test.args); code != 2 {
				t.Fatalf("runCompletion(%q) = %d, want 2", test.args, code)
			}
			if !strings.Contains(stderr.String(), test.want) || !strings.Contains(stderr.String(), completionUsage) {
				t.Fatalf("stderr = %q, want %q and usage", stderr.String(), test.want)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
		})
	}
}

func TestRunCompletionHelp(t *testing.T) {
	completionUsage := usageForCommand("completion")
	var stdout, stderr bytes.Buffer
	application := New(Options{Stdout: &stdout, Stderr: &stderr})
	if code := application.runCompletion([]string{"--help"}); code != 0 {
		t.Fatalf("runCompletion(--help) = %d, stderr = %q", code, stderr.String())
	}
	if stdout.String() != completionUsage || stderr.Len() != 0 {
		t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
}

func TestCompletionRenderingIsDeterministic(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		first, err := renderCompletion(shell)
		if err != nil {
			t.Fatalf("renderCompletion(%s) error = %v", shell, err)
		}
		second, err := renderCompletion(shell)
		if err != nil {
			t.Fatalf("renderCompletion(%s) second error = %v", shell, err)
		}
		if first != second {
			t.Errorf("%s completion rendering is not deterministic", shell)
		}
	}
}

func TestGeneratedCompletionSyntax(t *testing.T) {
	for _, test := range []struct {
		shell string
		args  []string
	}{
		{shell: "bash", args: []string{"-n"}},
		{shell: "zsh", args: []string{"-n"}},
		{shell: "fish", args: []string{"-n"}},
	} {
		t.Run(test.shell, func(t *testing.T) {
			binary, err := exec.LookPath(test.shell)
			if err != nil {
				t.Skipf("%s is not installed", test.shell)
			}
			script, err := renderCompletion(test.shell)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "rnlctl."+test.shell)
			if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(binary, append(test.args, path)...)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("%s syntax check failed: %v\n%s", test.shell, err, output)
			}
		})
	}
}

func TestGeneratedBashCompletionBehavior(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	script, err := renderCompletion("bash")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "rnlctl.bash")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}

	complete := func(words []string, cursor int) []string {
		t.Helper()
		var assignment strings.Builder
		assignment.WriteString("COMP_WORDS=(")
		for _, word := range words {
			assignment.WriteByte(' ')
			assignment.WriteString(shellQuote(word))
		}
		assignment.WriteString(" )\n")
		program := "source \"$1\"\n" + assignment.String() + "COMP_CWORD=" + strconv.Itoa(cursor) + "\n_rnlctl_completion\nprintf '%s\\n' \"${COMPREPLY[@]}\"\n"
		command := exec.Command(bash, "-c", program, "completion-test", path)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("bash completion failed: %v\n%s", err, output)
		}
		return strings.Fields(string(output))
	}

	assertBashCandidates := func(words []string, cursor int, expected ...string) {
		t.Helper()
		lines := complete(words, cursor)
		for _, candidate := range expected {
			found := false
			for _, line := range lines {
				if line == candidate {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("completion for %q = %q, missing %q", words, lines, candidate)
			}
		}
	}
	assertBashMissing := func(words []string, cursor int, forbidden ...string) {
		t.Helper()
		lines := complete(words, cursor)
		for _, candidate := range forbidden {
			for _, line := range lines {
				if line == candidate {
					t.Errorf("completion for %q = %q, unexpectedly includes %q", words, lines, candidate)
				}
			}
		}
	}

	assertBashCandidates([]string{"rnlctl", "con"}, 1, "config")
	assertBashCandidates([]string{"rnlctl", "ov"}, 1, "overview")
	assertBashCandidates([]string{"rnlctl", "--quiet", "con"}, 2, "config")
	assertBashCandidates([]string{"rnlctl", "--progress", "a"}, 2, "auto")
	assertBashCandidates([]string{"rnlctl", "--progress=a"}, 1, "--progress=auto")
	assertBashCandidates([]string{"rnlctl", "--progress", "plain", "con"}, 3, "config")
	assertBashCandidates([]string{"rnlctl", "config", "--no-color", "se"}, 3, "set")
	assertBashCandidates([]string{"rnlctl", "config", "--progress", "plain", "se"}, 4, "set")
	assertBashCandidates([]string{"rnlctl", "config", "--progress=plain", "se"}, 3, "set")
	assertBashCandidates([]string{"rnlctl", "config", "set", "NODE_"}, 3, "NODE_PORT=", "NODE_BIND_ADDR=")
	assertBashCandidates([]string{"rnlctl", "logs", "--l"}, 2, "--lines")
	assertBashCandidates([]string{"rnlctl", "--quiet", "logs", "--s"}, 3, "--since")
	assertBashCandidates([]string{"rnlctl", "upgrade", "--d"}, 2, "--dry-run")
	assertBashMissing([]string{"rnlctl", "logs", "core", "--s"}, 3, "--since")
	assertBashCandidates([]string{"rnlctl", "logs", "--since", "15m", ""}, 4, "node")
	assertBashMissing([]string{"rnlctl", "logs", "--since", "15m", ""}, 4, "core", "core-errors")
	assertBashMissing([]string{"rnlctl", "logs", "--since=15m", ""}, 3, "core", "core-errors")
	assertBashCandidates([]string{"rnlctl", "config", "set", "NODE_PORT=12345", "NODE_"}, 4, "NODE_BIND_ADDR=")
	assertBashMissing([]string{"rnlctl", "config", "set", "NODE_PORT=12345", "NODE_"}, 4, "NODE_PORT=")

	directoryRoot := t.TempDir()
	bundleDirectory := filepath.Join(directoryRoot, "bundle")
	if err := os.Mkdir(bundleDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Join(directoryRoot, "bu")
	assertBashCandidates([]string{"rnlctl", "install", "--bundle-root=" + prefix}, 2, "--bundle-root="+bundleDirectory)
	// Bash may split '=' according to COMP_WORDBREAKS. Exercise that form as
	// well so the generated script keeps working with the default word breaks.
	assertBashCandidates([]string{"rnlctl", "install", "--bundle-root", "=", prefix}, 4, bundleDirectory)
}

func TestGeneratedZshCompletionBehavior(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is not installed")
	}
	script, err := renderCompletion("zsh")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "_rnlctl")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}

	complete := func(words string, cursor string) string {
		t.Helper()
		program := `
compdef() { :; }
source "$1"
_describe() {
  local name=${@[4]}
  eval "print -l -- \${${name}[@]}"
}
compadd() { print -l -- "$@"; }
words=(` + words + `)
CURRENT=` + cursor + `
_rnlctl
`
		command := exec.Command(zsh, "-fc", program, "completion-test", path)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("zsh completion failed: %v\n%s", err, output)
		}
		return string(output)
	}

	assertZshCandidates := func(words string, cursor string, expected ...string) {
		t.Helper()
		output := complete(words, cursor)
		for _, candidate := range expected {
			if !strings.Contains(output, candidate) {
				t.Errorf("completion for %q = %q, missing %q", words, output, candidate)
			}
		}
	}
	assertZshMissing := func(words string, cursor string, forbidden ...string) {
		t.Helper()
		output := complete(words, cursor)
		for _, candidate := range forbidden {
			if strings.Contains(output, candidate) {
				t.Errorf("completion for %q = %q, unexpectedly includes %q", words, output, candidate)
			}
		}
	}

	assertZshCandidates("rnlctl --quiet config --no-color se", "5", "set:Set and validate")
	assertZshCandidates("rnlctl ov", "2", "overview:Show a concise operator summary")
	assertZshCandidates("rnlctl --progress a", "3", "auto:Use terminal progress")
	assertZshCandidates("rnlctl '--progress=a'", "2", "--progress=auto:Use terminal progress")
	assertZshCandidates("rnlctl --progress plain config --no-color se", "6", "set:Set and validate")
	assertZshCandidates("rnlctl config --progress plain se", "5", "set:Set and validate")
	assertZshCandidates("rnlctl config '--progress=plain' se", "4", "set:Set and validate")
	assertZshCandidates("rnlctl config set NODE_", "4", "NODE_PORT=", "NODE_BIND_ADDR=")
	assertZshCandidates("rnlctl --quiet logs --s", "4", "--since:Show entries")
	assertZshMissing("rnlctl logs core --s", "4", "--since:Show entries")
	assertZshCandidates("rnlctl logs --since 15m ''", "5", "node:remnanode-lite")
	assertZshMissing("rnlctl logs --since 15m ''", "5", "core:rw-core", "core-errors:rw-core")
	assertZshCandidates("rnlctl config set NODE_PORT=12345 NODE_", "5", "NODE_BIND_ADDR=")
	assertZshMissing("rnlctl config set NODE_PORT=12345 NODE_", "5", "NODE_PORT=")
}

func TestGeneratedFishCompletionBehavior(t *testing.T) {
	fish, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("fish is not installed")
	}
	script, err := renderCompletion("fish")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "rnlctl.fish")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}

	complete := func(line string) string {
		t.Helper()
		command := exec.Command(fish, "-c", "source \"$argv[1]\"; complete -C \"$argv[2]\"", path, line)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("fish completion failed: %v\n%s", err, output)
		}
		return string(output)
	}
	assertFishCandidates := func(line string, expected ...string) {
		t.Helper()
		output := complete(line)
		for _, candidate := range expected {
			if !strings.Contains(output, candidate) {
				t.Errorf("completion for %q = %q, missing %q", line, output, candidate)
			}
		}
	}
	assertFishMissing := func(line string, forbidden ...string) {
		t.Helper()
		output := complete(line)
		for _, candidate := range forbidden {
			if strings.Contains(output, candidate) {
				t.Errorf("completion for %q = %q, unexpectedly includes %q", line, output, candidate)
			}
		}
	}

	assertFishCandidates("rnlctl config ", "set\tSet and validate")
	assertFishCandidates("rnlctl ov", "overview\tShow a concise operator summary")
	assertFishCandidates("rnlctl --progress a", "auto")
	assertFishCandidates("rnlctl --progress=au", "auto")
	assertFishCandidates("rnlctl --progress plain config ", "set\tSet and validate")
	assertFishCandidates("rnlctl config --progress plain ", "set\tSet and validate")
	assertFishCandidates("rnlctl config --progress=plain ", "set\tSet and validate")
	assertFishCandidates("rnlctl config set NODE_PORT=12345 ", "NODE_BIND_ADDR=")
	assertFishMissing("rnlctl config set NODE_PORT=12345 ", "NODE_PORT=")
	assertFishMissing("rnlctl logs core --", "--since")
	assertFishCandidates("rnlctl logs --since 15m ", "node\tremnanode-lite")
	assertFishMissing("rnlctl logs --since 15m ", "core\trw-core", "core-errors\trw-core")
	assertFishMissing("rnlctl install --bundle-root config ", "show\t", "set\t")

	directoryRoot := t.TempDir()
	bundleDirectory := filepath.Join(directoryRoot, "bundle")
	if err := os.Mkdir(bundleDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	assertFishCandidates("rnlctl install --bundle-root="+filepath.Join(directoryRoot, "bu"), "--bundle-root="+bundleDirectory)
}
