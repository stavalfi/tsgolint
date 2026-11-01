package zod_schemas_file_only

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/checker"
	"github.com/typescript-eslint/tsgolint/internal/rule"
	"github.com/typescript-eslint/tsgolint/internal/utils"
)

const schemasFileName = "zod-schemas.ts"

type ZodSchemasFileOnlyOptions struct {
	// Globs (filepath.Match), relative to a tsconfig.json directory, naming
	// directories that count as their own package and so may hold their own
	// zod-schemas.ts — e.g. "src/commands/*" gives each command its own file.
	SubPackageDirs []string `json:"subPackageDirs"`
}

func buildWrongFileMessage(name string) rule.RuleMessage {
	return rule.RuleMessage{
		Id:          "schemaOutsideSchemasFile",
		Description: "Zod schema '" + name + "' is declared outside " + schemasFileName + ".",
		Help:        "Move it to the package's " + schemasFileName + " and import it, so every schema in a package has one home.",
	}
}

func buildDuplicateFileMessage(packageDir string, other string) rule.RuleMessage {
	return rule.RuleMessage{
		Id:          "duplicateSchemasFile",
		Description: "Package '" + packageDir + "' has more than one " + schemasFileName + "; also found at '" + other + "'.",
		Help:        "A package is the directory of its tsconfig.json (or a subPackageDirs match), and may hold exactly one " + schemasFileName + ". Merge them.",
	}
}

