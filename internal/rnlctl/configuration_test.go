package rnlctl

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEngineReadConfigurationProjectsOnlyEditableValues(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	raw := mustReadTestFile(t, harness.paths.EnvironmentFile)
	raw = append(raw, []byte("NODE_BIND_ADDR=127.0.0.1\nINTERNAL_REST_TOKEN=do-not-display\nUNKNOWN_PRIVATE_VALUE=also-hidden\n")...)
	if err := os.WriteFile(harness.paths.EnvironmentFile, raw, 0o640); err != nil {
		t.Fatal(err)
	}

	configuration, err := harness.engine.ReadConfiguration(context.Background())
	if err != nil {
		t.Fatalf("ReadConfiguration() error = %v", err)
	}
	if configuration.SchemaVersion != configurationSchemaVersion || configuration.Path != harness.paths.EnvironmentFile {
		t.Fatalf("ReadConfiguration() metadata = %#v", configuration)
	}
	if len(configuration.Values) != len(editableConfigurationKeys) || configuration.Values["NODE_BIND_ADDR"] != "127.0.0.1" {
		t.Fatalf("ReadConfiguration() values = %#v", configuration.Values)
	}
	for _, forbidden := range []string{"SECRET_KEY", "SECRET_KEY_FILE", "INTERNAL_REST_TOKEN", "UNKNOWN_PRIVATE_VALUE", "XRAY_BIN"} {
		if _, exists := configuration.Values[forbidden]; exists {
			t.Fatalf("ReadConfiguration() exposed %s", forbidden)
		}
	}
}

func TestEngineUpdateConfigurationPreservesUnmanagedContent(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	raw := mustReadTestFile(t, harness.paths.EnvironmentFile)
	raw = append(raw, []byte("\n# administrator note\nUNKNOWN_SETTING=keep-me\nBODY_LIMIT_MB=16\n")...)
	if err := os.WriteFile(harness.paths.EnvironmentFile, raw, 0o640); err != nil {
		t.Fatal(err)
	}
	harness.host.calls = nil

	result, err := harness.engine.UpdateConfiguration(context.Background(), ConfigurationUpdateRequest{
		Set: map[string]string{
			"NODE_PORT":      "12345",
			"NODE_BIND_ADDR": "127.0.0.1",
			"GOMEMLIMIT":     "192MiB",
		},
		Unset: []string{"BODY_LIMIT_MB"},
	})
	if err != nil {
		t.Fatalf("UpdateConfiguration() error = %v", err)
	}
	if !result.Changed || result.Operation != "config-set" {
		t.Fatalf("UpdateConfiguration() = %#v", result)
	}
	updated := string(mustReadTestFile(t, harness.paths.EnvironmentFile))
	for _, want := range []string{
		"NODE_PORT=12345", "NODE_BIND_ADDR=127.0.0.1", "GOMEMLIMIT=192MiB",
		"# administrator note", "UNKNOWN_SETTING=keep-me", "XRAY_BIN=" + filepath.Join(harness.paths.CurrentLink, "lib", "rw-core"),
	} {
		if !strings.Contains(updated, want) {
			t.Fatalf("updated node.env does not contain %q:\n%s", want, updated)
		}
	}
	if strings.Contains(updated, "BODY_LIMIT_MB=") {
		t.Fatalf("updated node.env retained BODY_LIMIT_MB:\n%s", updated)
	}
	if countCall(harness.host.calls, "apply-ownership") != 1 || containsCall(harness.host.calls, "restart") {
		t.Fatalf("host calls = %q", harness.host.calls)
	}
	if err := harness.engine.CheckConfiguration(context.Background()); err != nil {
		t.Fatalf("CheckConfiguration() error = %v", err)
	}
	assertMode(t, harness.paths.EnvironmentFile, 0o640)
}

