package rnlctl

import (
	"fmt"
	"os"
	"runtime"
)

// Engine implements the durable Native Linux lifecycle state machine.
type Engine struct {
	paths        Paths
	host         HostController
	resolver     BundleResolver
	architecture string
	requireRoot  func() bool
	failure      FailureInjector
}

func NewEngine(options EngineOptions) *Engine {
	paths := options.Paths
	if paths.LibraryRoot == "" {
		paths = ProductionPaths()
	}
	if options.Host == nil {
		options.Host = NewLinuxHost(LinuxHostOptions{})
	}
	if options.Architecture == "" {
		options.Architecture = runtime.GOARCH
	}
	if options.RequireRoot == nil {
		options.RequireRoot = func() bool { return os.Geteuid() == 0 }
	}
	if options.Resolver == nil {
		options.Resolver = NewGitHubResolver(GitHubResolverOptions{})
	}
	return &Engine{
		paths: paths, host: options.Host, resolver: options.Resolver,
		architecture: options.Architecture, requireRoot: options.RequireRoot,
		failure: options.Failure,
	}
}

func (engine *Engine) requirePrivileges() error {
	if engine.requireRoot != nil && !engine.requireRoot() {
		return fmt.Errorf("Native lifecycle operations must run as root")
	}
	return nil
}

func (engine *Engine) checkpoint(name string) error {
	if engine.failure == nil {
		return nil
	}
	if err := engine.failure.Fail(name); err != nil {
		return fmt.Errorf("injected failure at %s: %w", name, err)
	}
	return nil
}
