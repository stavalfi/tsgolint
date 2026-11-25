package link_signal_first_statement

import (
	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/checker"
	"github.com/microsoft/typescript-go/shim/core"
	"github.com/typescript-eslint/tsgolint/internal/rule"
	"github.com/typescript-eslint/tsgolint/internal/utils"
)

func buildLinkSignalFirstMessage(callerSignal string) rule.RuleMessage {
	return rule.RuleMessage{
		Id:          "linkSignalFirst",
		Description: "An async method links the caller's signal once, as its first statement, so nothing below it can run after this instance was closed.",
		Help:        "Start the body with `const signal = super.linkSignal({ signal: " + callerSignal + " });` and use `signal` below it.",
	}
}

func buildLinkSignalTwiceMessage() rule.RuleMessage {
	return rule.RuleMessage{
		Id:          "linkSignalTwice",
		Description: "The caller's signal is linked by the first statement already; linking it again wraps it twice.",
		Help:        "Use `signal` instead.",
	}
}

func walk(node *ast.Node, visit func(*ast.Node)) {
	node.ForEachChild(func(child *ast.Node) bool {
		visit(child)
		walk(child, visit)
		return false
	})
}

// Whether node reads the constant that declaration declares.
func readsDeclaration(typeChecker *checker.Checker, node *ast.Node, declaration *ast.Node) bool {
	symbol := utils.ValueSymbolOf(typeChecker, node)
	return symbol != nil && symbol.ValueDeclaration == declaration
}

// Every read of declaration's constant inside body.
func referencesOf(typeChecker *checker.Checker, body *ast.Node, declaration *ast.Node) []*ast.Node {
	name := declaration.Name().Text()
	references := []*ast.Node{}
	walk(body, func(node *ast.Node) {
		if !ast.IsIdentifier(node) || node.Text() != name || node == declaration.Name() {
			return
		}
		parent := node.Parent
		var symbol *ast.Symbol
		if parent != nil && parent.Kind == ast.KindShorthandPropertyAssignment && parent.Name() == node {
			symbol = checker.Checker_GetShorthandAssignmentValueSymbol(typeChecker, parent)
		} else {
			if ast.IsDeclarationName(node) {
				return
			}
			symbol = typeChecker.GetSymbolAtLocation(node)
		}
		if symbol != nil && symbol.ValueDeclaration == declaration {
			references = append(references, node)
		}
	})
	return references
}

// A top-level `const signal` / `let signal` in body that is not the linked one.
func declaresSignalElsewhere(body *ast.Node, linked *utils.LinkSignalDeclaration) bool {
	for _, statement := range body.AsBlock().Statements.Nodes {
		if !ast.IsVariableStatement(statement) {
			continue
		}
		for _, declaration := range statement.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes {
			if linked != nil && declaration == linked.Declaration {
				continue
			}
			name := declaration.Name()
			if name != nil && ast.IsIdentifier(name) && name.Text() == "signal" {
				return true
			}
		}
	}
	return false
}

// The statement's text plus the whitespace up to whatever follows it, so
// removing it does not leave a blank line behind.
func statementRange(sourceFile *ast.SourceFile, statement *ast.Node) core.TextRange {
	text := sourceFile.Text()
	end := statement.End()
	for end < len(text) && (text[end] == ' ' || text[end] == '\t' || text[end] == '\n' || text[end] == '\r') {
		end++
	}
	return utils.TrimNodeTextRange(sourceFile, statement).WithEnd(end)
}

// The indentation of the line node starts on, plus one level.
func bodyIndentation(sourceFile *ast.SourceFile, node *ast.Node) string {
	text := sourceFile.Text()
	start := utils.TrimNodeTextRange(sourceFile, node).Pos()
	lineStart := start
	for lineStart > 0 && text[lineStart-1] != '\n' {
		lineStart--
	}
	indentEnd := lineStart
	for indentEnd < start && (text[indentEnd] == ' ' || text[indentEnd] == '\t') {
		indentEnd++
	}
	return text[lineStart:indentEnd] + "  "
}