func TestEngineUpdateConfigurationRejectsUnsafeOrInvalidCandidates(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	original := mustReadTestFile(t, harness.paths.EnvironmentFile)

	tests := []struct {
		name    string
		request ConfigurationUpdateRequest
		want    string
	}{
		{name: "managed path", request: ConfigurationUpdateRequest{Set: map[string]string{"XRAY_BIN": "/tmp/core"}}, want: "not administrator-editable"},
		{name: "secret", request: ConfigurationUpdateRequest{Set: map[string]string{"SECRET_KEY": "value"}}, want: "not administrator-editable"},
		{name: "newline", request: ConfigurationUpdateRequest{Set: map[string]string{"NODE_BIND_ADDR": "127.0.0.1\nLOW_MEMORY=0"}}, want: "unsupported whitespace"},
		{name: "interior whitespace", request: ConfigurationUpdateRequest{Set: map[string]string{"NODE_BIND_ADDR": "127.0.0.1 localhost"}}, want: "unsupported whitespace"},
		{name: "unicode whitespace", request: ConfigurationUpdateRequest{Set: map[string]string{"NODE_BIND_ADDR": "127.0.0.1\u00a0"}}, want: "unsupported whitespace"},
		{name: "escape control", request: ConfigurationUpdateRequest{Set: map[string]string{"NODE_BIND_ADDR": "127.0.0.1\x1b]0;title\x07"}}, want: "unsupported whitespace"},
		{name: "bell control", request: ConfigurationUpdateRequest{Set: map[string]string{"NODE_BIND_ADDR": "127.0.0.1\x07"}}, want: "unsupported whitespace"},
		{name: "invalid UTF-8", request: ConfigurationUpdateRequest{Set: map[string]string{"NODE_BIND_ADDR": string([]byte{'1', '2', '7', '.', '0', '.', '0', '.', '1', 0xff})}}, want: "unsupported whitespace"},
		{name: "literal backslash n", request: ConfigurationUpdateRequest{Set: map[string]string{"NODE_BIND_ADDR": `127.0.0.1\nLOW_MEMORY=0`}}, want: "unsupported whitespace"},
		{name: "SNI boolean", request: ConfigurationUpdateRequest{Set: map[string]string{"SNI_VERIFICATION": "enabled"}}, want: "SNI_VERIFICATION must be a boolean"},
		{name: "nftables logging boolean", request: ConfigurationUpdateRequest{Set: map[string]string{"NFTABLES_LOGGING": "enabled"}}, want: "NFTABLES_LOGGING must be a boolean"},
		{name: "nftables reply boolean", request: ConfigurationUpdateRequest{Set: map[string]string{"NFTABLES_ACCEPT_REPLY_TRAFFIC": "enabled"}}, want: "NFTABLES_ACCEPT_REPLY_TRAFFIC must be a boolean"},
		{name: "low memory body limit", request: ConfigurationUpdateRequest{Set: map[string]string{"LOW_MEMORY": "1", "BODY_LIMIT_MB": "64"}}, want: "must not exceed 16"},
		{name: "required port", request: ConfigurationUpdateRequest{Unset: []string{"NODE_PORT"}}, want: "NODE_PORT must be between 1 and 65535"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := harness.engine.UpdateConfiguration(context.Background(), test.request)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("UpdateConfiguration() error = %v, want %q", err, test.want)
			}
			if got := mustReadTestFile(t, harness.paths.EnvironmentFile); !reflect.DeepEqual(got, original) {
				t.Fatalf("node.env changed after rejection")
			}
		})
	}
}

func TestEngineUpdateConfigurationAcceptsEmptyOptionalValue(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)

	if _, err := harness.engine.UpdateConfiguration(context.Background(), ConfigurationUpdateRequest{
		Set: map[string]string{"NODE_BIND_ADDR": "127.0.0.1"},
	}); err != nil {
		t.Fatalf("seed UpdateConfiguration() error = %v", err)
	}
	if _, err := harness.engine.UpdateConfiguration(context.Background(), ConfigurationUpdateRequest{
		Set: map[string]string{"NODE_BIND_ADDR": ""},
	}); err != nil {
		t.Fatalf("UpdateConfiguration(empty optional value) error = %v", err)
	}
	configuration, err := harness.engine.ReadConfiguration(context.Background())
	if err != nil {
		t.Fatalf("ReadConfiguration() error = %v", err)
	}
	if got := configuration.Values["NODE_BIND_ADDR"]; got != "" {
		t.Fatalf("NODE_BIND_ADDR = %q, want empty", got)
	}
}

