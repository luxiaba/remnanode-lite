package sourceoracle

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractorBindsRootModuleToNestFactoryBootstrap(t *testing.T) {
	repository := createOfficialFixture(t)
	replaceFixtureText(t, repository, mainSource,
		"import { AppModule } from './app.module';",
		"import { XrayModule } from './modules/xray/xray.module';",
	)
	replaceFixtureText(t, repository, mainSource, "NestFactory.create(AppModule", "NestFactory.create(XrayModule")
	err := generateChangedFixture(t, repository, "bootstrap wrong root")
	assertErrorContains(t, err, "want src/app.module.ts")
}

func TestExtractorRejectsMissingNestFactoryBootstrap(t *testing.T) {
	repository := createOfficialFixture(t)
	writeFixtureFile(t, repository, mainSource, `
import { ROOT } from '@example/api';
const app = { setGlobalPrefix() {} };
app.setGlobalPrefix(ROOT, { exclude: [] });
`)
	err := generateChangedFixture(t, repository, "remove bootstrap")
	assertErrorContains(t, err, "NestFactory.create")
}

func TestExtractorRejectsUninvokedNestBootstrap(t *testing.T) {
	repository := createOfficialFixture(t)
	replaceFixtureText(t, repository, mainSource, "void bootstrap();", "// bootstrap is never invoked")
	err := generateChangedFixture(t, repository, "remove bootstrap invocation")
	assertErrorContains(t, err, "must be invoked exactly once")
}

func TestExtractorRejectsConditionallyExecutedNestBootstrap(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		repository := createOfficialFixture(t)
		replaceFixtureText(t, repository, mainSource,
			"    const app = await NestFactory.create(AppModule, {});\n    app.setGlobalPrefix(ROOT, {",
			"    if (false) {\n        const app = await NestFactory.create(AppModule, {});\n        app.setGlobalPrefix(ROOT, {",
		)
		replaceFixtureText(t, repository, mainSource, "});\n}\nvoid bootstrap();", "        });\n    }\n}\nvoid bootstrap();")
		err := generateChangedFixture(t, repository, "conditionally create app")
		assertErrorContains(t, err, "NestFactory.create must be a top-level bootstrap statement")
	})

	t.Run("invocation", func(t *testing.T) {
		repository := createOfficialFixture(t)
		replaceFixtureText(t, repository, mainSource, "void bootstrap();", "if (false) { void bootstrap(); }")
		err := generateChangedFixture(t, repository, "conditionally invoke bootstrap")
		assertErrorContains(t, err, "void bootstrap() must be a top-level statement")
	})
}

func TestExtractorRejectsDynamicModuleGraphMetadata(t *testing.T) {
	tests := []struct {
		name        string
		old         string
		replacement string
	}{
		{name: "conditional", old: "    XrayModule,", replacement: "    true ? XrayModule : PluginModule,"},
		{name: "spread", old: "    XrayModule,", replacement: "    ...XrayModule,"},
		{name: "nested", old: "    XrayModule,", replacement: "    [XrayModule],"},
		{name: "property", old: "    XrayModule,", replacement: "    Modules.XrayModule,"},
		{name: "unapproved call", old: "    XrayModule,", replacement: "    XrayModule.forRoot(),"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := createOfficialFixture(t)
			replaceFixtureText(t, repository, rootModuleSource, test.old, test.replacement)
			err := generateChangedFixture(t, repository, "unsupported module metadata")
			assertErrorContains(t, err, "Nest module src/app.module.ts")
		})
	}
}

func TestExtractorRejectsConditionalControllerMetadata(t *testing.T) {
	repository := createOfficialFixture(t)
	replaceFixtureText(t, repository, "src/modules/xray/xray.module.ts",
		"controllers: [XrayController]",
		"controllers: [true ? XrayController : PluginController]",
	)
	err := generateChangedFixture(t, repository, "conditional controller")
	assertErrorContains(t, err, "not a bare identifier")
}

func TestExtractorAllowsOnlyPinnedDynamicModuleRegistrations(t *testing.T) {
	repository := createOfficialFixture(t)
	replaceFixtureText(t, repository, rootModuleSource,
		"from '@nestjs/config'",
		"from '@example/untrusted-config'",
	)
	err := generateChangedFixture(t, repository, "change dynamic import source")
	assertErrorContains(t, err, "must use a direct named import from @nestjs/config")
}

