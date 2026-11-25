package require_link_signal

import (
	"testing"

	"github.com/typescript-eslint/tsgolint/internal/rule_tester"
	"github.com/typescript-eslint/tsgolint/internal/rules/fixtures"
)

// The fixtures project sets `"types": []`, so the base class is declared here.
const stub = `
interface AbortSignal { aborted: boolean }
interface Promise<T> {}
declare function fetch(url: string, init?: { signal?: AbortSignal }): Promise<unknown>;
declare abstract class Base {
  protected linkSignal(options: { signal: AbortSignal }): AbortSignal;
}
`

func TestRequireLinkSignal(t *testing.T) {
	t.Parallel()
	rule_tester.RunRuleTester(fixtures.GetRootDir(), "tsconfig.json", t, &RequireLinkSignalRule, []rule_tester.ValidTestCase{
		{Code: stub + `
class A extends Base {
  run({ signal }: { signal: AbortSignal }): void {
    const linked = super.linkSignal({ signal });
    fetch("x", { signal: linked });
  }
}`},
		{Code: stub + `
class A extends Base {
  run(options: { signal: AbortSignal }): void {
    fetch("x", { signal: super.linkSignal({ signal: options.signal }) });
  }
}`},
		// No `linkSignal` to reach: the class does not inherit it.
		{Code: stub + `
class B {}
class A extends B {
  run({ signal }: { signal: AbortSignal }): void { fetch("x", { signal }); }
}`},
		{Code: stub + `
function run({ signal }: { signal: AbortSignal }): void { fetch("x", { signal }); }
`},
		// An async method reads the linked `signal` its first statement declared.
		{Code: stub + `
class A extends Base {
  async run({ signal: callerSignal }: { signal: AbortSignal }): Promise<void> {
    const signal = super.linkSignal({ signal: callerSignal });
    await fetch("x", { signal });
  }
}`},
	}, []rule_tester.InvalidTestCase{
		{
			Code: stub + `
class A extends Base {
  run({ signal }: { signal: AbortSignal }): void { fetch("x", { signal }); }
}`,
			Output: []string{stub + `
class A extends Base {
  run({ signal }: { signal: AbortSignal }): void { fetch("x", { signal: super.linkSignal({ signal }) }); }
}`},
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "requireLinkSignal", Line: 10, Column: 65, EndLine: 10, EndColumn: 71},
			},
		},
		{
			Code: stub + `
class A extends Base {
  run({ signal: renamed }: { signal: AbortSignal }): void { fetch("x", { signal: renamed }); }
}`,
			Output: []string{stub + `
class A extends Base {
  run({ signal: renamed }: { signal: AbortSignal }): void { fetch("x", { signal: super.linkSignal({ signal: renamed }) }); }
}`},
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "requireLinkSignal", Line: 10, Column: 82, EndLine: 10, EndColumn: 89},
			},
		},
		{
			Code: stub + `
class A extends Base {
  run(abort: AbortSignal): void { const alias = abort; fetch("x", { signal: alias }); }
}`,
			Output: []string{stub + `
class A extends Base {
  run(abort: AbortSignal): void { const alias = super.linkSignal({ signal: abort }); fetch("x", { signal: alias }); }
}`},
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "requireLinkSignal", Line: 10, Column: 49, EndLine: 10, EndColumn: 54},
			},
		},
		{
			Code: stub + `
class A extends Base {
  run(options: { signal: AbortSignal }): void { fetch("x", { signal: options.signal }); }
}`,
			Output: []string{stub + `
class A extends Base {
  run(options: { signal: AbortSignal }): void { fetch("x", { signal: super.linkSignal({ signal: options.signal }) }); }
}`},
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "requireLinkSignal", Line: 10, Column: 70, EndLine: 10, EndColumn: 84},
			},
		},

		// A nested `function` has no `super`: reported, not fixed.
		{
			Code: stub + `
class A extends Base {
  run(abort: AbortSignal): void { function inner() { fetch("x", { signal: abort }); } inner(); }
}`,
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "requireLinkSignal", Line: 10, Column: 75, EndLine: 10, EndColumn: 80},
			},
		},

		// Private methods link too.
		{
			Code: stub + `
class A extends Base {
  #run({ signal }: { signal: AbortSignal }): void { fetch("x", { signal }); }
}`,
			Output: []string{stub + `
class A extends Base {
  #run({ signal }: { signal: AbortSignal }): void { fetch("x", { signal: super.linkSignal({ signal }) }); }
}`},
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "requireLinkSignal", Line: 10, Column: 66, EndLine: 10, EndColumn: 72},
			},
		},

		// In an async method the linked `signal` of the first statement is what a read becomes.
		{
			Code: stub + `
declare function use(options: { callerSignal: AbortSignal }): Promise<void>;
class A extends Base {
  async run({ signal: callerSignal }: { signal: AbortSignal }): Promise<void> {
    const signal = super.linkSignal({ signal: callerSignal });
    await fetch("x", { signal: callerSignal });
    await use({ callerSignal });
  }
}`,
			Output: []string{stub + `
declare function use(options: { callerSignal: AbortSignal }): Promise<void>;
class A extends Base {
  async run({ signal: callerSignal }: { signal: AbortSignal }): Promise<void> {
    const signal = super.linkSignal({ signal: callerSignal });
    await fetch("x", { signal });
    await use({ callerSignal: signal });
  }
}`},
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "requireLinkSignal", Line: 13, Column: 32, EndLine: 13, EndColumn: 44},
				{MessageId: "requireLinkSignal", Line: 14, Column: 17, EndLine: 14, EndColumn: 29},
			},
		},
		// Until that first statement exists there is nothing to rewrite to: reported, not fixed.
		{
			Code: stub + `
class A extends Base {
  async run(options: { signal: AbortSignal }): Promise<void> {
    await fetch("x", { signal: options.signal });
  }
}`,
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "requireLinkSignal", Line: 11, Column: 32, EndLine: 11, EndColumn: 46},
			},
		},
	})
}
