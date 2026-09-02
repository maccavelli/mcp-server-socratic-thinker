package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/maccavelli/mcplib/selfupdate"
	"github.com/spf13/cobra"
)

// newTestRoot builds a root command tree wired like the real one but with the
// updater construction seam replaced, so no test makes a live GitHub call.
func newTestRoot(t *testing.T, build func() (*selfupdate.Updater, error)) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	prev := newUpdater
	newUpdater = func(_, _ *os.File) (*selfupdate.Updater, error) { return build() }
	t.Cleanup(func() { newUpdater = prev })

	var out bytes.Buffer
	root := &cobra.Command{Use: cliName}
	root.PersistentPreRunE = rootCmd.PersistentPreRunE
	root.AddCommand(newUpdateCmd())
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(""))
	return root, &out
}

func failingBuild() (*selfupdate.Updater, error) {
	return nil, errors.New("updater construction should not have been reached")
}

// TestUpdateDoesNotInitializeConfig is the sentinel this phase exists for.
// Before MADR 0005 the root ran initConfig for every command through
// cobra.OnInitialize, so merely checking for an update would construct the
// config, start an fsnotify watcher, and pull in the Recall client, metrics
// store, telemetry and dashboard behind it. Cfg staying nil proves the
// annotated command opted out.
func TestUpdateDoesNotInitializeConfig(t *testing.T) {
	Cfg = nil
	root, _ := newTestRoot(t, failingBuild)
	root.SetArgs([]string{"update", "--check"})
	_ = root.ExecuteContext(context.Background())
	if Cfg != nil {
		t.Fatal("update initialized configuration; it must not start a config watcher or any runtime component")
	}
}

// TestOrdinaryCommandStillInitializesConfig proves the opt-out is scoped to the
// annotation and did not silently disable configuration for everything else.
func TestOrdinaryCommandStillInitializesConfig(t *testing.T) {
	Cfg = nil
	root := &cobra.Command{Use: cliName}
	root.PersistentPreRunE = rootCmd.PersistentPreRunE
	ran := false
	root.AddCommand(&cobra.Command{Use: "ordinary", RunE: func(*cobra.Command, []string) error { ran = true; return nil }})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"ordinary"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ordinary command failed: %v", err)
	}
	if !ran {
		t.Fatal("ordinary command did not run")
	}
	if Cfg == nil {
		t.Fatal("ordinary command did not initialize configuration; the opt-out is too wide")
	}
	Cfg = nil
}

func TestUpdateCommandIsAnnotatedToSkipConfig(t *testing.T) {
	if got := newUpdateCmd().Annotations[skipConfigAnnotation]; got != skipConfigValue {
		t.Fatalf("annotation = %q, want \"true\"", got)
	}
}

func TestUpdateFlagSurface(t *testing.T) {
	cmd := newUpdateCmd()
	for _, name := range []string{"check", "yes", "force", "version"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing --%s", name)
		}
	}
	if f := cmd.Flags().ShorthandLookup("y"); f == nil || f.Name != "yes" {
		t.Error("missing -y shorthand for --yes")
	}
}

func TestUpdateRejectsPositionalArgs(t *testing.T) {
	cmd := newUpdateCmd()
	if err := cmd.Args(cmd, []string{"stray"}); err == nil {
		t.Fatal("expected positional arguments to be rejected")
	}
}

// TestUpdateRejectsContradictoryFlags proves the contradiction is caught by the
// shared request validator before any network work.
func TestUpdateRejectsContradictoryFlags(t *testing.T) {
	for _, flag := range []string{"--yes", "--force"} {
		t.Run(flag, func(t *testing.T) {
			root, _ := newTestRoot(t, func() (*selfupdate.Updater, error) {
				return defaultUpdater(os.Stderr, os.Stderr)
			})
			root.SetArgs([]string{"update", "--check", flag})
			err := root.ExecuteContext(context.Background())
			if err == nil {
				t.Fatalf("--check %s was accepted", flag)
			}
			if errors.Is(err, selfupdate.ErrUpdateAvailable) {
				t.Fatal("contradiction was not detected before evaluation")
			}
		})
	}
}

// TestUpdateUsesCallerContext guards against a reintroduced context.Background.
func TestUpdateUsesCallerContext(t *testing.T) {
	root, _ := newTestRoot(t, func() (*selfupdate.Updater, error) {
		return defaultUpdater(os.Stderr, os.Stderr)
	})
	root.SetArgs([]string{"update", "--check"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := root.ExecuteContext(ctx); err == nil {
		t.Fatal("a cancelled caller context did not abort the command")
	}
}

func TestBuildKindMapping(t *testing.T) {
	prev := RawBuildKind
	t.Cleanup(func() { RawBuildKind = prev })
	for _, tc := range []struct {
		stamp string
		want  selfupdate.BuildKind
	}{
		{"release", selfupdate.ReleaseBuild},
		{"local", selfupdate.LocalBuild},
		{"", selfupdate.LocalBuild},
		{"Release", selfupdate.LocalBuild},
		{"release ", selfupdate.LocalBuild},
	} {
		RawBuildKind = tc.stamp
		if got := buildKind(); got != tc.want {
			t.Errorf("RawBuildKind=%q -> %v, want %v", tc.stamp, got, tc.want)
		}
	}
}

// TestDefaultVersionIsNotAReleaseIdentity is the regression guard for the
// hard-coded "v4.4.4" default, which outranked every real tag and would have
// made a locally built binary believe it was permanently up to date.
func TestDefaultVersionIsNotAReleaseIdentity(t *testing.T) {
	if RawVersion == "v4.4.4" {
		t.Fatal("RawVersion still defaults to the fabricated v4.4.4")
	}
	if RawBuildKind == releaseBuildKind {
		t.Fatal("an unstamped build must not claim to be a release build")
	}
	if err := selfupdate.NewStrictVersionPolicy().Validate(RawVersion); err == nil {
		t.Fatalf("default RawVersion %q validates as a release tag; it must not", RawVersion)
	}
}

func TestExitCodeMapping(t *testing.T) {
	if got := selfupdate.ExitCode(selfupdate.Result{}, nil); got != 0 {
		t.Errorf("nil error -> %d, want 0", got)
	}
	if got := selfupdate.ExitCode(selfupdate.Result{}, selfupdate.ErrUpdateAvailable); got != 10 {
		t.Errorf("ErrUpdateAvailable -> %d, want 10", got)
	}
	if got := selfupdate.ExitCode(selfupdate.Result{}, errors.New("boom")); got != 1 {
		t.Errorf("generic error -> %d, want 1", got)
	}
}

// TestUpdatePlatformsAreTheFrozenMatrix keeps the release matrix and the
// selector in agreement.
func TestUpdatePlatformsAreTheFrozenMatrix(t *testing.T) {
	want := map[string]bool{"linux/amd64": true, "darwin/arm64": true, "windows/amd64": true}
	if len(updatePlatforms) != len(want) {
		t.Fatalf("updatePlatforms = %v", updatePlatforms)
	}
	for _, p := range updatePlatforms {
		if !want[p.OS+"/"+p.Arch] {
			t.Errorf("unexpected platform %v", p)
		}
	}
	if _, err := selfupdate.NewExactAssetSelector(updatePlatforms); err != nil {
		t.Fatalf("selector rejected the frozen matrix: %v", err)
	}
}