func TestExtractorAllowsOnlyPinnedExternalModuleImports(t *testing.T) {
	repository := createOfficialFixture(t)
	replaceFixtureText(t, repository, "src/modules/internal/internal.module.ts",
		"import { Module } from '@nestjs/common';",
		"import { Module } from '@nestjs/common';\nimport { CqrsModule } from '@example/cqrs';",
	)
	replaceFixtureText(t, repository, "src/modules/internal/internal.module.ts",
		"@Module({ controllers: [InternalController] })",
		"@Module({ imports: [CqrsModule], controllers: [InternalController] })",
	)
	err := generateChangedFixture(t, repository, "change external module source")
	assertErrorContains(t, err, "must use a direct named import from @nestjs/cqrs")
}

func TestExtractorRejectsAliasedModuleImport(t *testing.T) {
	repository := createOfficialFixture(t)
	replaceFixtureText(t, repository, rootModuleSource,
		"import { XrayModule } from './modules/xray/xray.module';",
		"import { XrayModule as AuditedModule } from './modules/xray/xray.module';",
	)
	replaceFixtureText(t, repository, rootModuleSource, "    XrayModule,", "    AuditedModule,")
	err := generateChangedFixture(t, repository, "alias module import")
	assertErrorContains(t, err, "without aliasing")
}

func TestExtractorRejectsModuleReferenceWithoutStaticImport(t *testing.T) {
	repository := createOfficialFixture(t)
	replaceFixtureText(t, repository, rootModuleSource,
		"import { XrayModule } from './modules/xray/xray.module';\n",
		"",
	)
	err := generateChangedFixture(t, repository, "remove module import")
	assertErrorContains(t, err, "without a static import binding")
}

func TestExtractorRejectsControllerOutsideNamingConvention(t *testing.T) {
	repository := createOfficialFixture(t)
	writeFixtureFile(t, repository, "src/modules/admin.ts", `
import { Controller, Get } from '@nestjs/common';
@Controller(XRAY_CONTROLLER)
export class AdminController {
    @Get(XRAY_ROUTES.HEALTH)
    public health(): void {}
}
`)
	err := generateChangedFixture(t, repository, "add nonconventional controller")
	assertErrorContains(t, err, "outside a *.controller.ts file")
}

func TestExtractorRejectsRegisteredUndiscoveredController(t *testing.T) {
	repository := createOfficialFixture(t)
	writeFixtureFile(t, repository, "src/modules/plugin/admin.ts", `export class AdminController {}`)
	replaceFixtureText(t, repository, "src/modules/plugin/plugin.module.ts",
		"import { PluginController } from './plugin.controller';",
		"import { PluginController } from './plugin.controller';\nimport { AdminController } from './admin';",
	)
	replaceFixtureText(t, repository, "src/modules/plugin/plugin.module.ts",
		"controllers: [PluginController]",
		"controllers: [PluginController, AdminController]",
	)
	err := generateChangedFixture(t, repository, "register undiscovered controller")
	assertErrorContains(t, err, "is not a discovered controller")
}

func TestExtractorBindsControllerDecoratorToExportedClass(t *testing.T) {
	repository := createOfficialFixture(t)
	writeFixtureFile(t, repository, "src/modules/xray/xray.controller.ts", `
import { Controller, Get } from '@nestjs/common';
@Controller(XRAY_CONTROLLER)
class DecoyController {
    @Get(XRAY_ROUTES.HEALTH)
    public health(): void {}
}
export class XrayController {}
`)
	err := generateChangedFixture(t, repository, "move controller decorator")
	assertErrorContains(t, err, "does not decorate exported class XrayController")
}

func TestExtractorRejectsRouteDecoratorOutsideControllerClass(t *testing.T) {
	repository := createOfficialFixture(t)
	writeFixtureFile(t, repository, "src/modules/xray/xray.controller.ts", `
import { Controller, Get } from '@nestjs/common';
@Controller(XRAY_CONTROLLER)
export class XrayController {}
class DecoyController {
    @Get(XRAY_ROUTES.HEALTH)
    public health(): void {}
}
`)
	err := generateChangedFixture(t, repository, "move route decorator")
	assertErrorContains(t, err, "HTTP route decorator outside exported controller class")
}

