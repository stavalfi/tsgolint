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

// zod.compile returns the schema it is given, so the field's type is still Zod.
const classStub = `
declare class ZodType {}
declare class ZodObject extends ZodType {}
declare const z: { object(): ZodObject; compile<T>(schema: T): T };
`

// Every file name the class-field cases compile under. The fixtures tsconfig
// globs the directory and the base VFS is cached across cases, so each case
// declares the other's name too.
var classFieldFileNames = []string{"file.ts", "zod-schemas.ts"}

func otherFiles(fileName string) map[string]string {
	files := map[string]string{}
	for _, other := range classFieldFileNames {
		if other != fileName {
			files[other] = "export {};"
		}
	}
	return files
}

const fieldsInitialised = `class A { readonly #schema: ZodType = z.compile(z.object()); static FOO = z.object(); }
export const a = new A();`

const fieldAssignedInConstructor = `
class B {
  readonly #schema: unknown;
  constructor() {
    this.#schema = z.compile(z.object());
  }
  get schema(): unknown { return this.#schema; }
}
export const b = new B();`

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
			Code: classStub + `
declare class E { schema: ZodType }
class F { declare schema: ZodType }
export const f = new F();`,
		},
		{
			FileName: "zod-schemas.ts",
			Files:    otherFiles("zod-schemas.ts"),
			Code:     classStub + fieldsInitialised + fieldAssignedInConstructor,
		},
	}, []rule_tester.InvalidTestCase{
		{
			FileName: "file.ts",
			Files:    otherFiles("file.ts"),
			Code:     classStub + fieldsInitialised,
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "schemaOutsideSchemasFile", Line: 5, Column: 20, EndLine: 5, EndColumn: 27},
				{MessageId: "schemaOutsideSchemasFile", Line: 5, Column: 69, EndLine: 5, EndColumn: 72},
			},
		},
		// The field is typed loosely, so the assignment is where the schema is built.
		{
			FileName: "file.ts",
			Files:    otherFiles("file.ts"),
			Code:     classStub + fieldAssignedInConstructor,
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "schemaOutsideSchemasFile", Line: 9, Column: 10, EndLine: 9, EndColumn: 17},
			},
		},
		{
			FileName: "file.ts",
			Files:    otherFiles("file.ts"),
			Code: classStub + `
class C {
  field: unknown;
  init(): void {
    this.field = z.object();
  }
}
export const c = new C();`,
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "schemaOutsideSchemasFile", Line: 9, Column: 10, EndLine: 9, EndColumn: 15},
			},
		},
		// A field typed as a schema is reported once, on its declaration — not
		// again where the constructor assigns it.
		{
			FileName: "file.ts",
			Files:    otherFiles("file.ts"),
			Code: classStub + `
class G {
  readonly #schema: ZodType;
  constructor() {
    this.#schema = z.compile(z.object());
  }
  get schema(): ZodType { return this.#schema; }
}
export const g = new G();`,
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "schemaOutsideSchemasFile", Line: 7, Column: 12, EndLine: 7, EndColumn: 19},
			},
		},
	})
}