// A package is the nearest ancestor directory holding a tsconfig.json, unless a
// directory between the file and that tsconfig.json matches one of
// subPackageDirs — then the nearest such directory is the package. Returns ""
// when there is no tsconfig.json, which leaves the file unattributed and
// unchecked for duplicates.
func packageDirOf(fileName string, subPackageDirs []string) string {
	tsconfigDir := ""
	for dir := filepath.Dir(fileName); ; {
		if _, err := os.Stat(filepath.Join(dir, "tsconfig.json")); err == nil {
			tsconfigDir = dir
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
	for dir := filepath.Dir(fileName); dir != tsconfigDir; dir = filepath.Dir(dir) {
		relative, err := filepath.Rel(tsconfigDir, dir)
		if err != nil {
			break
		}
		for _, pattern := range subPackageDirs {
			if matched, _ := filepath.Match(pattern, filepath.ToSlash(relative)); matched {
				return dir
			}
		}
	}
	return tsconfigDir
}

// Tests are free to hold as many zod-schemas.ts as they like: a test's schemas
// describe that test's fixtures, not the package's contracts, so there is
// nothing to merge them into. Only the production tree is held to one file.
func isUnderTestDir(fileName string, packageDir string) bool {
	relative, err := filepath.Rel(packageDir, fileName)
	if err != nil {
		return false
	}
	for _, segment := range strings.Split(filepath.ToSlash(relative), "/") {
		if segment == "test" {
			return true
		}
	}
	return false
}

// zod's runtime classes are all named ZodSomething (ZodObject, ZodString, ...),
// so the declared type identifies a schema regardless of how it was built or
// what the binding is called.
func isZodSchemaType(typeChecker *checker.Checker, node *ast.Node) bool {
	t := utils.GetConstrainedTypeAtLocation(typeChecker, node)
	return utils.SomeUnionTypePart(t, func(part *checker.Type) bool {
		symbol := checker.Type_symbol(part)
		return symbol != nil && strings.HasPrefix(symbol.Name, "Zod")
	})
}

// The module an identifier was imported from, or "" when it is not an import
// binding.
func importedFrom(typeChecker *checker.Checker, identifier *ast.Node) string {
	symbol := typeChecker.GetSymbolAtLocation(identifier)
	if symbol == nil || symbol.Flags&ast.SymbolFlagsAlias == 0 {
		return ""
	}
	for _, declaration := range symbol.Declarations {
		for node := declaration; node != nil; node = node.Parent {
			if !ast.IsImportDeclaration(node) {
				continue
			}
			if specifier := node.AsImportDeclaration().ModuleSpecifier; ast.IsStringLiteral(specifier) {
				return specifier.Text()
			}
			return ""
		}
	}
	return ""
}

func isZodModule(specifier string) bool {
	return specifier == "zod" || strings.HasPrefix(specifier, "zod/")
}

// `./zod-schemas`, `../zod-schemas.ts`, `#/zod-schemas.js`: a builder that lives
// where schemas belong.
func isSchemasModule(specifier string) bool {
	base := path.Base(specifier)
	return strings.TrimSuffix(strings.TrimSuffix(base, ".ts"), ".js") == strings.TrimSuffix(schemasFileName, ".ts")
}

// Walks a call/property chain back to the identifier it started from:
// `z.compile(z.object())` roots at `z`, `ZodSchemas.foo(...)` at `ZodSchemas`.
func rootIdentifier(node *ast.Node) *ast.Node {
	current := node
	for {
		switch {
		case ast.IsCallExpression(current):
			current = current.AsCallExpression().Expression
		case ast.IsPropertyAccessExpression(current):
			current = current.AsPropertyAccessExpression().Expression
		case ast.IsParenthesizedExpression(current):
			current = current.AsParenthesizedExpression().Expression
		case ast.IsIdentifier(current):
			return current
		default:
			return nil
		}
	}
}

// A call that builds a schema in this file: a chain rooted at the zod import
// (`z.object(...)`, `z.compile(...)`), or a method on a schema
// (`cursorSchema.optional()`). Calling a builder imported from a zod-schemas.ts
// only references a schema built there.
func buildsSchema(typeChecker *checker.Checker, call *ast.Node) bool {
	callee := call.AsCallExpression().Expression
	if root := rootIdentifier(callee); root != nil && isZodModule(importedFrom(typeChecker, root)) {
		return true
	}
	if !ast.IsPropertyAccessExpression(callee) {
		return false
	}
	receiver := callee.AsPropertyAccessExpression().Expression
	// The builder class itself (`typeof ZodSchemas`) reads as Zod-named.
	if ast.IsIdentifier(receiver) && isSchemasModule(importedFrom(typeChecker, receiver)) {
		return false
	}
	return isZodSchemaType(typeChecker, receiver)
}

// Whether the expression constructs a schema here, rather than holding one
// passed in (`this.#schema = checkpointSchema`) or built in a zod-schemas.ts.
func constructsSchema(typeChecker *checker.Checker, expression *ast.Node) bool {
	var visit func(node *ast.Node) bool
	visit = func(node *ast.Node) bool {
		if ast.IsCallExpression(node) && buildsSchema(typeChecker, node) {
			return true
		}
		return node.ForEachChild(visit)
	}
	return visit(expression)
}

// A class field reported where it is declared: `static FOO = z.object()`. A
// field with no initialiser is decided by the assignments to it.
// `declare schema: ZodString` — or any field of a `declare class` — describes a
// schema that lives elsewhere, so it is not one.
func isReportedPropertyDeclaration(typeChecker *checker.Checker, node *ast.Node) bool {
	if !ast.IsPropertyDeclaration(node) {
		return false
	}
	if node.Flags&ast.NodeFlagsAmbient != 0 || ast.HasSyntacticModifier(node, ast.ModifierFlagsAmbient) {
		return false
	}
	if filepath.Base(ast.GetSourceFileOfNode(node).FileName()) == schemasFileName {
		return false
	}
	declaration := node.AsPropertyDeclaration()
	name := declaration.Name()
	if name == nil || !(ast.IsIdentifier(name) || ast.IsPrivateIdentifier(name)) {
		return false
	}
	if declaration.Initializer == nil || !constructsSchema(typeChecker, declaration.Initializer) {
		return false
	}
	return isZodSchemaType(typeChecker, name)
}

var ZodSchemasFileOnlyRule = rule.Rule{
	Name: "zod-schemas-file-only",
	Run: func(ctx rule.RuleContext, options any) rule.RuleListeners {
		opts := utils.UnmarshalOptions[ZodSchemasFileOnlyOptions](options, "zod-schemas-file-only")
		fileName := ctx.SourceFile.FileName()
		isSchemasFile := filepath.Base(fileName) == schemasFileName

		listeners := rule.RuleListeners{}

		if !isSchemasFile {
			listeners[ast.KindVariableDeclaration] = func(node *ast.Node) {
				// `declare const schema: ZodString` introduces no schema — it
				// describes one that lives elsewhere, same as an import.
				if node.Flags&ast.NodeFlagsAmbient != 0 {
					return
				}

				declaration := node.AsVariableDeclaration()
				name := declaration.Name()
				if name == nil || !ast.IsIdentifier(name) {
					return
				}
				// Check the binding, not the initialiser: a schema assigned from
				// a helper or a function call is still a schema living here.
				if !isZodSchemaType(ctx.TypeChecker, name) {
					return
				}
				ctx.ReportNode(name, buildWrongFileMessage(name.Text()))
			}

			listeners[ast.KindPropertyDeclaration] = func(node *ast.Node) {
				if !isReportedPropertyDeclaration(ctx.TypeChecker, node) {
					return
				}
				name := node.AsPropertyDeclaration().Name()
				ctx.ReportNode(name, buildWrongFileMessage(name.Text()))
			}

			// `this.#schema = z.compile(...)` in a constructor or method builds the
			// schema here. `this.#schema = checkpointSchema` only holds one.
			listeners[ast.KindBinaryExpression] = func(node *ast.Node) {
				if !ast.IsAssignmentExpression(node, true) {
					return
				}
				binary := node.AsBinaryExpression()
				if !ast.IsPropertyAccessExpression(binary.Left) {
					return
				}
				access := binary.Left.AsPropertyAccessExpression()
				if access.Expression.Kind != ast.KindThisKeyword {
					return
				}
				name := access.Name()
				if !isZodSchemaType(ctx.TypeChecker, binary.Right) || !constructsSchema(ctx.TypeChecker, binary.Right) {
					return
				}
				// The field's declaration already carries the report.
				if symbol := ctx.TypeChecker.GetSymbolAtLocation(name); symbol != nil {
					for _, declaration := range symbol.Declarations {
						if isReportedPropertyDeclaration(ctx.TypeChecker, declaration) {
							return
						}
					}
				}
				ctx.ReportNode(name, buildWrongFileMessage(name.Text()))
			}
			return listeners
		}

		// This file is a zod-schemas.ts. Report once, on the file, if its package
		// holds another one. Every zod-schemas.ts in the package reports, so the
		// diagnostic is visible whichever file you happen to be linting.
		listeners[ast.KindSourceFile] = func(node *ast.Node) {
			packageDir := packageDirOf(fileName, opts.SubPackageDirs)
			if packageDir == "" || isUnderTestDir(fileName, packageDir) {
				return
			}

			siblings := []string{}
			for _, sourceFile := range ctx.Program.SourceFiles() {
				other := sourceFile.FileName()
				if other == fileName || filepath.Base(other) != schemasFileName {
					continue
				}
				if packageDirOf(other, opts.SubPackageDirs) == packageDir && !isUnderTestDir(other, packageDir) {
					siblings = append(siblings, other)
				}
			}
			if len(siblings) == 0 {
				return
			}
			sort.Strings(siblings)

			ctx.ReportRange(
				ctx.SourceFile.Loc.WithEnd(ctx.SourceFile.Loc.Pos()),
				buildDuplicateFileMessage(packageDir, siblings[0]),
			)
		}

		return listeners
	},
}