func TestExtractorRejectsAliasedAndQualifiedNestDecorators(t *testing.T) {
	t.Run("alias", func(t *testing.T) {
		repository := createOfficialFixture(t)
		replaceFixtureText(t, repository, "src/modules/xray/xray.controller.ts",
			"import { Controller, Get } from '@nestjs/common';",
			"import { Controller, Get as RouteGet } from '@nestjs/common';",
		)
		replaceFixtureText(t, repository, "src/modules/xray/xray.controller.ts", "@Get(", "@RouteGet(")
		err := generateChangedFixture(t, repository, "alias route decorator")
		assertErrorContains(t, err, "aliases Nest decorator Get as RouteGet")
	})

	t.Run("qualified", func(t *testing.T) {
		repository := createOfficialFixture(t)
		replaceFixtureText(t, repository, "src/modules/xray/xray.controller.ts",
			"import { Controller, Get } from '@nestjs/common';",
			"import * as Nest from '@nestjs/common';",
		)
		replaceFixtureText(t, repository, "src/modules/xray/xray.controller.ts", "@Controller(", "@Nest.Controller(")
		replaceFixtureText(t, repository, "src/modules/xray/xray.controller.ts", "@Get(", "@Nest.Get(")
		err := generateChangedFixture(t, repository, "qualify decorators")
		assertErrorContains(t, err, "qualified")
	})
}

func TestExtractorBindsExclusionsToGlobalPrefixCall(t *testing.T) {
	repository := createOfficialFixture(t)
	writeFixtureFile(t, repository, mainSource, `
import { NestFactory } from '@nestjs/core';
import { ROOT } from '@libs/contracts/api';
import { XRAY_INTERNAL_FULL_PATH, XRAY_INTERNAL_FULL_WEBHOOK_PATH } from '@libs/contracts/constants';
import { AppModule } from './app.module';
async function bootstrap(): Promise<void> {
    const app = await NestFactory.create(AppModule, {});
    const decoy = { exclude: [XRAY_INTERNAL_FULL_PATH, XRAY_INTERNAL_FULL_WEBHOOK_PATH] };
    void decoy;
    app.setGlobalPrefix(ROOT, {});
}
void bootstrap();
`)
	err := generateChangedFixture(t, repository, "move prefix exclusions")
	assertErrorContains(t, err, "does not exclude XRAY_INTERNAL_FULL_PATH")
}

func TestExtractorStrictlyParsesGlobalPrefixCall(t *testing.T) {
	t.Run("root expression", func(t *testing.T) {
		repository := createOfficialFixture(t)
		replaceFixtureText(t, repository, mainSource, "setGlobalPrefix(ROOT,", "setGlobalPrefix(ROOT + '/v2',")
		err := generateChangedFixture(t, repository, "change prefix expression")
		assertErrorContains(t, err, "first argument")
	})

	t.Run("different receiver", func(t *testing.T) {
		repository := createOfficialFixture(t)
		replaceFixtureText(t, repository, mainSource, "app.setGlobalPrefix", "other.setGlobalPrefix")
		err := generateChangedFixture(t, repository, "change prefix receiver")
		assertErrorContains(t, err, "not called on bootstrapped application app")
	})

	t.Run("conditional call", func(t *testing.T) {
		repository := createOfficialFixture(t)
		replaceFixtureText(t, repository, mainSource,
			"    app.setGlobalPrefix(ROOT, {",
			"    if (false) {\n        app.setGlobalPrefix(ROOT, {",
		)
		replaceFixtureText(t, repository, mainSource, "});\n}\nvoid bootstrap();", "        });\n    }\n}\nvoid bootstrap();")
		err := generateChangedFixture(t, repository, "conditionally set prefix")
		assertErrorContains(t, err, "setGlobalPrefix must be a top-level bootstrap statement")
	})

	t.Run("conditional exclusion", func(t *testing.T) {
		repository := createOfficialFixture(t)
		replaceFixtureText(t, repository, mainSource,
			"exclude: [XRAY_INTERNAL_FULL_PATH, XRAY_INTERNAL_FULL_WEBHOOK_PATH]",
			"exclude: [enabled ? XRAY_INTERNAL_FULL_PATH : XRAY_INTERNAL_FULL_WEBHOOK_PATH]",
		)
		err := generateChangedFixture(t, repository, "make exclusions conditional")
		assertErrorContains(t, err, "not a bare identifier")
	})

	t.Run("root import source", func(t *testing.T) {
		repository := createOfficialFixture(t)
		replaceFixtureText(t, repository, mainSource, "from '@libs/contracts/api'", "from '@example/api'")
		err := generateChangedFixture(t, repository, "change root import source")
		assertErrorContains(t, err, "ROOT must be a direct named import from @libs/contracts/api")
	})

	t.Run("exclusion import source", func(t *testing.T) {
		repository := createOfficialFixture(t)
		replaceFixtureText(t, repository, mainSource, "from '@libs/contracts/constants'", "from '@example/constants'")
		err := generateChangedFixture(t, repository, "change exclusion import source")
		assertErrorContains(t, err, "must be a direct named import from @libs/contracts/constants")
	})

	t.Run("extra exclusion", func(t *testing.T) {
		repository := createOfficialFixture(t)
		replaceFixtureText(t, repository, "libs/contract/constants/internal/internal.constants.ts",
			"export const XRAY_INTERNAL_WEBHOOK_PATH = '/webhook';",
			"export const EXTRA_INTERNAL_PATH = '/extra';\nexport const XRAY_INTERNAL_WEBHOOK_PATH = '/webhook';",
		)
		replaceFixtureText(t, repository, mainSource,
			"XRAY_INTERNAL_FULL_PATH, XRAY_INTERNAL_FULL_WEBHOOK_PATH } from '@libs/contracts/constants';",
			"XRAY_INTERNAL_FULL_PATH, XRAY_INTERNAL_FULL_WEBHOOK_PATH, EXTRA_INTERNAL_PATH } from '@libs/contracts/constants';",
		)
		replaceFixtureText(t, repository, mainSource,
			"exclude: [XRAY_INTERNAL_FULL_PATH, XRAY_INTERNAL_FULL_WEBHOOK_PATH]",
			"exclude: [XRAY_INTERNAL_FULL_PATH, XRAY_INTERNAL_FULL_WEBHOOK_PATH, EXTRA_INTERNAL_PATH]",
		)
		err := generateChangedFixture(t, repository, "add extra prefix exclusion")
		assertErrorContains(t, err, "global-prefix exclusions differ")
	})
}

