package sourceoracle

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateAndVerifyIgnoreDirtyWorktreeAndIndex(t *testing.T) {
	repository := createOfficialFixture(t)
	commit := runGit(t, repository, "rev-parse", "HEAD")
	expectation := fixtureExpectation(commit)

	first, err := Generate(context.Background(), repository, expectation)
	if err != nil {
		t.Fatalf("generate manifest: %v", err)
	}
	second, err := Generate(context.Background(), repository, expectation)
	if err != nil {
		t.Fatalf("regenerate manifest: %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("manifest generation is not deterministic")
	}
	if err := Validate(first, expectation); err != nil {
		t.Fatalf("validate generated manifest: %v", err)
	}

	writeFixtureFile(t, repository, apiRoutesSource, "export const ROOT = '/tampered' as const;\n")
	runGit(t, repository, "add", apiRoutesSource)
	replacementTree := runGit(t, repository, "write-tree")
	replacementCommit := runGit(t, repository, "commit-tree", replacementTree, "-p", commit, "-m", "replacement")
	runGit(t, repository, "replace", commit, replacementCommit)
	if replaced := runGit(t, repository, "cat-file", "blob", commit+":"+apiRoutesSource); !strings.Contains(replaced, "/tampered") {
		t.Fatalf("test replace ref did not redirect the ordinary Git lookup: %q", replaced)
	}
	writeFixtureFile(t, repository, apiRoutesSource, "not even TypeScript\n")
	if err := VerifySource(context.Background(), repository, first, expectation); err != nil {
		t.Fatalf("dirty worktree, index, or replace ref changed the pinned oracle: %v", err)
	}
}

func TestGeneratedManifestContainsMachineExtractedRoutes(t *testing.T) {
	repository := createOfficialFixture(t)
	commit := runGit(t, repository, "rev-parse", "HEAD")
	raw, err := Generate(context.Background(), repository, fixtureExpectation(commit))
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	want := []ManifestRoute{
		{
			Method:           "POST",
			Path:             "/node/plugin/nftables/block-ips",
			ControllerSource: "src/modules/plugin/plugin.controller.ts",
			Decorator:        "PLUGIN_ROUTES.NFTABLES.BLOCK_IPS (REST_API.PLUGIN.NFTABLES.BLOCK_IPS)",
		},
		{
			Method:           "GET",
			Path:             "/node/xray/healthcheck",
			ControllerSource: "src/modules/xray/xray.controller.ts",
			Decorator:        "XRAY_ROUTES.HEALTH (REST_API.XRAY.HEALTH)",
		},
	}
	if len(manifest.Routes) != len(want) {
		t.Fatalf("routes = %#v, want %#v", manifest.Routes, want)
	}
	for index := range want {
		if manifest.Routes[index] != want[index] {
			t.Errorf("route %d = %#v, want %#v", index, manifest.Routes[index], want[index])
		}
	}
}

func TestValidateRejectsContractRouteDrift(t *testing.T) {
	repository := createOfficialFixture(t)
	commit := runGit(t, repository, "rev-parse", "HEAD")
	expectation := fixtureExpectation(commit)
	raw, err := Generate(context.Background(), repository, expectation)
	if err != nil {
		t.Fatal(err)
	}
	expectation.Routes[0].Method = "DELETE"
	if err := Validate(raw, expectation); err == nil || !strings.Contains(err.Error(), "machine-extracted official routes differ") {
		t.Fatalf("Validate route drift error = %v", err)
	}
}

func TestValidateRejectsNonCanonicalManifest(t *testing.T) {
	repository := createOfficialFixture(t)
	commit := runGit(t, repository, "rev-parse", "HEAD")
	expectation := fixtureExpectation(commit)
	raw, err := Generate(context.Background(), repository, expectation)
	if err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte(" \n"), raw...)
	if err := Validate(tampered, expectation); err == nil || !strings.Contains(err.Error(), "not canonical") {
		t.Fatalf("Validate non-canonical manifest error = %v", err)
	}
}