func TestEngineUpdateConfigurationAppliesAndRollsBackFailedHealth(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	original := mustReadTestFile(t, harness.paths.EnvironmentFile)
	harness.host.calls = nil
	harness.host.fail("wait-healthy", errors.New("new configuration unhealthy"), nil)

	_, err := harness.engine.UpdateConfiguration(context.Background(), ConfigurationUpdateRequest{
		Set:   map[string]string{"NODE_PORT": "12345"},
		Apply: true,
	})
	if err == nil || !strings.Contains(err.Error(), "new configuration unhealthy") {
		t.Fatalf("UpdateConfiguration() error = %v", err)
	}
	if got := mustReadTestFile(t, harness.paths.EnvironmentFile); !reflect.DeepEqual(got, original) {
		t.Fatalf("node.env after rollback differs from original")
	}
	if countCall(harness.host.calls, "restart") != 2 || countCall(harness.host.calls, "wait-healthy:remnanode-lite") != 2 || countCall(harness.host.calls, "apply-ownership") != 2 {
		t.Fatalf("host calls = %q", harness.host.calls)
	}

	harness.host.calls = nil
	result, err := harness.engine.UpdateConfiguration(context.Background(), ConfigurationUpdateRequest{
		Set:   map[string]string{"NODE_PORT": "12345"},
		Apply: true,
	})
	if err != nil || !result.Changed {
		t.Fatalf("UpdateConfiguration(success) = %#v, %v", result, err)
	}
	if countCall(harness.host.calls, "restart") != 1 || countCall(harness.host.calls, "wait-healthy:remnanode-lite") != 1 {
		t.Fatalf("host calls = %q", harness.host.calls)
	}
}

