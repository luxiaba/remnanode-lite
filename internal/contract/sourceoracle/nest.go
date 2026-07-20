package sourceoracle

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

var (
	exportClassPattern = regexp.MustCompile(`\bexport\s+class\s+([A-Za-z_$][A-Za-z0-9_$]*)\b`)
	bareIdentifier     = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

	moduleDecoratorPattern     = regexp.MustCompile(`@Module\s*\(`)
	controllerDecoratorPattern = regexp.MustCompile(`@Controller\s*\(`)
	decoratorTokenPattern      = regexp.MustCompile(`@\s*([A-Za-z_$][A-Za-z0-9_$]*)\b`)
	qualifiedDecoratorPattern  = regexp.MustCompile(`@\s*[A-Za-z_$][A-Za-z0-9_$]*\s*\.\s*[A-Za-z_$][A-Za-z0-9_$]*\s*\(`)
	importTokenPattern         = regexp.MustCompile(`\bimport\b`)
	staticImportPattern        = regexp.MustCompile(`(?s)^\s*import\s+(.+?)\s+from\s+['"]([^'"]+)['"]\s*;\s*$`)
	reExportPattern            = regexp.MustCompile(`\bexport\s*(?:\{|\*)`)
	dynamicModuleCallPattern   = regexp.MustCompile(`^([A-Za-z_$][A-Za-z0-9_$]*)\.([A-Za-z_$][A-Za-z0-9_$]*)\s*\(`)
	nestFactoryCreatePattern   = regexp.MustCompile(`\bNestFactory\s*\.\s*create\s*\(`)
	nestBootstrapPattern       = regexp.MustCompile(`\bconst\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*await\s+NestFactory\s*\.\s*create\s*\(`)
	bootstrapFunctionPattern   = regexp.MustCompile(`\basync\s+function\s+bootstrap\s*\([^)]*\)\s*(?::[^\{]+)?\s*\{`)
	bootstrapInvocationPattern = regexp.MustCompile(`\bvoid\s+bootstrap\s*\(\s*\)\s*;`)
)

var supportedNestDecoratorImports = map[string]struct{}{
	"All": {}, "Controller": {}, "Delete": {}, "Get": {}, "Head": {},
	"Module": {}, "Options": {}, "Patch": {}, "Post": {}, "Put": {}, "Sse": {},
}

var allowedNestDecorators = map[string]struct{}{
	"All": {}, "Body": {}, "Controller": {}, "Delete": {}, "Get": {},
	"Global": {}, "Head": {}, "HttpCode": {}, "Ip": {}, "Module": {}, "Options": {},
	"Patch": {}, "Post": {}, "Put": {}, "Sse": {}, "UseFilters": {},
	"UseGuards": {},
}

// The pinned AppModule uses these framework registrations. They are not
// source-graph edges, so each is allowed only at its exact source and import.
var allowedDynamicModuleImports = map[string]map[string]string{
	rootModuleSource: {
		"ConfigModule.forRoot":             "@nestjs/config",
		"JwtModule.registerAsync":          "@nestjs/jwt",
		"ScheduleModule.forRoot":           "@nestjs/schedule",
		"XtlsSdkNestjsModule.forRootAsync": "@remnawave/xtls-sdk-nestjs",
	},
}

var allowedExternalModuleImports = map[string]map[string]string{
	"src/modules/_plugin/plugin.module.ts": {
		"CqrsModule": "@nestjs/cqrs",
	},
	"src/modules/asn-lmdb/asn-lmdb.module.ts": {
		"CqrsModule": "@nestjs/cqrs",
	},
	"src/modules/handler/handler.module.ts": {
		"CqrsModule": "@nestjs/cqrs",
	},
	"src/modules/internal/internal.module.ts": {
		"CqrsModule": "@nestjs/cqrs",
	},
	"src/modules/network-stats/network-stats.module.ts": {
		"CqrsModule": "@nestjs/cqrs",
	},
	"src/modules/stats/stats.module.ts": {
		"CqrsModule": "@nestjs/cqrs",
	},
	"src/modules/xray-core/xray.module.ts": {
		"CqrsModule": "@nestjs/cqrs",
	},
}

