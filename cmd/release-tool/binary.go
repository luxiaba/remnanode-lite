package main

import (
	"bytes"
	"debug/buildinfo"
	"debug/elf"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
)

type goModule struct {
	Path    string
	Version string
	Sum     string
	UsedBy  []string
}

func validateELFArchitecture(name string, data []byte, architecture string) error {
	file, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%s is not a valid ELF executable: %w", name, err)
	}
	defer file.Close()

	wantMachine := elf.EM_NONE
	switch architecture {
	case "amd64":
		wantMachine = elf.EM_X86_64
	case "arm64":
		wantMachine = elf.EM_AARCH64
	default:
		return fmt.Errorf("unsupported architecture %q", architecture)
	}
	if file.Class != elf.ELFCLASS64 || file.Data != elf.ELFDATA2LSB || file.Machine != wantMachine {
		return fmt.Errorf("%s ELF architecture is class=%s data=%s machine=%s, want linux/%s",
			name, file.Class, file.Data, file.Machine, architecture)
	}
	if file.Type != elf.ET_EXEC && file.Type != elf.ET_DYN {
		return fmt.Errorf("%s ELF type is %s, want an executable or position-independent executable", name, file.Type)
	}
	return nil
}

func collectGoModules(files []bundleFile) ([]goModule, error) {
	byIdentity := make(map[string]goModule)
	type commandIdentity struct {
		path   string
		module string
	}
	expectedCommands := map[string]commandIdentity{
		"bin/remnanode-lite": {path: "github.com/luxiaba/remnanode-lite/cmd/remnanode-lite", module: "github.com/luxiaba/remnanode-lite"},
		"bin/rnlctl":         {path: "github.com/luxiaba/remnanode-lite/cmd/rnlctl", module: "github.com/luxiaba/remnanode-lite"},
		"lib/rw-core":        {path: "github.com/xtls/xray-core/main", module: "github.com/xtls/xray-core"},
	}
	seenCommands := make(map[string]struct{}, len(expectedCommands))
	for _, file := range files {
		expectedCommand, wanted := expectedCommands[file.Path]
		if !wanted {
			continue
		}
		info, err := buildinfo.Read(bytes.NewReader(file.Data))
		if err != nil {
			return nil, fmt.Errorf("read Go build information from %s: %w", file.Path, err)
		}
		if info.Path != expectedCommand.path || info.Main.Path != expectedCommand.module {
			return nil, fmt.Errorf("%s Go build identity is %q from module %q, want %q",
				file.Path, info.Path, info.Main.Path, expectedCommand.path)
		}
		seenCommands[file.Path] = struct{}{}
		for _, dependency := range info.Deps {
			if dependency.Replace != nil {
				return nil, fmt.Errorf("%s uses replaced Go module %s; release binaries require an unreplaced module graph", file.Path, dependency.Path)
			}
			module := goModule{Path: dependency.Path, Version: dependency.Version, Sum: dependency.Sum}
			if module.Path == "" || module.Version == "" || module.Sum == "" {
				return nil, fmt.Errorf("%s contains incomplete Go module metadata for %q", file.Path, module.Path)
			}
			if !strings.HasPrefix(module.Sum, "h1:") {
				return nil, fmt.Errorf("%s contains unsupported Go module checksum for %s", file.Path, module.Path)
			}
			checksum, decodeErr := base64.StdEncoding.DecodeString(strings.TrimPrefix(module.Sum, "h1:"))
			if decodeErr != nil || len(checksum) != 32 {
				return nil, fmt.Errorf("%s contains invalid Go module checksum for %s", file.Path, module.Path)
			}
			identity := module.Path + "\x00" + module.Version
			existing, exists := byIdentity[identity]
			if exists && existing.Sum != module.Sum {
				return nil, fmt.Errorf("release binaries contain inconsistent checksums for Go module %s@%s", module.Path, module.Version)
			}
			if !exists {
				existing = module
			}
			existing.UsedBy = append(existing.UsedBy, file.Path)
			byIdentity[identity] = existing
		}
	}
	if len(seenCommands) != len(expectedCommands) {
		return nil, fmt.Errorf("project binary set is incomplete")
	}
	modules := make([]goModule, 0, len(byIdentity))
	for _, module := range byIdentity {
		sort.Strings(module.UsedBy)
		modules = append(modules, module)
	}
	sort.Slice(modules, func(left, right int) bool {
		if modules[left].Path == modules[right].Path {
			return modules[left].Version < modules[right].Version
		}
		return modules[left].Path < modules[right].Path
	})
	return modules, nil
}
