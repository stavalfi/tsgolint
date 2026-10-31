package require_fs_utf8

import (
	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/typescript-eslint/tsgolint/internal/rule"
	"github.com/typescript-eslint/tsgolint/internal/utils"
)

// Index of the options/encoding argument for each fs function, i.e. how many
// arguments come before it.
var encodingArgIndex = map[string]int{
	"readFile":       1,
	"readFileSync":   1,
	"writeFile":      2,
	"writeFileSync":  2,
	"appendFile":     2,
	"appendFileSync": 2,
}

// Only these modules count. The JS rule matched on the method name alone, so
// any `readFile` on any object was reported.
var fsSpecifiers = []utils.TypeOrValueSpecifier{
	{From: utils.TypeOrValueSpecifierFromPackage, Package: "fs", Name: []string{
		"readFile", "readFileSync", "writeFile", "writeFileSync", "appendFile", "appendFileSync",
	}},
	{From: utils.TypeOrValueSpecifierFromPackage, Package: "fs/promises", Name: []string{
		"readFile", "writeFile", "appendFile",
	}},
	{From: utils.TypeOrValueSpecifierFromPackage, Package: "node:fs", Name: []string{
		"readFile", "readFileSync", "writeFile", "writeFileSync", "appendFile", "appendFileSync",
	}},
	{From: utils.TypeOrValueSpecifierFromPackage, Package: "node:fs/promises", Name: []string{
		"readFile", "writeFile", "appendFile",
	}},
}

func buildFsUtf8Message() rule.RuleMessage {
	return rule.RuleMessage{
		Id:          "fsUtf8",
		Description: `Pass "utf8" explicitly: without an encoding this returns a Buffer, or writes one.`,
		Help:        `Add { encoding: "utf8" } as the options object.`,
	}
}

// An options object that already names `encoding` is fine; so is a bare string
// or template literal, which is the encoding itself. `null` and `undefined`, in
// the slot or as the `encoding` value, mean no encoding: node returns a Buffer.
func declaresEncoding(node *ast.Node) bool {
	if isNullish(node) {
		return false
	}
	if !ast.IsObjectLiteralExpression(node) {
		return true
	}
	for _, property := range node.AsObjectLiteralExpression().Properties.Nodes {
		if property.Kind == ast.KindSpreadAssignment {
			return true
		}
		name := property.Name()
		if name == nil || name.Text() != "encoding" {
			continue
		}
		initializer := property.Initializer()
		return initializer == nil || !isNullish(initializer)
	}
	return false
}

func isNullish(node *ast.Node) bool {
	return node.Kind == ast.KindNullKeyword || (ast.IsIdentifier(node) && node.Text() == "undefined")
}

func calleeName(expression *ast.Node) string {
	if ast.IsIdentifier(expression) {
		return expression.Text()
	}
	if ast.IsPropertyAccessExpression(expression) {
		return expression.AsPropertyAccessExpression().Name().Text()
	}
	return ""
}

var RequireFsUtf8Rule = rule.Rule{
	Name: "require-fs-utf8",
	Run: func(ctx rule.RuleContext, options any) rule.RuleListeners {
		return rule.RuleListeners{
			ast.KindCallExpression: func(node *ast.Node) {
				call := node.AsCallExpression()
				index, watched := encodingArgIndex[calleeName(call.Expression)]
				if !watched {
					return
				}

				if !utils.ValueMatchesSomeSpecifier(call.Expression, fsSpecifiers, ctx.Program, ctx.TypeChecker) {
					return
				}

				if call.Arguments == nil {
					return
				}
				args := call.Arguments.Nodes
				if len(args) < index {
					return
				}

				if len(args) > index {
					if declaresEncoding(args[index]) {
						return
					}
					ctx.ReportNode(node, buildFsUtf8Message())
					return
				}

				anchor := args[index-1]
				ctx.ReportNodeWithFixes(node, buildFsUtf8Message(), func() []rule.RuleFix {
					return []rule.RuleFix{rule.RuleFixInsertAfter(anchor, `, { encoding: "utf8" }`)}
				})
			},
		}
	},
}