func discoverNestSources(treeFiles []string) (controllers, modules []string) {
	for _, sourcePath := range treeFiles {
		switch {
		case strings.HasSuffix(sourcePath, ".controller.ts"):
			controllers = append(controllers, sourcePath)
		case strings.HasSuffix(sourcePath, ".module.ts"), strings.HasSuffix(sourcePath, ".modules.ts"):
			modules = append(modules, sourcePath)
		}
	}
	sort.Strings(controllers)
	sort.Strings(modules)
	return controllers, modules
}

type importKind uint8

const (
	importDefault importKind = iota + 1
	importNamed
	importNamespace
)

type importBinding struct {
	imported string
	local    string
	source   string
	kind     importKind
}

func (binding importBinding) isDirectNamed(imported, source string) bool {
	return binding.kind == importNamed && binding.imported == imported &&
		binding.local == imported && binding.source == source
}

type decoratedClass struct {
	name              string
	code              string
	bodyStart         int
	bodyEnd           int
	decoratorArgument string
	imports           map[string]importBinding
}

type nestModule struct {
	className       string
	source          string
	importItems     []string
	controllerNames []string
	bindings        map[string]importBinding
}

type nestBootstrap struct {
	applicationVariable string
	rootClass           string
	rootSource          string
	code                string
	bindings            map[string]importBinding
	functionBodyStart   int
	functionBodyEnd     int
}