func TestExtractorRejectsCustomCompositeRouteDecorator(t *testing.T) {
	repository := createOfficialFixture(t)
	replaceFixtureText(t, repository, "src/modules/xray/xray.controller.ts",
		"import { Controller, Get } from '@nestjs/common';",
		"import { Controller } from '@nestjs/common';\nimport { CustomGet } from './custom-route';",
	)
	replaceFixtureText(t, repository, "src/modules/xray/xray.controller.ts", "@Get(", "@CustomGet(")
	err := generateChangedFixture(t, repository, "use custom route decorator")
	assertErrorContains(t, err, "unsupported Nest decorator @CustomGet")
}

func TestExtractorRejectsNestRouteWrapperDecorator(t *testing.T) {
	repository := createOfficialFixture(t)
	replaceFixtureText(t, repository, "src/modules/xray/xray.controller.ts",
		"import { Controller, Get } from '@nestjs/common';",
		"import { Controller, applyDecorators } from '@nestjs/common';",
	)
	replaceFixtureText(t, repository, "src/modules/xray/xray.controller.ts", "@Get(", "@applyDecorators(")
	err := generateChangedFixture(t, repository, "use Nest route wrapper")
	assertErrorContains(t, err, "unsupported Nest decorator @applyDecorators")
}

func TestExtractorRejectsBareCompositeRouteDecorator(t *testing.T) {
	repository := createOfficialFixture(t)
	writeFixtureFile(t, repository, "src/modules/xray/xray.controller.ts", `
import { Controller, Get } from '@nestjs/common';
const HiddenGet = Get(XRAY_ROUTES.HEALTH);
@Controller(XRAY_CONTROLLER)
export class XrayController {
    @HiddenGet
    public health(): void {}
}
`)
	err := generateChangedFixture(t, repository, "use bare composite route decorator")
	assertErrorContains(t, err, "unsupported Nest decorator @HiddenGet")
}

func TestExtractorRejectsControllerWrapperOutsideNamingConvention(t *testing.T) {
	repository := createOfficialFixture(t)
	writeFixtureFile(t, repository, "src/modules/controller-wrapper.ts", `
import { Controller } from '@nestjs/common';
export const WrappedController = Controller('hidden');
`)
	err := generateChangedFixture(t, repository, "add controller wrapper")
	assertErrorContains(t, err, "imports Controller outside a *.controller.ts file")
}

func replaceFixtureText(t *testing.T, root, sourcePath, old, replacement string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(sourcePath))
	raw, err := os.ReadFile(absolute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), old) {
		t.Fatalf("%s does not contain replacement target %q", sourcePath, old)
	}
	updated := strings.Replace(string(raw), old, replacement, 1)
	writeFixtureFile(t, root, sourcePath, updated)
}

func generateChangedFixture(t *testing.T, repository, message string) error {
	t.Helper()
	runGit(t, repository, "add", ".")
	runGit(t, repository, "commit", "-m", message)
	commit := runGit(t, repository, "rev-parse", "HEAD")
	_, err := Generate(context.Background(), repository, fixtureExpectation(commit))
	return err
}

func assertErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want substring %q", err, want)
	}
}
