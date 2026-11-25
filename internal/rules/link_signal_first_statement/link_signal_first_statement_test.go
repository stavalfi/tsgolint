package link_signal_first_statement

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

func TestLinkSignalFirstStatement(t *testing.T) {
	t.Parallel()
	rule_tester.RunRuleTester(fixtures.GetRootDir(), "tsconfig.json", t, &LinkSignalFirstStatementRule, []rule_tester.ValidTestCase{
		{Code: stub + `
class A extends Base {
  async run({ signal: callerSignal }: { signal: AbortSignal }): Promise<void> {
    const signal = super.linkSignal({ signal: callerSignal });
    await fetch("x", { signal });
  }
}`},
		{Code: stub + `
class A extends Base {
  async run(options: { signal: AbortSignal }): Promise<void> {
    const signal = super.linkSignal({ signal: options.signal });
    await fetch("x", { signal });
  }
}`},
		{Code: stub + `
class A extends Base {
  async #run({ activityAbortSignal }: { activityAbortSignal: AbortSignal }): Promise<void> {
    const signal = super.linkSignal({ signal: activityAbortSignal });
    await fetch("x", { signal });
  }
}`},
		// Not async: the rule leaves sync methods to require-link-signal.
		{Code: stub + `
class A extends Base {
  run({ signal }: { signal: AbortSignal }): void {
    fetch("x", { signal: super.linkSignal({ signal }) });
  }
}`},
		// No caller signal to link.
		{Code: stub + `
class A extends Base {
  async run(): Promise<void> {}
}`},
		// No `linkSignal` to reach.
		{Code: stub + `
class B {}
class A extends B {
  async run({ signal }: { signal: AbortSignal }): Promise<void> { await fetch("x", { signal }); }
}`},
		// A static method has no `super`.
		{Code: stub + `
class A extends Base {
  static async run({ signal }: { signal: AbortSignal }): Promise<void> { await fetch("x", { signal }); }
}`},
		// Linking something that is not the caller's signal is a different statement.
		{Code: stub + `
declare function current(): AbortSignal;
class A extends Base {
  async run({ signal: callerSignal }: { signal: AbortSignal }): Promise<void> {
    const signal = super.linkSignal({ signal: callerSignal });
    const other = super.linkSignal({ signal: current() });
    await fetch("x", { signal: other });
  }
}`},
	}, []rule_tester.InvalidTestCase{
		// No link at all: the binding is renamed so `const signal` can take its name.
		{
			Code: stub + `
class A extends Base {
  async run({ signal }: { signal: AbortSignal }): Promise<void> {
    await fetch("x", { signal });
  }
}`,
			Output: []string{stub + `
class A extends Base {
  async run({ signal: callerSignal }: { signal: AbortSignal }): Promise<void> {
    const signal = super.linkSignal({ signal: callerSignal });
    await fetch("x", { signal });
  }
}`},
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "linkSignalFirst", Line: 10, Column: 9, EndLine: 10, EndColumn: 12},
			},
		},
		// Linked inline: hoisted into the first statement, the inline call becomes `signal`.
		{
			Code: stub + `
class A extends Base {
  async run({ signal }: { signal: AbortSignal }): Promise<void> {
    await fetch("x", { signal: super.linkSignal({ signal }) });
  }
}`,
			Output: []string{stub + `
class A extends Base {
  async run({ signal: callerSignal }: { signal: AbortSignal }): Promise<void> {
    const signal = super.linkSignal({ signal: callerSignal });
    await fetch("x", { signal });
  }
}`},
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "linkSignalFirst", Line: 10, Column: 9, EndLine: 10, EndColumn: 12},
				{MessageId: "linkSignalTwice", Line: 11, Column: 32, EndLine: 11, EndColumn: 60},
			},
		},
		// Linked under another name: renamed to `signal`, along with every read.
		{
			Code: stub + `
class A extends Base {
  async run({ signal }: { signal: AbortSignal }): Promise<void> {
    const anySignal = super.linkSignal({ signal });
    await fetch("x", { signal: anySignal });
    await fetch("y", { signal: anySignal });
  }
}`,
			Output: []string{stub + `
class A extends Base {
  async run({ signal: callerSignal }: { signal: AbortSignal }): Promise<void> {
    const signal = super.linkSignal({ signal: callerSignal });
    await fetch("x", { signal });
    await fetch("y", { signal });
  }
}`},
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "linkSignalFirst", Line: 11, Column: 5, EndLine: 11, EndColumn: 52},
			},
		},
		// Linked, but not first: moved up.
		{
			Code: stub + `
class A extends Base {
  async run(options: { signal: AbortSignal }): Promise<void> {
    const url = "x";
    const signal = super.linkSignal({ signal: options.signal });
    await fetch(url, { signal });
  }
}`,
			Output: []string{stub + `
class A extends Base {
  async run(options: { signal: AbortSignal }): Promise<void> {
    const signal = super.linkSignal({ signal: options.signal });
    const url = "x";
    await fetch(url, { signal });
  }
}`},
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "linkSignalFirst", Line: 12, Column: 5, EndLine: 12, EndColumn: 65},
			},
		},
		// A renamed binding and a shorthand read of the old name.
		{
			Code: stub + `
declare function use(options: { linked: AbortSignal }): Promise<void>;
class A extends Base {
  async run({ signal: abort }: { signal: AbortSignal }): Promise<void> {
    const linked = super.linkSignal({ signal: abort });
    await use({ linked });
  }
}`,
			Output: []string{stub + `
declare function use(options: { linked: AbortSignal }): Promise<void>;
class A extends Base {
  async run({ signal: abort }: { signal: AbortSignal }): Promise<void> {
    const signal = super.linkSignal({ signal: abort });
    await use({ linked: signal });
  }
}`},
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "linkSignalFirst", Line: 12, Column: 5, EndLine: 12, EndColumn: 56},
			},
		},
		// A private method links too.
		{
			Code: stub + `
class A extends Base {
  async #run(options: { signal: AbortSignal }): Promise<void> {
    await fetch("x", { signal: options.signal });
  }
}`,
			Output: []string{stub + `
class A extends Base {
  async #run(options: { signal: AbortSignal }): Promise<void> {
    const signal = super.linkSignal({ signal: options.signal });
    await fetch("x", { signal });
  }
}`},
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "linkSignalFirst", Line: 10, Column: 9, EndLine: 10, EndColumn: 13},
			},
		},
		// `const signal = options.signal` is the raw signal under the linked name: replaced in place.
		{
			Code: stub + `
class A extends Base {
  async run(options: { signal: AbortSignal }): Promise<void> {
    const url = "x";
    const signal = options.signal;
    await fetch(url, { signal });
  }
}`,
			Output: []string{stub + `
class A extends Base {
  async run(options: { signal: AbortSignal }): Promise<void> {
    const signal = super.linkSignal({ signal: options.signal });
    const url = "x";
    await fetch(url, { signal });
  }
}`},
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "linkSignalFirst", Line: 12, Column: 5, EndLine: 12, EndColumn: 35},
			},
		},
		// Raw reads inside nested arrows follow, unless a nested parameter named `signal` shadows them.
		{
			Code: stub + `
declare function each(run: (options: { signal: AbortSignal }) => Promise<unknown>): Promise<void>;
class A extends Base {
  async run({ signal, url }: { signal: AbortSignal; url: string }): Promise<void> {
    await each(() => fetch(url, { signal }));
    await each(({ signal }) => fetch(url, { signal }));
    await each(({ signal: inner }) => fetch(url, { signal: inner }));
  }
}`,
			Output: []string{stub + `
declare function each(run: (options: { signal: AbortSignal }) => Promise<unknown>): Promise<void>;
class A extends Base {
  async run({ signal: callerSignal, url }: { signal: AbortSignal; url: string }): Promise<void> {
    const signal = super.linkSignal({ signal: callerSignal });
    await each(() => fetch(url, { signal }));
    await each(({ signal }) => fetch(url, { signal }));
    await each(({ signal: inner }) => fetch(url, { signal: inner }));
  }
}`},
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "linkSignalFirst", Line: 11, Column: 9, EndLine: 11, EndColumn: 12},
			},
		},
		// An object parameter forwarded whole carries the raw signal: it is spread with the linked one.
		{
			Code: stub + `
class A extends Base {
  async run(options: { signal: AbortSignal; url: string }): Promise<void> {
    await this.inner(options);
    this.log(options.url);
  }
  async inner(options: { signal: AbortSignal; url: string }): Promise<void> {
    const signal = super.linkSignal({ signal: options.signal });
    await fetch(options.url, { signal });
  }
  log(url: string): void {}
}`,
			Output: []string{stub + `
class A extends Base {
  async run(options: { signal: AbortSignal; url: string }): Promise<void> {
    const signal = super.linkSignal({ signal: options.signal });
    await this.inner({ ...options, signal });
    this.log(options.url);
  }
  async inner(options: { signal: AbortSignal; url: string }): Promise<void> {
    const signal = super.linkSignal({ signal: options.signal });
    await fetch(options.url, { signal });
  }
  log(url: string): void {}
}`},
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "linkSignalFirst", Line: 10, Column: 9, EndLine: 10, EndColumn: 12},
			},
		},
		// Another `signal` is declared in the body: reported, not fixed.
		{
			Code: stub + `
declare function make(): AbortSignal;
class A extends Base {
  async run({ signal: callerSignal }: { signal: AbortSignal }): Promise<void> {
    const signal = make();
    await fetch("x", { signal });
  }
}`,
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "linkSignalFirst", Line: 11, Column: 9, EndLine: 11, EndColumn: 12},
			},
		},
		// Linking the caller's signal again below the first statement.
		{
			Code: stub + `
class A extends Base {
  async run({ signal: callerSignal }: { signal: AbortSignal }): Promise<void> {
    const signal = super.linkSignal({ signal: callerSignal });
    await fetch("x", { signal: super.linkSignal({ signal: callerSignal }) });
    await fetch("y", { signal: super.linkSignal({ signal }) });
  }
}`,
			Output: []string{stub + `
class A extends Base {
  async run({ signal: callerSignal }: { signal: AbortSignal }): Promise<void> {
    const signal = super.linkSignal({ signal: callerSignal });
    await fetch("x", { signal });
    await fetch("y", { signal });
  }
}`},
			Errors: []rule_tester.InvalidTestCaseError{
				{MessageId: "linkSignalTwice", Line: 12, Column: 32, EndLine: 12, EndColumn: 74},
				{MessageId: "linkSignalTwice", Line: 13, Column: 32, EndLine: 13, EndColumn: 60},
			},
		},
	})
}