func verifyNestModuleReachability(
	ctx context.Context,
	reader gitObjectReader,
	controllerSources, moduleSources []string,
) error {
	controllerClasses := make(map[string]string, len(controllerSources))
	controllersBySource := make(map[string]string, len(controllerSources))
	for _, sourcePath := range controllerSources {
		raw, err := reader.readBlob(ctx, sourcePath)
		if err != nil {
			return err
		}
		controller, err := parseNestController(sourcePath, string(raw))
		if err != nil {
			return err
		}
		if previous, exists := controllerClasses[controller.name]; exists {
			return fmt.Errorf("controller class %s is exported by both %s and %s", controller.name, previous, sourcePath)
		}
		controllerClasses[controller.name] = sourcePath
		controllersBySource[sourcePath] = controller.name
	}

	modules := make(map[string]nestModule, len(moduleSources))
	modulesBySource := make(map[string]string, len(moduleSources))
	for _, sourcePath := range moduleSources {
		raw, err := reader.readBlob(ctx, sourcePath)
		if err != nil {
			return err
		}
		module, err := parseNestModule(sourcePath, string(raw))
		if err != nil {
			return err
		}
		if previous, exists := modules[module.className]; exists {
			return fmt.Errorf("Nest module class %s is exported by both %s and %s", module.className, previous.source, sourcePath)
		}
		modules[module.className] = module
		modulesBySource[sourcePath] = module.className
	}

	mainRaw, err := reader.readBlob(ctx, mainSource)
	if err != nil {
		return err
	}
	bootstrap, err := parseNestBootstrap(string(mainRaw))
	if err != nil {
		return fmt.Errorf("parse %s bootstrap: %w", mainSource, err)
	}
	if bootstrap.rootSource != rootModuleSource {
		return fmt.Errorf("%s bootstraps %s from %s, want %s", mainSource, bootstrap.rootClass, bootstrap.rootSource, rootModuleSource)
	}
	rootClass, exists := modulesBySource[bootstrap.rootSource]
	if !exists || rootClass != bootstrap.rootClass {
		return fmt.Errorf("%s bootstraps %s from %s, which is not the audited Nest root module", mainSource, bootstrap.rootClass, bootstrap.rootSource)
	}

	edges := make(map[string][]string, len(modules))
	registrations := make(map[string][]string, len(controllerClasses))
	for _, module := range modules {
		for _, item := range module.importItems {
			importedClass, err := resolveModuleMetadataImport(module, item, modulesBySource)
			if err != nil {
				return err
			}
			if importedClass != "" {
				edges[module.className] = append(edges[module.className], importedClass)
			}
		}
		for _, controllerName := range module.controllerNames {
			binding, exists := module.bindings[controllerName]
			if !exists {
				return fmt.Errorf("Nest module %s references controller %s without a static import binding", module.source, controllerName)
			}
			if binding.kind != importNamed || binding.imported != controllerName || binding.local != controllerName {
				return fmt.Errorf("Nest module %s controller %s must use a direct named import without aliasing", module.source, controllerName)
			}
			controllerSource, err := resolveRelativeTypeScriptImport(module.source, binding.source)
			if err != nil {
				return fmt.Errorf("Nest module %s controller %s: %w", module.source, controllerName, err)
			}
			actualClass, exists := controllersBySource[controllerSource]
			if !exists {
				return fmt.Errorf("Nest module %s registers %s from %s, which is not a discovered controller", module.source, controllerName, controllerSource)
			}
			if actualClass != controllerName {
				return fmt.Errorf("Nest module %s imports controller %s from %s, which exports %s", module.source, controllerName, controllerSource, actualClass)
			}
			registrations[controllerName] = append(registrations[controllerName], module.source)
		}
	}

	reachable := map[string]struct{}{rootClass: {}}
	queue := []string{rootClass}
	for len(queue) != 0 {
		current := queue[0]
		queue = queue[1:]
		for _, imported := range edges[current] {
			if _, visited := reachable[imported]; visited {
				continue
			}
			reachable[imported] = struct{}{}
			queue = append(queue, imported)
		}
	}

	for className, sourcePath := range controllerClasses {
		registeredBy := registrations[className]
		var reachableRegistrations []string
		for _, moduleSource := range registeredBy {
			moduleClass := modulesBySource[moduleSource]
			if _, ok := reachable[moduleClass]; ok {
				reachableRegistrations = append(reachableRegistrations, moduleSource)
			}
		}
		if len(reachableRegistrations) != 1 {
			sort.Strings(reachableRegistrations)
			return fmt.Errorf(
				"controller %s from %s is registered by %d reachable Nest modules (%s), want exactly one",
				className,
				sourcePath,
				len(reachableRegistrations),
				strings.Join(reachableRegistrations, ", "),
			)
		}
	}
	return nil
}

func parseNestModule(sourcePath, source string) (nestModule, error) {
	class, err := parseDecoratedExportedClass(sourcePath, source, "Module", moduleDecoratorPattern)
	if err != nil {
		return nestModule{}, fmt.Errorf("parse Nest module %s: %w", sourcePath, err)
	}
	if err := requireDirectNestDecoratorImport(sourcePath, class.imports, "Module"); err != nil {
		return nestModule{}, err
	}
	properties, err := parseObjectLiteral(class.decoratorArgument)
	if err != nil {
		return nestModule{}, fmt.Errorf("parse Nest module %s metadata: %w", sourcePath, err)
	}
	imports, err := parseTopLevelArray(properties["imports"])
	if err != nil {
		return nestModule{}, fmt.Errorf("parse Nest module %s imports: %w", sourcePath, err)
	}
	controllers, err := parseBareIdentifierArray(properties["controllers"])
	if err != nil {
		return nestModule{}, fmt.Errorf("parse Nest module %s controllers: %w", sourcePath, err)
	}
	return nestModule{
		className:       class.name,
		source:          sourcePath,
		importItems:     imports,
		controllerNames: controllers,
		bindings:        class.imports,
	}, nil
}

