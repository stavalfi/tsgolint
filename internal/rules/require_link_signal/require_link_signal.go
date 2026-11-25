package require_link_signal

import (
	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/checker"
	"github.com/typescript-eslint/tsgolint/internal/rule"
	"github.com/typescript-eslint/tsgolint/internal/utils"
)

func buildRequireLinkSignalMessage(text string) rule.RuleMessage {
	return rule.RuleMessage{
		Id:          "requireLinkSignal",
		Description: "`" + text + "` is the caller's signal only — it ignores this instance being closed.",
		Help:        "Use `super.linkSignal({ signal: " + text + " })` instead.",
	}
}

// Whether declaration is a parameter — or a binding inside one — of a method
// whose class inherits `linkSignal`.
func isLinkableParameter(typeChecker *checker.Checker, declaration *ast.Node) bool {
	root := ast.GetRootDeclaration(declaration)
	if root.Kind != ast.KindParameter {
		return false
	}
	function := root.Parent
	switch function.Kind {
	case ast.KindMethodDeclaration, ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor:
	default:
		return false
	}
	// `linkSignal` is an instance member; a static method has no `super` to reach it.
	if ast.IsStatic(function) {
		return false
	}
	return utils.InheritsLinkSignal(typeChecker, function.Parent)
}

// `super.linkSignal({ signal })` and `super.linkSignal({ signal: options.signal })`
// are the one place the raw parameter belongs.
func isLinkSignalArgument(node *ast.Node) bool {
	property := node.Parent
	switch {
	case property == nil:
		return false
	case property.Kind == ast.KindShorthandPropertyAssignment && property.Name() == node:
	case property.Kind == ast.KindPropertyAssignment && property.Initializer() == node:
	default:
		return false
	}
	object := property.Parent
	call := object.Parent
	if call == nil || !utils.IsSuperLinkSignalCall(call) {
		return false
	}
	return call.AsCallExpression().Arguments.Nodes[0] == object
}

var RequireLinkSignalRule = rule.Rule{
	Name: "require-link-signal",
	Run: func(ctx rule.RuleContext, options any) rule.RuleListeners {
		check := func(node *ast.Node, symbol *ast.Symbol) {
			if symbol == nil || symbol.ValueDeclaration == nil || !isLinkableParameter(ctx.TypeChecker, symbol.ValueDeclaration) {
				return
			}
			if !utils.IsAbortSignalType(ctx.TypeChecker.GetTypeAtLocation(node)) || isLinkSignalArgument(node) {
				return
			}
			textRange := utils.TrimNodeTextRange(ctx.SourceFile, node)
			text := ctx.SourceFile.Text()[textRange.Pos():textRange.End()]
			message := buildRequireLinkSignalMessage(text)
			// `super` is only valid up to the nearest non-arrow function, so the
			// fix stops being safe inside a nested `function` of the method.
			method := utils.EnclosingMethod(node)
			if method == nil {
				ctx.ReportNode(node, message)
				return
			}
			// An async method links once, in its first statement
			// (link-signal-first-statement): a read below it becomes `signal`,
			// and until that statement exists there is nothing safe to rewrite to.
			if utils.LinksSignalFirst(ctx.TypeChecker, method) != nil {
				if !utils.StartsWithLinkedSignal(ctx.TypeChecker, method) || utils.ShadowsSignal(node, method) {
					ctx.ReportNode(node, message)
					return
				}
				ctx.ReportNodeWithFixes(node, message, func() []rule.RuleFix {
					target, replacement := utils.ReplaceWithLinkedSignal(ctx.SourceFile, node)
					if target == nil {
						return nil
					}
					return []rule.RuleFix{rule.RuleFixReplace(ctx.SourceFile, target, replacement)}
				})
				return
			}
			ctx.ReportNodeWithFixes(node, message, func() []rule.RuleFix {
				linked := "super.linkSignal({ signal: " + text + " })"
				if text == "signal" {
					linked = "super.linkSignal({ signal })"
				}
				if parent := node.Parent; parent.Kind == ast.KindShorthandPropertyAssignment {
					return []rule.RuleFix{rule.RuleFixReplace(ctx.SourceFile, parent, text+": "+linked)}
				}
				return []rule.RuleFix{rule.RuleFixReplaceRange(textRange, linked)}
			})
		}

		return rule.RuleListeners{
			ast.KindIdentifier: func(node *ast.Node) {
				parent := node.Parent
				if parent != nil && parent.Kind == ast.KindShorthandPropertyAssignment && parent.Name() == node {
					check(node, checker.Checker_GetShorthandAssignmentValueSymbol(ctx.TypeChecker, parent))
					return
				}
				if ast.IsDeclarationName(node) {
					return
				}
				check(node, ctx.TypeChecker.GetSymbolAtLocation(node))
			},
			// `options.signal`, where `options` is the parameter.
			ast.KindPropertyAccessExpression: func(node *ast.Node) {
				root := utils.RootIdentifier(node)
				if root == nil {
					return
				}
				check(node, ctx.TypeChecker.GetSymbolAtLocation(root))
			},
		}
	},
}
