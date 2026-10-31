package no_export_rename

import (
	"testing"

	"github.com/typescript-eslint/tsgolint/internal/rule_tester"
	"github.com/typescript-eslint/tsgolint/internal/rules/fixtures"
)

func TestNoExportRename(t *testing.T) {
	t.Parallel()
	rule_tester.RunRuleTester(fixtures.GetRootDir(), "tsconfig.json", t, &NoExportRenameRule, []rule_tester.ValidTestCase{
		{Code: "const a = 1; export { a };"},
		{Code: "export const a = 1;"},
		{Code: "import { a as b } from './x'; export { b };"},
	}, []rule_tester.InvalidTestCase{
		{
			Code:   "const a = 1; export { a as b };",
			Errors: []rule_tester.InvalidTestCaseError{{MessageId: "exportRename", Line: 1, Column: 23, EndColumn: 29}},
		},
		{
			Code:   "export { a, b as c, type D as E } from './x';",
			Errors: []rule_tester.InvalidTestCaseError{{MessageId: "exportRename", Line: 1, Column: 13}, {MessageId: "exportRename", Line: 1, Column: 21}},
		},
	})
}
