package no_export_rename

import (
	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/typescript-eslint/tsgolint/internal/rule"
)

func buildExportRenameMessage() rule.RuleMessage {
	return rule.RuleMessage{
		Id:          "exportRename",
		Description: "Renaming an export with `as` is banned.",
		Help:        "Export the binding under its own name; rename it at its declaration instead.",
	}
}

var NoExportRenameRule = rule.Rule{
	Name: "no-export-rename",
	Run: func(ctx rule.RuleContext, options any) rule.RuleListeners {
		return rule.RuleListeners{
			ast.KindExportSpecifier: func(node *ast.Node) {
				if node.AsExportSpecifier().PropertyName == nil {
					return
				}
				ctx.ReportNode(node, buildExportRenameMessage())
			},
		}
	},
}