func parseNestController(sourcePath, source string) (decoratedClass, error) {
	class, err := parseDecoratedExportedClass(sourcePath, source, "Controller", controllerDecoratorPattern)
	if err != nil {
		return decoratedClass{}, fmt.Errorf("parse controller class %s: %w", sourcePath, err)
	}
	if err := requireDirectNestDecoratorImport(sourcePath, class.imports, "Controller"); err != nil {
		return decoratedClass{}, err
	}
	return class, nil
}

func parseDecoratedExportedClass(
	sourcePath, source, decoratorName string,
	decoratorPattern *regexp.Regexp,
) (decoratedClass, error) {
	code := maskTypeScriptNonCode(source)
	if qualifiedDecoratorPattern.MatchString(code) {
		return decoratedClass{}, fmt.Errorf("qualified Nest decorators are unsupported")
	}
	if reExportPattern.MatchString(code) {
		return decoratedClass{}, fmt.Errorf("re-exports are unsupported in audited Nest sources")
	}
	bindings, err := parseStaticImports(source)
	if err != nil {
		return decoratedClass{}, err
	}
	if err := rejectAliasedNestDecoratorImports(sourcePath, bindings); err != nil {
		return decoratedClass{}, err
	}
	if err := verifyNestDecoratorImports(sourcePath, code, bindings); err != nil {
		return decoratedClass{}, err
	}

	classes := exportClassPattern.FindAllStringSubmatchIndex(code, -1)
	if len(classes) != 1 {
		return decoratedClass{}, fmt.Errorf("found %d exported classes, want exactly one", len(classes))
	}
	className := code[classes[0][2]:classes[0][3]]
	classStart := classes[0][0]
	bodyStart := strings.IndexByte(code[classes[0][1]:], '{')
	if bodyStart < 0 {
		return decoratedClass{}, fmt.Errorf("exported class %s has no body", className)
	}
	bodyStart += classes[0][1]
	bodyEnd, err := balancedEnd(code, bodyStart, '{', '}')
	if err != nil {
		return decoratedClass{}, fmt.Errorf("exported class %s: %w", className, err)
	}

	decorators := decoratorPattern.FindAllStringIndex(code, -1)
	if len(decorators) != 1 {
		return decoratedClass{}, fmt.Errorf("found %d @%s decorators, want exactly one", len(decorators), decoratorName)
	}
	open := decorators[0][1] - 1
	decoratorEnd, err := balancedEnd(code, open, '(', ')')
	if err != nil {
		return decoratedClass{}, fmt.Errorf("@%s: %w", decoratorName, err)
	}
	if decoratorEnd >= classStart || strings.TrimSpace(code[decoratorEnd+1:classStart]) != "" {
		return decoratedClass{}, fmt.Errorf("@%s does not decorate exported class %s", decoratorName, className)
	}
	return decoratedClass{
		name:              className,
		code:              code,
		bodyStart:         bodyStart,
		bodyEnd:           bodyEnd,
		decoratorArgument: strings.TrimSpace(code[open+1 : decoratorEnd]),
		imports:           bindings,
	}, nil
}