func TestEngineConfigurationApplyRequiresActiveInstallationBeforeWriting(t *testing.T) {
	for _, prepared := range []bool{false, true} {
		t.Run(map[bool]string{false: "stopped", true: "prepared"}[prepared], func(t *testing.T) {
			harness := newLifecycleHarness(t, "2.8.0-rnl.1")
			harness.install(t, prepared)
			if !prepared {
				if _, err := harness.engine.Stop(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			original := mustReadTestFile(t, harness.paths.EnvironmentFile)
			_, err := harness.engine.UpdateConfiguration(context.Background(), ConfigurationUpdateRequest{
				Set: map[string]string{"NODE_PORT": "12345"}, Apply: true,
			})
			if err == nil {
				t.Fatal("UpdateConfiguration(--apply) unexpectedly succeeded")
			}
			if got := mustReadTestFile(t, harness.paths.EnvironmentFile); !reflect.DeepEqual(got, original) {
				t.Fatal("node.env changed before --apply rejection")
			}

			originalSecret, readErr := os.ReadFile(harness.paths.SecretFile)
			secretExisted := readErr == nil
			if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
				t.Fatal(readErr)
			}
			newSecret := writeTestSecretValue(t, filepath.Join(t.TempDir(), "new-secret.key"), "inactive")
			if _, err := harness.engine.SetSecret(context.Background(), SecretUpdateRequest{File: newSecret, Apply: true}); err == nil {
				t.Fatal("SetSecret(--apply) unexpectedly succeeded")
			}
			installedSecret, readErr := os.ReadFile(harness.paths.SecretFile)
			if secretExisted {
				if readErr != nil || !reflect.DeepEqual(installedSecret, originalSecret) {
					t.Fatal("Secret changed before --apply rejection")
				}
			} else if !errors.Is(readErr, os.ErrNotExist) {
				t.Fatalf("prepared Secret was created before --apply rejection: %v", readErr)
			}

			if _, err := harness.engine.ApplyConfiguration(context.Background()); err == nil {
				t.Fatal("ApplyConfiguration() unexpectedly succeeded")
			}
			if containsCall(harness.host.calls, "restart") {
				t.Fatalf("inactive configuration operation restarted service: %q", harness.host.calls)
			}
		})
	}
}

func TestEngineApplyConfigurationValidatesThenRestarts(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	if _, err := harness.engine.UpdateConfiguration(context.Background(), ConfigurationUpdateRequest{
		Set: map[string]string{"NODE_PORT": "12345"},
	}); err != nil {
		t.Fatal(err)
	}
	harness.host.calls = nil

	result, err := harness.engine.ApplyConfiguration(context.Background())
	if err != nil || result.Operation != "config-apply" || !result.Changed {
		t.Fatalf("ApplyConfiguration() = %#v, %v", result, err)
	}
	if countCall(harness.host.calls, "restart") != 1 || countCall(harness.host.calls, "wait-healthy:remnanode-lite") != 1 {
		t.Fatalf("host calls = %q", harness.host.calls)
	}

	raw := mustReadTestFile(t, harness.paths.EnvironmentFile)
	raw = bytes.Replace(raw, []byte("NODE_PORT=12345"), []byte("NODE_PORT=70000"), 1)
	if err := os.WriteFile(harness.paths.EnvironmentFile, raw, 0o640); err != nil {
		t.Fatal(err)
	}
	harness.host.calls = nil
	if _, err := harness.engine.ApplyConfiguration(context.Background()); err == nil || !strings.Contains(err.Error(), "NODE_PORT must be between") {
		t.Fatalf("ApplyConfiguration(invalid) error = %v", err)
	}
	if containsCall(harness.host.calls, "restart") {
		t.Fatalf("invalid configuration restarted service: %q", harness.host.calls)
	}
}

func TestEngineCheckAndApplyConfigurationRejectUnsafePermissions(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	if err := os.Chmod(harness.paths.EnvironmentFile, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := harness.engine.CheckConfiguration(context.Background()); err == nil || !strings.Contains(err.Error(), "regular 0640 file") {
		t.Fatalf("CheckConfiguration() error = %v", err)
	}
	harness.host.calls = nil
	if _, err := harness.engine.ApplyConfiguration(context.Background()); err == nil || !strings.Contains(err.Error(), "regular 0640 file") {
		t.Fatalf("ApplyConfiguration() error = %v", err)
	}
	if containsCall(harness.host.calls, "restart") {
		t.Fatalf("unsafe configuration restarted service: %q", harness.host.calls)
	}

	for _, test := range []struct {
		name string
		run  func() error
	}{
		{
			name: "set same value",
			run: func() error {
				_, err := harness.engine.UpdateConfiguration(context.Background(), ConfigurationUpdateRequest{
					Set: map[string]string{"NODE_PORT": "2222"}, Apply: true,
				})
				return err
			},
		},
		{
			name: "set same Secret",
			run: func() error {
				_, err := harness.engine.SetSecret(context.Background(), SecretUpdateRequest{
					File: harness.paths.SecretFile, Apply: true,
				})
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			harness.host.calls = nil
			if err := test.run(); err == nil || !strings.Contains(err.Error(), "regular 0640 file") {
				t.Fatalf("operation error = %v", err)
			}
			if containsCall(harness.host.calls, "restart") {
				t.Fatalf("unsafe configuration restarted service: %q", harness.host.calls)
			}
		})
	}
}

func TestEngineUpdateConfigurationRestoresFileAfterOwnershipFailure(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	original := mustReadTestFile(t, harness.paths.EnvironmentFile)
	harness.host.calls = nil
	harness.host.fail("apply-ownership", errors.New("ownership failed"), nil)

	_, err := harness.engine.UpdateConfiguration(context.Background(), ConfigurationUpdateRequest{
		Set: map[string]string{"NODE_PORT": "12345"},
	})
	if err == nil || !strings.Contains(err.Error(), "ownership failed") {
		t.Fatalf("UpdateConfiguration() error = %v", err)
	}
	if got := mustReadTestFile(t, harness.paths.EnvironmentFile); !reflect.DeepEqual(got, original) {
		t.Fatal("node.env was not restored after ownership failure")
	}
	if countCall(harness.host.calls, "apply-ownership") != 2 {
		t.Fatalf("host calls = %q", harness.host.calls)
	}
}

func TestEngineSetSecretRestoresFileAfterOwnershipFailure(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	original := mustReadTestFile(t, harness.paths.SecretFile)
	newSecret := writeTestSecretValue(t, filepath.Join(t.TempDir(), "new-secret.key"), "ownership")
	harness.host.calls = nil
	harness.host.fail("apply-ownership", errors.New("ownership failed"), nil)

	_, err := harness.engine.SetSecret(context.Background(), SecretUpdateRequest{File: newSecret})
	if err == nil || !strings.Contains(err.Error(), "ownership failed") {
		t.Fatalf("SetSecret() error = %v", err)
	}
	if got := mustReadTestFile(t, harness.paths.SecretFile); !reflect.DeepEqual(got, original) {
		t.Fatal("Secret was not restored after ownership failure")
	}
	if countCall(harness.host.calls, "apply-ownership") != 2 {
		t.Fatalf("host calls = %q", harness.host.calls)
	}
}

func TestEngineSetSecretSupportsPreparedInstallAndRollsBackFailedApply(t *testing.T) {
	t.Run("prepared without apply", func(t *testing.T) {
		harness := newLifecycleHarness(t, "2.8.0-rnl.1")
		harness.install(t, true)
		newSecret := writeTestSecretValue(t, filepath.Join(t.TempDir(), "new-secret.key"), "prepared")
		result, err := harness.engine.SetSecret(context.Background(), SecretUpdateRequest{File: newSecret})
		if err != nil || !result.Changed {
			t.Fatalf("SetSecret() = %#v, %v", result, err)
		}
		if _, err := readExistingSecret(harness.paths.SecretFile); err != nil {
			t.Fatalf("installed Secret error = %v", err)
		}
		if err := harness.engine.CheckConfiguration(context.Background()); err != nil {
			t.Fatalf("CheckConfiguration() error = %v", err)
		}
	})

	t.Run("active rollback", func(t *testing.T) {
		harness := newLifecycleHarness(t, "2.8.0-rnl.1")
		harness.install(t, false)
		original := mustReadTestFile(t, harness.paths.SecretFile)
		newSecret := writeTestSecretValue(t, filepath.Join(t.TempDir(), "new-secret.key"), "active")
		harness.host.calls = nil
		harness.host.fail("wait-healthy", errors.New("rotated Secret rejected"), nil)
		_, err := harness.engine.SetSecret(context.Background(), SecretUpdateRequest{File: newSecret, Apply: true})
		if err == nil || !strings.Contains(err.Error(), "rotated Secret rejected") {
			t.Fatalf("SetSecret() error = %v", err)
		}
		if got := mustReadTestFile(t, harness.paths.SecretFile); !reflect.DeepEqual(got, original) {
			t.Fatal("Secret was not restored")
		}
		if countCall(harness.host.calls, "restart") != 2 || countCall(harness.host.calls, "apply-ownership") != 2 {
			t.Fatalf("host calls = %q", harness.host.calls)
		}
	})

	t.Run("same Secret still validates before apply", func(t *testing.T) {
		harness := newLifecycleHarness(t, "2.8.0-rnl.1")
		harness.install(t, false)
		raw := mustReadTestFile(t, harness.paths.EnvironmentFile)
		raw = bytes.Replace(raw, []byte("NODE_PORT=2222"), []byte("NODE_PORT=70000"), 1)
		if err := os.WriteFile(harness.paths.EnvironmentFile, raw, 0o640); err != nil {
			t.Fatal(err)
		}
		harness.host.calls = nil

		_, err := harness.engine.SetSecret(context.Background(), SecretUpdateRequest{
			File: harness.paths.SecretFile, Apply: true,
		})
		if err == nil || !strings.Contains(err.Error(), "NODE_PORT must be between") {
			t.Fatalf("SetSecret(same, --apply) error = %v", err)
		}
		if containsCall(harness.host.calls, "restart") {
			t.Fatalf("invalid configuration restarted service: %q", harness.host.calls)
		}
	})
}

func TestEngineConfigurationApplyCancellationUsesDetachedRecovery(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T, lifecycleHarness, context.Context) error
	}{
		{
			name: "config apply",
			run: func(_ *testing.T, harness lifecycleHarness, ctx context.Context) error {
				_, err := harness.engine.ApplyConfiguration(ctx)
				return err
			},
		},
		{
			name: "unchanged config set apply",
			run: func(t *testing.T, harness lifecycleHarness, ctx context.Context) error {
				configuration, err := harness.engine.ReadConfiguration(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				_, err = harness.engine.UpdateConfiguration(ctx, ConfigurationUpdateRequest{
					Set: map[string]string{"NODE_PORT": configuration.Values["NODE_PORT"]}, Apply: true,
				})
				return err
			},
		},
		{
			name: "unchanged secret set apply",
			run: func(t *testing.T, harness lifecycleHarness, ctx context.Context) error {
				secretData, err := os.ReadFile(harness.paths.SecretFile)
				if err != nil {
					t.Fatal(err)
				}
				source := filepath.Join(t.TempDir(), "same-secret.key")
				if err := os.WriteFile(source, secretData, 0o600); err != nil {
					t.Fatal(err)
				}
				_, err = harness.engine.SetSecret(ctx, SecretUpdateRequest{File: source, Apply: true})
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newLifecycleHarness(t, "2.8.0-rnl.1")
			harness.install(t, false)
			harness.host.calls = nil
			harness.host.fail("restart", context.Canceled)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			if err := test.run(t, harness, ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("operation error = %v, want context cancellation", err)
			}
			if got := countCall(harness.host.calls, "restart"); got != 2 {
				t.Fatalf("restart calls = %d, want canceled attempt plus detached recovery; calls = %q", got, harness.host.calls)
			}
			if !containsCall(harness.host.calls, "wait-healthy:remnanode-lite") {
				t.Fatalf("detached recovery did not wait for health; calls = %q", harness.host.calls)
			}
		})
	}
}

func TestEngineSetSecretRejectsSymlinkAndConfigurationMutationUsesLifecycleLock(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	symlink := filepath.Join(t.TempDir(), "secret-link")
	if err := os.Symlink(harness.secret, symlink); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.engine.SetSecret(context.Background(), SecretUpdateRequest{File: symlink}); err == nil || !strings.Contains(err.Error(), "symbolic links") {
		t.Fatalf("SetSecret(symlink) error = %v", err)
	}

	lock, err := acquireOperationLock(harness.paths)
	if err != nil {
		t.Fatal(err)
	}
	_, updateErr := harness.engine.UpdateConfiguration(context.Background(), ConfigurationUpdateRequest{Set: map[string]string{"NODE_PORT": "12345"}})
	_, secretErr := harness.engine.SetSecret(context.Background(), SecretUpdateRequest{File: filepath.Join(t.TempDir(), "missing-secret")})
	if closeErr := lock.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if !errors.Is(updateErr, ErrConcurrentOperation) {
		t.Fatalf("UpdateConfiguration() error = %v, want ErrConcurrentOperation", updateErr)
	}
	if !errors.Is(secretErr, ErrConcurrentOperation) {
		t.Fatalf("SetSecret() error = %v, want ErrConcurrentOperation before reading source", secretErr)
	}
}

func mustReadTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
