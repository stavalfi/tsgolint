package zod_schemas_file_only

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/typescript-eslint/tsgolint/internal/rule_tester"
	"github.com/typescript-eslint/tsgolint/internal/rules/fixtures"
)

// zod's schema classes are named ZodSomething, which is what the rule keys off.
const zodStub = `
declare class ZodString {
  optional(): ZodString;
}
declare class ZodObject {}
declare const z: { string(): ZodString; object(): ZodObject };
`

// The rule tester lints a file that is not named zod-schemas.ts, so these cover
// the "declared in the wrong file" half. The duplicate-file half needs several
// real files in one program and is covered by the poc integration test.
func TestZodSchemasFileOnly(t *testing.T) {
	t.Parallel()
	rule_tester.RunRuleTester(fixtures.GetRootDir(), "tsconfig.json", t, &ZodSchemasFileOnlyRule, []rule_tester.ValidTestCase{
		{Code: `
const port = 8080;
    `},
		// Importing a schema is the whole point — only declaring one here is banned.
		{Code: zodStub + `
declare const imported: ZodString;
const used = imported.optional() ? 1 : 2;
    `},
	}, []rule_tester.InvalidTestCase{
		{
			Code: zodStub + `const schema = z.string();`,
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "schemaOutsideSchemasFile", Line: 7, Column: 7, EndLine: 7, EndColumn: 13},
			},
		},
		{
			Code: zodStub + `export const body = z.object();`,
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "schemaOutsideSchemasFile", Line: 7, Column: 14, EndLine: 7, EndColumn: 18},
			},
		},
		// Built through a helper rather than a `z.` chain — a syntactic rule keyed
		// on `z.` would miss this; the declared type still says ZodString.
		{
			Code: zodStub + `
function make(): ZodString { return z.string(); }
const viaHelper = make();
      `,
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "schemaOutsideSchemasFile", Line: 9, Column: 7, EndLine: 9, EndColumn: 16},
			},
		},
	})
}

// subPackageDirs makes a matching directory its own package, so a command folder
// can hold its own zod-schemas.ts next to the package-level one.
func TestPackageDirOfSubPackageDirs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	command := filepath.Join(root, "src", "commands", "start-backends")
	cases := []struct {
		file           string
		subPackageDirs []string
		want           string
	}{
		{filepath.Join(root, "src", "zod-schemas.ts"), []string{"src/commands/*"}, root},
		{filepath.Join(command, "zod-schemas.ts"), nil, root},
		{filepath.Join(command, "zod-schemas.ts"), []string{"src/commands/*"}, command},
		{filepath.Join(command, "nested", "zod-schemas.ts"), []string{"src/commands/*"}, command},
		{filepath.Join(command, "zod-schemas.ts"), []string{"src/other/*"}, root},
	}
	for _, c := range cases {
		if got := packageDirOf(c.file, c.subPackageDirs); got != c.want {
			t.Errorf("packageDirOf(%q, %v) = %q, want %q", c.file, c.subPackageDirs, got, c.want)
		}
	}
}

// A stand-in "zod" module: the class-field half keys off calls rooted at the
// zod import, so the schemas must come from a real import, not a global.
const zodModule = `
declare module "zod" {
  export class ZodType { optional(): ZodType }
  export class ZodObject extends ZodType {}
  export const z: { object(): ZodObject; compile<T>(schema: T): T };
}
`

// The package's zod-schemas.ts, holding a builder the classes may call.
const schemasModule = `import { z, type ZodType } from "zod";
export class ZodSchemas {
  static checkpoint({ cursorSchema }: { cursorSchema: ZodType }): ZodType {
    return z.compile(z.object()).optional() ?? cursorSchema;
  }
}
`

// Every file the class-field cases compile with. The fixtures tsconfig globs the
// directory and the base VFS is cached across cases, so each case declares all
// of them, whichever one it lints.
func otherFiles(fileName string) map[string]string {
	files := map[string]string{
		"zod-stub.d.ts":  zodModule,
		"file.ts":        "export {};",
		"zod-schemas.ts": schemasModule,
	}
	delete(files, fileName)
	return files
}

const zodImport = `import { z, type ZodType } from "zod";
`