func parseNestBootstrap(source string) (nestBootstrap, error) {
	code := maskTypeScriptNonCode(source)
	if len(nestFactoryCreatePattern.FindAllStringIndex(code, -1)) != 1 {
		return nestBootstrap{}, fmt.Errorf("found unsupported or ambiguous NestFactory.create bootstrap")
	}
	matches := nestBootstrapPattern.FindAllStringSubmatchIndex(code, -1)
	if len(matches) != 1 {
		return nestBootstrap{}, fmt.Errorf("NestFactory.create must be assigned with const APP = await NestFactory.create(...)")
	}
	functions := bootstrapFunctionPattern.FindAllStringIndex(code, -1)
	if len(functions) != 1 {
		return nestBootstrap{}, fmt.Errorf("found %d async bootstrap functions, want exactly one", len(functions))
	}
	functionBodyStart := functions[0][1] - 1
	functionBodyEnd, err := balancedEnd(code, functionBodyStart, '{', '}')
	if err != nil {
		return nestBootstrap{}, fmt.Errorf("parse bootstrap function: %w", err)
	}
	if matches[0][0] <= functionBodyStart || matches[0][1] >= functionBodyEnd {
		return nestBootstrap{}, fmt.Errorf("NestFactory.create is not owned by async bootstrap function")
	}
	if depth, err := braceDepth(code, functionBodyStart+1, matches[0][0]); err != nil || depth != 0 {
		return nestBootstrap{}, fmt.Errorf("NestFactory.create must be a top-level bootstrap statement")
	}
	invocations := bootstrapInvocationPattern.FindAllStringIndex(code, -1)
	if len(invocations) != 1 || invocations[0][0] <= functionBodyEnd {
		return nestBootstrap{}, fmt.Errorf("async bootstrap function must be invoked exactly once with void bootstrap()")
	}
	if depth, err := braceDepth(code, 0, invocations[0][0]); err != nil || depth != 0 {
		return nestBootstrap{}, fmt.Errorf("void bootstrap() must be a top-level statement")
	}
	bindings, err := parseStaticImports(source)
	if err != nil {
		return nestBootstrap{}, err
	}
	if binding, exists := bindings["NestFactory"]; !exists || !binding.isDirectNamed("NestFactory", "@nestjs/core") {
		return nestBootstrap{}, fmt.Errorf("NestFactory must be a direct named import from @nestjs/core")
	}
	open := matches[0][1] - 1
	end, err := balancedEnd(code, open, '(', ')')
	if err != nil {
		return nestBootstrap{}, err
	}
	arguments, err := splitTopLevel(code[open+1:end], ',')
	if err != nil {
		return nestBootstrap{}, fmt.Errorf("parse NestFactory.create arguments: %w", err)
	}
	if len(arguments) == 0 || strings.TrimSpace(arguments[0]) == "" {
		return nestBootstrap{}, fmt.Errorf("NestFactory.create has no root module argument")
	}
	rootClass := strings.TrimSpace(arguments[0])
	if !bareIdentifier.MatchString(rootClass) {
		return nestBootstrap{}, fmt.Errorf("NestFactory.create root %q is not a bare identifier", rootClass)
	}
	binding, exists := bindings[rootClass]
	if !exists || binding.kind != importNamed || binding.imported != rootClass || binding.local != rootClass {
		return nestBootstrap{}, fmt.Errorf("NestFactory.create root %s must use a direct named import without aliasing", rootClass)
	}
	rootSource, err := resolveRelativeTypeScriptImport(mainSource, binding.source)
	if err != nil {
		return nestBootstrap{}, fmt.Errorf("NestFactory.create root %s: %w", rootClass, err)
	}
	return nestBootstrap{
		applicationVariable: code[matches[0][2]:matches[0][3]],
		rootClass:           rootClass,
		rootSource:          rootSource,
		code:                code,
		bindings:            bindings,
		functionBodyStart:   functionBodyStart,
		functionBodyEnd:     functionBodyEnd,
	}, nil
}