var LinkSignalFirstStatementRule = rule.Rule{
	Name: "link-signal-first-statement",
	Run: func(ctx rule.RuleContext, options any) rule.RuleListeners {
		return rule.RuleListeners{
			ast.KindMethodDeclaration: func(method *ast.Node) {
				caller := utils.LinksSignalFirst(ctx.TypeChecker, method)
				if caller == nil || utils.StartsWithLinkedSignal(ctx.TypeChecker, method) {
					return
				}
				body := method.Body()
				linked := utils.FindLinkSignalDeclaration(method)
				// A `const x = super.linkSignal(...)` that links something other
				// than the caller's signal is not the statement this rule wants.
				if linked != nil {
					expression := utils.LinkedSignalExpression(linked.Call.AsCallExpression().Arguments.Nodes[0])
					if expression == nil ||
						!(utils.ReadsParameterOf(ctx.TypeChecker, expression, method) || readsDeclaration(ctx.TypeChecker, expression, linked.Declaration)) {
						linked = nil
					}
				}
				reportNode := method.Name()
				if linked != nil {
					reportNode = linked.Statement
				}
				message := buildLinkSignalFirstMessage(caller.Text)
				fixable := !declaresSignalElsewhere(body, linked)
				if linked != nil {
					declarations := linked.Statement.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes
					if len(declarations) != 1 || linked.Name() == "" {
						fixable = false
					}
				}
				if !fixable {
					ctx.ReportNode(reportNode, message)
					return
				}
				ctx.ReportNodeWithFixes(reportNode, message, func() []rule.RuleFix {
					fixes := []rule.RuleFix{}
					if caller.Rename != nil {
						fixes = append(fixes, rule.RuleFixReplace(ctx.SourceFile, caller.Rename, "signal: "+utils.RenamedCallerSignal))
					}
					if linked != nil {
						fixes = append(fixes, rule.RuleFixRemoveRange(statementRange(ctx.SourceFile, linked.Statement)))
						if linked.Name() != "signal" {
							for _, reference := range referencesOf(ctx.TypeChecker, body, linked.Declaration) {
								target, text := utils.ReplaceWithLinkedSignal(ctx.SourceFile, reference)
								fixes = append(fixes, rule.RuleFixReplace(ctx.SourceFile, target, text))
							}
						}
					}
					openBrace := utils.TrimNodeTextRange(ctx.SourceFile, body).Pos() + 1
					line := "\n" + bodyIndentation(ctx.SourceFile, method) + "const signal = super.linkSignal({ signal: " + caller.Text + " });"
					fixes = append(fixes, rule.RuleFixReplaceRange(core.NewTextRange(openBrace, openBrace), line))
					return fixes
				})
			},
			// A second `super.linkSignal` of the caller's signal below the first statement.
			ast.KindCallExpression: func(node *ast.Node) {
				if !utils.IsSuperLinkSignalCall(node) {
					return
				}
				method := utils.EnclosingMethod(node)
				if method == nil || utils.LinksSignalFirst(ctx.TypeChecker, method) == nil {
					return
				}
				linked := utils.FindLinkSignalDeclaration(method)
				if linked != nil && linked.Call == node {
					return
				}
				expression := utils.LinkedSignalExpression(node.AsCallExpression().Arguments.Nodes[0])
				if expression == nil {
					return
				}
				if !utils.ReadsParameterOf(ctx.TypeChecker, expression, method) &&
					!(linked != nil && readsDeclaration(ctx.TypeChecker, expression, linked.Declaration)) {
					return
				}
				ctx.ReportNodeWithFixes(node, buildLinkSignalTwiceMessage(), func() []rule.RuleFix {
					target, text := utils.ReplaceWithLinkedSignal(ctx.SourceFile, node)
					return []rule.RuleFix{rule.RuleFixReplace(ctx.SourceFile, target, text)}
				})
			},
		}
	},
}