const fieldsInitialised = `class A { readonly #schema = z.compile(z.object()); static FOO = z.object(); }
export const a = new A();
`

const compiledInConstructor = `class B {
  readonly #schema: ZodType;
  constructor() {
    this.#schema = z.compile(z.object());
  }
  get schema(): ZodType { return this.#schema; }
}
export const b = new B();
`

func TestZodSchemasFileOnlyClassFields(t *testing.T) {
	t.Parallel()
	rule_tester.RunRuleTester(fixtures.GetRootDir(), "tsconfig.json", t, &ZodSchemasFileOnlyRule, []rule_tester.ValidTestCase{
		{
			FileName: "file.ts",
			Files:    otherFiles("file.ts"),
			Code: `
class D {
  readonly port: number = 8080;
  name = "x";
  constructor() { this.name = "y"; }
}
export const d = new D();`,
		},
		// `declare` fields describe a schema that lives elsewhere.
		{
			FileName: "file.ts",
			Files:    otherFiles("file.ts"),
			Code: zodImport + `
declare class E { schema: ZodType }
class F { declare schema: ZodType }
export const f = new F();`,
		},
		// Holding a schema passed in builds nothing here; the field declaration
		// with no initialiser is not reported by itself.
		{
			FileName: "file.ts",
			Files:    otherFiles("file.ts"),
			Code: zodImport + `class P {
  readonly #schema: ZodType;
  constructor(checkpointSchema: ZodType) {
    this.#schema = checkpointSchema;
  }
  get schema(): ZodType { return this.#schema; }
}
export const p = new P(z.object());`,
		},
		// The fix the rule asks for: the builder lives in zod-schemas.ts.
		{
			FileName: "file.ts",
			Files:    otherFiles("file.ts"),
			Code: `import type { ZodType } from "zod";
import { ZodSchemas } from "./zod-schemas";
class Q {
  readonly #schema: ZodType;
  readonly #other = ZodSchemas.checkpoint({ cursorSchema: ZodSchemas.checkpoint({ cursorSchema: undefined! }) });
  constructor(cursorSchema: ZodType) {
    this.#schema = ZodSchemas.checkpoint({ cursorSchema });
  }
  get schema(): ZodType { return this.#schema ?? this.#other; }
}
export const q = new Q(undefined!);`,
		},
		{
			FileName: "zod-schemas.ts",
			Files:    otherFiles("zod-schemas.ts"),
			Code:     zodImport + fieldsInitialised + compiledInConstructor,
		},
	}, []rule_tester.InvalidTestCase{
		{
			FileName: "file.ts",
			Files:    otherFiles("file.ts"),
			Code:     zodImport + fieldsInitialised,
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "schemaOutsideSchemasFile", Line: 2, Column: 20, EndLine: 2, EndColumn: 27},
				{MessageId: "schemaOutsideSchemasFile", Line: 2, Column: 60, EndLine: 2, EndColumn: 63},
			},
		},
		// The field is declared bare, so the assignment is where the schema is built.
		{
			FileName: "file.ts",
			Files:    otherFiles("file.ts"),
			Code:     zodImport + compiledInConstructor,
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "schemaOutsideSchemasFile", Line: 5, Column: 10, EndLine: 5, EndColumn: 17},
			},
		},
		// A method on a schema builds a new one.
		{
			FileName: "file.ts",
			Files:    otherFiles("file.ts"),
			Code: zodImport + `class O {
  readonly #schema: ZodType;
  constructor(cursorSchema: ZodType) {
    this.#schema = cursorSchema.optional();
  }
  get schema(): ZodType { return this.#schema; }
}
export const o = new O(z.object());`,
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "schemaOutsideSchemasFile", Line: 5, Column: 10, EndLine: 5, EndColumn: 17},
			},
		},
		{
			FileName: "file.ts",
			Files:    otherFiles("file.ts"),
			Code: zodImport + `class C {
  field: unknown;
  init(): void {
    this.field = z.object();
  }
}
export const c = new C();`,
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "schemaOutsideSchemasFile", Line: 5, Column: 10, EndLine: 5, EndColumn: 15},
			},
		},
	})
}