func resolveModuleMetadataImport(module nestModule, item string, modulesBySource map[string]string) (string, error) {
	item = strings.TrimSpace(item)
	if bareIdentifier.MatchString(item) {
		binding, exists := module.bindings[item]
		if !exists {
			return "", fmt.Errorf("Nest module %s imports %s without a static import binding", module.source, item)
		}
		if binding.kind != importNamed || binding.imported != item || binding.local != item {
			return "", fmt.Errorf("Nest module %s import %s must be a direct named import without aliasing", module.source, item)
		}
		if !strings.HasPrefix(binding.source, ".") {
			wantSource := allowedExternalModuleImports[module.source][item]
			if wantSource == "" {
				return "", fmt.Errorf("Nest module %s has unapproved external module import %s from %s", module.source, item, binding.source)
			}
			if !binding.isDirectNamed(item, wantSource) {
				return "", fmt.Errorf("Nest module %s external import %s must use a direct named import from %s", module.source, item, wantSource)
			}
			return "", nil
		}
		importedSource, err := resolveRelativeTypeScriptImport(module.source, binding.source)
		if err != nil {
			return "", fmt.Errorf("Nest module %s import %s: %w", module.source, item, err)
		}
		className, exists := modulesBySource[importedSource]
		if !exists {
			return "", fmt.Errorf("Nest module %s imports %s from %s, which is not a discovered Nest module", module.source, item, importedSource)
		}
		if className != item {
			return "", fmt.Errorf("Nest module %s imports %s from %s, which exports %s", module.source, item, importedSource, className)
		}
		return className, nil
	}

	call := dynamicModuleCallPattern.FindStringSubmatchIndex(item)
	if call == nil {
		return "", fmt.Errorf("Nest module %s imports metadata item %q; only bare identifiers are supported", module.source, item)
	}
	open := call[1] - 1
	end, err := balancedEnd(item, open, '(', ')')
	if err != nil || strings.TrimSpace(item[end+1:]) != "" {
		return "", fmt.Errorf("Nest module %s has unsupported dynamic import %q", module.source, item)
	}
	base := item[call[2]:call[3]]
	method := item[call[4]:call[5]]
	callee := base + "." + method
	wantSource := allowedDynamicModuleImports[module.source][callee]
	if wantSource == "" {
		return "", fmt.Errorf("Nest module %s has unapproved dynamic import %s", module.source, callee)
	}
	binding, exists := module.bindings[base]
	if !exists || !binding.isDirectNamed(base, wantSource) {
		return "", fmt.Errorf("Nest module %s dynamic import %s must use a direct named import from %s", module.source, callee, wantSource)
	}
	return "", nil
}

func parseStaticImports(source string) (map[string]importBinding, error) {
	code := maskTypeScriptNonCode(source)
	result := make(map[string]importBinding)
	for _, match := range importTokenPattern.FindAllStringIndex(code, -1) {
		remainder := code[match[1]:]
		semicolon := strings.IndexByte(remainder, ';')
		if semicolon < 0 {
			return nil, fmt.Errorf("unterminated static import at byte %d", match[0])
		}
		end := match[1] + semicolon + 1
		statement := source[match[0]:end]
		parsed := staticImportPattern.FindStringSubmatch(statement)
		if parsed == nil {
			// Dynamic import() and side-effect imports cannot bind audited symbols.
			continue
		}
		clause := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(parsed[1]), "type "))
		parts, err := splitTopLevel(clause, ',')
		if err != nil {
			return nil, fmt.Errorf("parse import from %s: %w", parsed[2], err)
		}
		for _, part := range parts {
			part = strings.TrimSpace(part)
			switch {
			case strings.HasPrefix(part, "{"):
				if !strings.HasSuffix(part, "}") {
					return nil, fmt.Errorf("unsupported named import %q", part)
				}
				members, err := splitTopLevel(part[1:len(part)-1], ',')
				if err != nil {
					return nil, err
				}
				for _, member := range members {
					fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(member), "type "))
					if len(fields) == 0 {
						continue
					}
					binding := importBinding{imported: fields[0], local: fields[0], source: parsed[2], kind: importNamed}
					if len(fields) == 3 && fields[1] == "as" {
						binding.local = fields[2]
					} else if len(fields) != 1 {
						return nil, fmt.Errorf("unsupported named import member %q", member)
					}
					if !bareIdentifier.MatchString(binding.imported) || !bareIdentifier.MatchString(binding.local) {
						return nil, fmt.Errorf("invalid named import member %q", member)
					}
					if err := addImportBinding(result, binding); err != nil {
						return nil, err
					}
				}
			case strings.HasPrefix(part, "*"):
				fields := strings.Fields(part)
				if len(fields) != 3 || fields[0] != "*" || fields[1] != "as" || !bareIdentifier.MatchString(fields[2]) {
					return nil, fmt.Errorf("unsupported namespace import %q", part)
				}
				if err := addImportBinding(result, importBinding{imported: "*", local: fields[2], source: parsed[2], kind: importNamespace}); err != nil {
					return nil, err
				}
			default:
				if !bareIdentifier.MatchString(part) {
					return nil, fmt.Errorf("unsupported default import %q", part)
				}
				if err := addImportBinding(result, importBinding{imported: "default", local: part, source: parsed[2], kind: importDefault}); err != nil {
					return nil, err
				}
			}
		}
	}
	return result, nil
}