func TestParserIgnoresExportsAndDecoratorsInCommentsAndStrings(t *testing.T) {
	t.Parallel()
	source := `
// export const COMMENTED = 'wrong';
const text = "export const STRING_VALUE = 'wrong'; @Post(FAKE.ROUTE)";
const marker = /@Delete(FAKE.REGEXP_ROUTE)/;
/* @Get(COMMENTED.ROUTE) */
export const REAL = 'value' as const;
@Get(REAL.ROUTE)
`
	expressions, err := parseExportedConstants(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(expressions) != 1 || expressions["REAL"] != "'value'" {
		t.Fatalf("expressions = %#v", expressions)
	}
	decorators := httpDecoratorPattern.FindAllStringSubmatch(maskTypeScriptNonCode(source), -1)
	if len(decorators) != 1 || decorators[0][1] != "Get" || decorators[0][2] != "REAL.ROUTE" {
		t.Fatalf("decorators = %#v", decorators)
	}
}

func TestParserRejectsDuplicateObjectKeys(t *testing.T) {
	t.Parallel()
	_, err := parseExportedConstants(`export const ROUTES = { SAME: 'one', SAME: 'two' } as const;`)
	if err == nil || !strings.Contains(err.Error(), "declared more than once") {
		t.Fatalf("duplicate key error = %v", err)
	}
}

func TestParserRejectsUnsupportedScalarSuffix(t *testing.T) {
	t.Parallel()
	_, err := parseExportedConstants(`export const ROOT = '/node' + '/v2' as const;`)
	if err == nil || !strings.Contains(err.Error(), "unsupported expression suffix") {
		t.Fatalf("unsupported suffix error = %v", err)
	}
}

func TestExtractorRejectsUnmatchedRESTAPIRoute(t *testing.T) {
	repository := createOfficialFixture(t)
	commit := runGit(t, repository, "rev-parse", "HEAD")
	writeFixtureFile(t, repository, apiRoutesSource,
		"export const ROOT = '/node' as const;\n"+
			"export const REST_API = {\n"+
			"    XRAY: {\n"+
			"        HEALTH: `${ROOT}/${CONTROLLERS.XRAY_CONTROLLER}/${CONTROLLERS.XRAY_ROUTES.HEALTH}`,\n"+
			"        NEW_ROUTE: `${ROOT}/${CONTROLLERS.XRAY_CONTROLLER}/new-route`,\n"+
			"    },\n"+
			"    PLUGIN: {\n"+
			"        NFTABLES: {\n"+
			"            BLOCK_IPS: `${ROOT}/${CONTROLLERS.PLUGIN_CONTROLLER}/${CONTROLLERS.PLUGIN_ROUTES.NFTABLES.BLOCK_IPS}`,\n"+
			"        },\n"+
			"    },\n"+
			"} as const;\n",
	)
	runGit(t, repository, "add", apiRoutesSource)
	runGit(t, repository, "commit", "-m", "add unmatched route")
	commit = runGit(t, repository, "rev-parse", "HEAD")
	expectation := fixtureExpectation(commit)
	_, err := Generate(context.Background(), repository, expectation)
	if err == nil || !strings.Contains(err.Error(), "no matching controller decorator") {
		t.Fatalf("Generate unmatched REST_API route error = %v", err)
	}
}

func TestExtractorRejectsUnregisteredController(t *testing.T) {
	repository := createOfficialFixture(t)
	writeFixtureFile(t, repository, "src/modules/xray/xray.module.ts", `
import { Module } from '@nestjs/common';
import { XrayController } from './xray.controller';
@Module({ controllers: [] })
export class XrayModule {}
`)
	runGit(t, repository, "add", ".")
	runGit(t, repository, "commit", "-m", "unregister controller")
	commit := runGit(t, repository, "rev-parse", "HEAD")
	_, err := Generate(context.Background(), repository, fixtureExpectation(commit))
	if err == nil || !strings.Contains(err.Error(), "registered by 0 reachable Nest modules") {
		t.Fatalf("Generate unregistered controller error = %v", err)
	}
}

func TestExtractorRejectsUndeclaredControllerSource(t *testing.T) {
	repository := createOfficialFixture(t)
	writeFixtureFile(t, repository, "src/modules/extra/extra.controller.ts", `
@Controller(EXTRA_CONTROLLER)
export class ExtraController {
    @Get(EXTRA_ROUTES.STATUS)
    public status(): void {}
}
`)
	runGit(t, repository, "add", ".")
	runGit(t, repository, "commit", "-m", "add controller")
	commit := runGit(t, repository, "rev-parse", "HEAD")
	_, err := Generate(context.Background(), repository, fixtureExpectation(commit))
	if err == nil || !strings.Contains(err.Error(), "official controller inventory differs") {
		t.Fatalf("Generate undeclared controller error = %v", err)
	}
}

func TestExtractorRejectsMissingInternalPrefixExclusion(t *testing.T) {
	repository := createOfficialFixture(t)
	writeFixtureFile(t, repository, mainSource, fixtureMainSource(`{}`))
	runGit(t, repository, "add", ".")
	runGit(t, repository, "commit", "-m", "remove internal exclusions")
	commit := runGit(t, repository, "rev-parse", "HEAD")
	_, err := Generate(context.Background(), repository, fixtureExpectation(commit))
	if err == nil || !strings.Contains(err.Error(), "does not exclude XRAY_INTERNAL_FULL_PATH") {
		t.Fatalf("Generate missing prefix exclusion error = %v", err)
	}
}

func createOfficialFixture(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	runGit(t, repository, "init", "--object-format=sha1")
	runGit(t, repository, "config", "user.name", "Contract Test")
	runGit(t, repository, "config", "user.email", "contract@example.test")
	runGit(t, repository, "config", "commit.gpgsign", "false")

	files := map[string]string{
		"package.json": `{"name":"@example/node","version":"1.2.3"}`,
		mainSource: fixtureMainSource(`{
    exclude: [XRAY_INTERNAL_FULL_PATH, XRAY_INTERNAL_FULL_WEBHOOK_PATH],
}`),
		apiRoutesSource: "export const ROOT = '/node' as const;\n" +
			"export const REST_API = {\n" +
			"    XRAY: {\n" +
			"        HEALTH: `${ROOT}/${CONTROLLERS.XRAY_CONTROLLER}/${CONTROLLERS.XRAY_ROUTES.HEALTH}`,\n" +
			"    },\n" +
			"    PLUGIN: {\n" +
			"        NFTABLES: {\n" +
			"            BLOCK_IPS: `${ROOT}/${CONTROLLERS.PLUGIN_CONTROLLER}/${CONTROLLERS.PLUGIN_ROUTES.NFTABLES.BLOCK_IPS}`,\n" +
			"        },\n" +
			"    },\n" +
			"} as const;\n",
		"libs/contract/api/controllers/xray.ts": `
export const XRAY_CONTROLLER = 'xray' as const;
export const XRAY_ROUTES = { HEALTH: 'healthcheck' } as const;
`,
		"libs/contract/api/controllers/plugin.ts": "export const PLUGIN_CONTROLLER = 'plugin' as const;\n" +
			"export const NFTABLES_ROUTE = 'nftables' as const;\n" +
			"export const PLUGIN_ROUTES = {\n" +
			"    NFTABLES: {\n" +
			"        BLOCK_IPS: `${NFTABLES_ROUTE}/block-ips`,\n" +
			"    },\n" +
			"} as const;\n",
		"libs/contract/constants/internal/internal.constants.ts": "export const XRAY_INTERNAL_API_CONTROLLER = 'internal';\n" +
			"export const XRAY_INTERNAL_API_PATH = '/get-config';\n" +
			"export const XRAY_INTERNAL_FULL_PATH = `/${XRAY_INTERNAL_API_CONTROLLER}${XRAY_INTERNAL_API_PATH}`;\n" +
			"export const XRAY_INTERNAL_WEBHOOK_PATH = '/webhook';\n" +
			"export const XRAY_INTERNAL_FULL_WEBHOOK_PATH = `/${XRAY_INTERNAL_API_CONTROLLER}${XRAY_INTERNAL_WEBHOOK_PATH}`;\n",
		"src/modules/xray/xray.controller.ts": `
import { Controller, Get } from '@nestjs/common';
@Controller(XRAY_CONTROLLER)
export class XrayController {
    @Get(XRAY_ROUTES.HEALTH)
    public health(): void {}
}
`,
		"src/modules/plugin/plugin.controller.ts": `
import { Controller, Post } from '@nestjs/common';
@Controller(PLUGIN_CONTROLLER)
export class PluginController {
    @Post(PLUGIN_ROUTES.NFTABLES.BLOCK_IPS)
    public block(): void {}
}
`,
		"src/modules/internal/internal.controller.ts": `
import { Controller, Get, Post } from '@nestjs/common';
@Controller(XRAY_INTERNAL_API_CONTROLLER)
export class InternalController {
    @Get(XRAY_INTERNAL_API_PATH)
    public config(): void {}

    @Post(XRAY_INTERNAL_WEBHOOK_PATH)
    public webhook(): void {}
}
`,
		rootModuleSource: `
import { Module } from '@nestjs/common';
import { ConfigModule } from '@nestjs/config';
import { JwtModule } from '@nestjs/jwt';
import { ScheduleModule } from '@nestjs/schedule';
import { XtlsSdkNestjsModule } from '@remnawave/xtls-sdk-nestjs';
import { XrayModule } from './modules/xray/xray.module';
import { PluginModule } from './modules/plugin/plugin.module';
import { InternalModule } from './modules/internal/internal.module';
@Module({ imports: [
    ConfigModule.forRoot({}),
    JwtModule.registerAsync({}),
    ScheduleModule.forRoot(),
    XtlsSdkNestjsModule.forRootAsync({}),
    XrayModule,
    PluginModule,
    InternalModule,
] })
export class AppModule {}
`,
		"src/modules/xray/xray.module.ts": `
import { Module } from '@nestjs/common';
import { XrayController } from './xray.controller';
@Module({ controllers: [XrayController] })
export class XrayModule {}
`,
		"src/modules/plugin/plugin.module.ts": `
import { Module } from '@nestjs/common';
import { PluginController } from './plugin.controller';
@Module({ controllers: [PluginController] })
export class PluginModule {}
`,
		"src/modules/internal/internal.module.ts": `
import { Module } from '@nestjs/common';
import { InternalController } from './internal.controller';
@Module({ controllers: [InternalController] })
export class InternalModule {}
`,
	}
	for path, content := range files {
		writeFixtureFile(t, repository, path, content)
	}
	runGit(t, repository, "add", ".")
	runGit(t, repository, "commit", "-m", "fixture")
	return repository
}

func fixtureMainSource(prefixOptions string) string {
	return `
import { NestFactory } from '@nestjs/core';
import { ROOT } from '@libs/contracts/api';
import { XRAY_INTERNAL_FULL_PATH, XRAY_INTERNAL_FULL_WEBHOOK_PATH } from '@libs/contracts/constants';
import { AppModule } from './app.module';
async function bootstrap(): Promise<void> {
    const app = await NestFactory.create(AppModule, {});
    app.setGlobalPrefix(ROOT, ` + prefixOptions + `);
}
void bootstrap();
`
}

func fixtureExpectation(commit string) Expectation {
	files := []string{
		"package.json",
		mainSource,
		apiRoutesSource,
		"libs/contract/api/controllers/xray.ts",
		"libs/contract/api/controllers/plugin.ts",
		"libs/contract/constants/internal/internal.constants.ts",
		"src/modules/xray/xray.controller.ts",
		"src/modules/plugin/plugin.controller.ts",
		"src/modules/internal/internal.controller.ts",
		rootModuleSource,
		"src/modules/xray/xray.module.ts",
		"src/modules/plugin/plugin.module.ts",
		"src/modules/internal/internal.module.ts",
	}
	return Expectation{
		Repository:  "https://example.test/node.git",
		PackageName: "@example/node",
		Version:     "1.2.3",
		Commit:      commit,
		Files:       files,
		Routes: []ExpectedRoute{
			{Method: "GET", Path: "/node/xray/healthcheck", ControllerSource: "src/modules/xray/xray.controller.ts"},
			{Method: "POST", Path: "/node/plugin/nftables/block-ips", ControllerSource: "src/modules/plugin/plugin.controller.ts"},
		},
		ExcludedControllers: []ExcludedController{
			{
				Source: "src/modules/internal/internal.controller.ts",
				RequiredPrefixExclusions: []string{
					"XRAY_INTERNAL_FULL_PATH",
					"XRAY_INTERNAL_FULL_WEBHOOK_PATH",
				},
			},
		},
	}
}

func writeFixtureFile(t *testing.T, root, path, content string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, arguments...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}