func addImportBinding(bindings map[string]importBinding, binding importBinding) error {
	if previous, exists := bindings[binding.local]; exists {
		return fmt.Errorf("import name %s is bound by both %s and %s", binding.local, previous.source, binding.source)
	}
	bindings[binding.local] = binding
	return nil
}

func rejectAliasedNestDecoratorImports(sourcePath string, bindings map[string]importBinding) error {
	for _, binding := range bindings {
		if binding.source != "@nestjs/common" {
			continue
		}
		if binding.kind == importNamespace {
			return fmt.Errorf("%s uses an unsupported namespace import from @nestjs/common", sourcePath)
		}
		if _, supported := supportedNestDecoratorImports[binding.imported]; supported && binding.local != binding.imported {
			return fmt.Errorf("%s aliases Nest decorator %s as %s", sourcePath, binding.imported, binding.local)
		}
	}
	return nil
}

func verifyNestDecoratorImports(sourcePath, code string, bindings map[string]importBinding) error {
	for _, match := range decoratorTokenPattern.FindAllStringSubmatch(code, -1) {
		name := match[1]
		if _, allowed := allowedNestDecorators[name]; !allowed {
			return fmt.Errorf("%s uses unsupported Nest decorator @%s", sourcePath, name)
		}
		binding, exists := bindings[name]
		if !exists || !binding.isDirectNamed(name, "@nestjs/common") {
			return fmt.Errorf("%s decorator @%s must use a direct named import from @nestjs/common", sourcePath, name)
		}
	}
	return nil
}

func braceDepth(code string, start, end int) (int, error) {
	if start < 0 || end < start || end > len(code) {
		return 0, fmt.Errorf("invalid brace depth range %d..%d", start, end)
	}
	depth := 0
	for index := start; index < end; index++ {
		switch code[index] {
		case '{':
			depth++
		case '}':
			depth--
			if depth < 0 {
				return 0, fmt.Errorf("unbalanced closing brace at byte %d", index)
			}
		}
	}
	return depth, nil
}

func requireDirectNestDecoratorImport(sourcePath string, bindings map[string]importBinding, name string) error {
	binding, exists := bindings[name]
	if !exists || !binding.isDirectNamed(name, "@nestjs/common") {
		return fmt.Errorf("%s decorator @%s must use a direct named import from @nestjs/common", sourcePath, name)
	}
	return nil
}

func resolveRelativeTypeScriptImport(sourcePath, importSource string) (string, error) {
	if !strings.HasPrefix(importSource, ".") {
		return "", fmt.Errorf("import %q is not a relative source binding", importSource)
	}
	resolved := path.Clean(path.Join(path.Dir(sourcePath), importSource))
	if !strings.HasSuffix(resolved, ".ts") {
		resolved += ".ts"
	}
	if err := validateSourcePath(resolved); err != nil {
		return "", err
	}
	return resolved, nil
}

func parseObjectLiteral(code string) (map[string]string, error) {
	code = strings.TrimSpace(code)
	if len(code) < 2 || code[0] != '{' {
		return nil, fmt.Errorf("expected object literal")
	}
	end, err := balancedEnd(code, 0, '{', '}')
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(code[end+1:]) != "" {
		return nil, fmt.Errorf("unsupported object literal suffix %q", strings.TrimSpace(code[end+1:]))
	}
	entries, err := splitTopLevel(code[1:end], ',')
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		colon, err := topLevelDelimiter(entry, ':')
		if err != nil || colon < 0 {
			return nil, fmt.Errorf("object metadata entry %q is not a key/value property", entry)
		}
		key := strings.TrimSpace(entry[:colon])
		if !bareIdentifier.MatchString(key) {
			return nil, fmt.Errorf("object metadata key %q is not a bare identifier", key)
		}
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("object metadata property %s is repeated", key)
		}
		result[key] = strings.TrimSpace(entry[colon+1:])
	}
	return result, nil
}

func parseTopLevelArray(code string) ([]string, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, nil
	}
	if len(code) < 2 || code[0] != '[' {
		return nil, fmt.Errorf("expected array literal")
	}
	end, err := balancedEnd(code, 0, '[', ']')
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(code[end+1:]) != "" {
		return nil, fmt.Errorf("unsupported array literal suffix %q", strings.TrimSpace(code[end+1:]))
	}
	items, err := splitTopLevel(code[1:end], ',')
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(items))
	for index, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			if len(items) == 1 || index == len(items)-1 {
				continue
			}
			return nil, fmt.Errorf("array literal contains an empty item")
		}
		result = append(result, item)
	}
	return result, nil
}

func parseBareIdentifierArray(code string) ([]string, error) {
	items, err := parseTopLevelArray(code)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !bareIdentifier.MatchString(item) {
			return nil, fmt.Errorf("metadata item %q is not a bare identifier", item)
		}
		if _, exists := seen[item]; exists {
			return nil, fmt.Errorf("metadata identifier %s is repeated", item)
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result, nil
}

func splitTopLevel(code string, delimiter byte) ([]string, error) {
	var result []string
	start := 0
	var parentheses, brackets, braces int
	for index := 0; index < len(code); index++ {
		switch code[index] {
		case '(':
			parentheses++
		case ')':
			parentheses--
		case '[':
			brackets++
		case ']':
			brackets--
		case '{':
			braces++
		case '}':
			braces--
		case delimiter:
			if parentheses == 0 && brackets == 0 && braces == 0 {
				result = append(result, strings.TrimSpace(code[start:index]))
				start = index + 1
			}
		}
		if parentheses < 0 || brackets < 0 || braces < 0 {
			return nil, fmt.Errorf("unbalanced metadata expression at byte %d", index)
		}
	}
	if parentheses != 0 || brackets != 0 || braces != 0 {
		return nil, fmt.Errorf("unbalanced metadata expression")
	}
	result = append(result, strings.TrimSpace(code[start:]))
	return result, nil
}

func topLevelDelimiter(code string, delimiter byte) (int, error) {
	var parentheses, brackets, braces int
	found := -1
	for index := 0; index < len(code); index++ {
		switch code[index] {
		case '(':
			parentheses++
		case ')':
			parentheses--
		case '[':
			brackets++
		case ']':
			brackets--
		case '{':
			braces++
		case '}':
			braces--
		case delimiter:
			if parentheses == 0 && brackets == 0 && braces == 0 {
				if found >= 0 {
					return -1, nil
				}
				found = index
			}
		}
		if parentheses < 0 || brackets < 0 || braces < 0 {
			return -1, fmt.Errorf("unbalanced metadata expression at byte %d", index)
		}
	}
	if parentheses != 0 || brackets != 0 || braces != 0 {
		return -1, fmt.Errorf("unbalanced metadata expression")
	}
	return found, nil
}

func balancedEnd(code string, open int, opening, closing byte) (int, error) {
	if open < 0 || open >= len(code) || code[open] != opening {
		return 0, fmt.Errorf("expected %q at byte %d", opening, open)
	}
	depth := 0
	for index := open; index < len(code); index++ {
		switch code[index] {
		case opening:
			depth++
		case closing:
			depth--
			if depth == 0 {
				return index, nil
			}
		}
	}
	return 0, fmt.Errorf("unterminated %q opened at byte %d", opening, open)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
