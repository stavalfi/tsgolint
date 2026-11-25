package utils

import (
	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/checker"
)

// Shared between require-link-signal and link-signal-first-statement: the
// shapes of `super.linkSignal(...)` and of the method that owns it.

// `super.linkSignal(<at least one argument>)`.
func IsSuperLinkSignalCall(node *ast.Node) bool {
	if node == nil || !ast.IsCallExpression(node) {
		return false
	}
	arguments := node.AsCallExpression().Arguments
	if arguments == nil || len(arguments.Nodes) == 0 {
		return false
	}
	callee := node.AsCallExpression().Expression
	if !ast.IsPropertyAccessExpression(callee) {
		return false
	}
	access := callee.AsPropertyAccessExpression()
	return access.Expression.Kind == ast.KindSuperKeyword && access.Name().Text() == "linkSignal"
}

// The signal expression inside `{ signal }` / `{ signal: <expr> }`, or nil when
// the argument has any other shape.
func LinkedSignalExpression(argument *ast.Node) *ast.Node {
	if argument == nil || !ast.IsObjectLiteralExpression(argument) {
		return nil
	}
	properties := argument.AsObjectLiteralExpression().Properties
	if properties == nil || len(properties.Nodes) != 1 {
		return nil
	}
	property := properties.Nodes[0]
	switch property.Kind {
	case ast.KindShorthandPropertyAssignment:
		if property.Name().Text() == "signal" {
			return property.Name()
		}
	case ast.KindPropertyAssignment:
		if name := property.Name(); ast.IsIdentifier(name) && name.Text() == "signal" {
			return property.Initializer()
		}
	}
	return nil
}

// The method `super` binds to at node, or nil inside a nested `function`,
// where `super` is not valid.
func EnclosingMethod(node *ast.Node) *ast.Node {
	for current := node.Parent; current != nil; current = current.Parent {
		switch current.Kind {
		case ast.KindMethodDeclaration, ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor:
			return current
		case ast.KindFunctionExpression, ast.KindFunctionDeclaration:
			return nil
		}
	}
	return nil
}

// Whether classNode extends something whose instance type has `linkSignal`.
// The base class that declares it extends nothing, so it is not checked
// against itself.
func InheritsLinkSignal(typeChecker *checker.Checker, classNode *ast.Node) bool {
	if classNode == nil || !ast.IsClassLike(classNode) || ast.GetExtendsHeritageClauseElement(classNode) == nil {
		return false
	}
	classSymbol := classNode.Symbol()
	if classSymbol == nil {
		return false
	}
	instanceType := checker.Checker_getDeclaredTypeOfSymbol(typeChecker, classSymbol)
	return instanceType != nil && checker.Checker_getPropertyOfType(typeChecker, instanceType, "linkSignal") != nil
}

// The identifier a `a.b.c` chain starts from, or nil.
func RootIdentifier(node *ast.Node) *ast.Node {
	for node != nil && ast.IsPropertyAccessExpression(node) {
		node = node.AsPropertyAccessExpression().Expression
	}
	if node == nil || !ast.IsIdentifier(node) {
		return nil
	}
	return node
}

// The value an identifier or `a.b` chain reads from, resolving a shorthand
// `{ signal }` to the variable it reads rather than the property it declares.
func ValueSymbolOf(typeChecker *checker.Checker, node *ast.Node) *ast.Symbol {
	root := RootIdentifier(node)
	if root == nil {
		return nil
	}
	if parent := root.Parent; parent != nil && parent.Kind == ast.KindShorthandPropertyAssignment && parent.Name() == root {
		return checker.Checker_GetShorthandAssignmentValueSymbol(typeChecker, parent)
	}
	return typeChecker.GetSymbolAtLocation(root)
}

// Whether node (an identifier or `a.b` chain) reads a parameter — or a
// binding inside one — of method.
func ReadsParameterOf(typeChecker *checker.Checker, node *ast.Node, method *ast.Node) bool {
	symbol := ValueSymbolOf(typeChecker, node)
	if symbol == nil || symbol.ValueDeclaration == nil {
		return false
	}
	declaration := ast.GetRootDeclaration(symbol.ValueDeclaration)
	return declaration.Kind == ast.KindParameter && declaration.Parent == method
}

// LinkSignalDeclaration is the first top-level `const <name> = super.linkSignal(...)`
// statement of a method body.
type LinkSignalDeclaration struct {
	Statement   *ast.Node
	Declaration *ast.Node
	Call        *ast.Node
	Index       int
}

// Name of the declared constant, or "" for a destructuring declaration.
func (d LinkSignalDeclaration) Name() string {
	name := d.Declaration.Name()
	if name == nil || !ast.IsIdentifier(name) {
		return ""
	}
	return name.Text()
}

// The first top-level `const X = super.linkSignal(...)` of method's body, or nil.
func FindLinkSignalDeclaration(method *ast.Node) *LinkSignalDeclaration {
	body := method.Body()
	if body == nil || !ast.IsBlock(body) {
		return nil
	}
	for index, statement := range body.AsBlock().Statements.Nodes {
		if !ast.IsVariableStatement(statement) {
			continue
		}
		declarations := statement.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes
		for _, declaration := range declarations {
			initializer := declaration.Initializer()
			if initializer != nil && IsSuperLinkSignalCall(ast.SkipParentheses(initializer)) {
				return &LinkSignalDeclaration{
					Statement:   statement,
					Declaration: declaration,
					Call:        ast.SkipParentheses(initializer),
					Index:       index,
				}
			}
		}
	}
	return nil
}

// Whether method already starts with `const signal = super.linkSignal({ signal: <parameter> })`.
func StartsWithLinkedSignal(typeChecker *checker.Checker, method *ast.Node) bool {
	linked := FindLinkSignalDeclaration(method)
	if linked == nil || linked.Index != 0 || linked.Name() != "signal" {
		return false
	}
	declarations := linked.Statement.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes
	if len(declarations) != 1 || !ast.IsVarConst(linked.Declaration) {
		return false
	}
	expression := LinkedSignalExpression(linked.Call.AsCallExpression().Arguments.Nodes[0])
	return expression != nil && ReadsParameterOf(typeChecker, expression, method)
}

// A `this` parameter is a type annotation, not an argument the caller passes.
func RealParameters(parameters []*ast.Node) []*ast.Node {
	params := make([]*ast.Node, 0, len(parameters))
	for _, parameter := range parameters {
		name := parameter.Name()
		if name != nil && ast.IsIdentifier(name) && name.Text() == "this" {
			continue
		}
		params = append(params, parameter)
	}
	return params
}

func IsAbortSignalType(t *checker.Type) bool {
	return t != nil && SomeUnionTypePart(t, func(part *checker.Type) bool {
		symbol := checker.Type_symbol(part)
		return symbol != nil && symbol.Name == "AbortSignal"
	})
}

// `async`, or declared to return a thenable.
func IsAsyncLike(typeChecker *checker.Checker, node *ast.Node) bool {
	if ast.HasSyntacticModifier(node, ast.ModifierFlagsAsync) {
		return true
	}
	signature := typeChecker.GetSignatureFromDeclaration(node)
	if signature == nil {
		return false
	}
	returnType := typeChecker.GetReturnTypeOfSignature(signature)
	if returnType == nil || IsIntrinsicErrorType(returnType) ||
		IsTypeFlagSet(returnType, checker.TypeFlagsAny|checker.TypeFlagsUnknown) {
		return false
	}
	return IsThenableType(typeChecker, node, returnType)
}

// CallerSignal is how a method's body reaches the `AbortSignal` its caller
// passed in.
type CallerSignal struct {
	// Source text that reads it: `callerSignal`, `options.signal`, `activityAbortSignal`.
	Text string
	// A shorthand `{ signal }` binding that must become `{ signal: callerSignal }`
	// before a `const signal` can be declared in the body, or nil.
	Rename *ast.Node
}

const RenamedCallerSignal = "callerSignal"

// Where method's first parameter carries the caller's `AbortSignal`, or nil
// when it carries none.
func FindCallerSignal(typeChecker *checker.Checker, method *ast.Node) *CallerSignal {
	params := RealParameters(method.Parameters())
	if len(params) == 0 {
		return nil
	}
	parameter := params[0]
	name := parameter.Name()
	if name == nil {
		return nil
	}
	switch {
	case ast.IsIdentifier(name):
		parameterType := typeChecker.GetTypeAtLocation(parameter)
		if parameterType == nil || IsIntrinsicErrorType(parameterType) {
			return nil
		}
		if IsAbortSignalType(parameterType) {
			return &CallerSignal{Text: name.Text()}
		}
		for _, part := range UnionTypeParts(parameterType) {
			for _, property := range checker.Checker_getPropertiesOfType(typeChecker, part) {
				if IsSymbolFlagSet(property, ast.SymbolFlagsOptional) {
					continue
				}
				if IsAbortSignalType(typeChecker.GetTypeOfSymbolAtLocation(property, parameter)) {
					return &CallerSignal{Text: name.Text() + "." + property.Name}
				}
			}
		}
	case ast.IsObjectBindingPattern(name):
		for _, element := range name.AsBindingPattern().Elements.Nodes {
			if !ast.IsBindingElement(element) {
				continue
			}
			bindingName := element.Name()
			if bindingName == nil || !ast.IsIdentifier(bindingName) {
				continue
			}
			if !IsAbortSignalType(typeChecker.GetTypeAtLocation(bindingName)) {
				continue
			}
			if element.AsBindingElement().PropertyName == nil && bindingName.Text() == "signal" {
				return &CallerSignal{Text: RenamedCallerSignal, Rename: element}
			}
			return &CallerSignal{Text: bindingName.Text()}
		}
	}
	return nil
}

// Whether method is one that link-signal-first-statement governs: an async
// instance method of a class that inherits `linkSignal`, taking the caller's
// signal. The returned CallerSignal is nil when it is not.
func LinksSignalFirst(typeChecker *checker.Checker, method *ast.Node) *CallerSignal {
	if method == nil || method.Kind != ast.KindMethodDeclaration || method.Body() == nil || ast.IsStatic(method) {
		return nil
	}
	if !InheritsLinkSignal(typeChecker, method.Parent) || !IsAsyncLike(typeChecker, method) {
		return nil
	}
	return FindCallerSignal(typeChecker, method)
}

// Replaces one read of the caller's or an older linked signal with the linked
// `signal`, keeping object literals shorthand: `{ signal: x }` becomes
// `{ signal }`, `{ x }` becomes `{ x: signal }`.
func ReplaceWithLinkedSignal(sourceFile *ast.SourceFile, node *ast.Node) (*ast.Node, string) {
	parent := node.Parent
	if parent != nil && parent.Kind == ast.KindPropertyAssignment && parent.Initializer() == node {
		if name := parent.Name(); ast.IsIdentifier(name) && name.Text() == "signal" {
			return parent, "signal"
		}
	}
	if parent != nil && parent.Kind == ast.KindShorthandPropertyAssignment && parent.Name() == node {
		// `{ signal }` already reads whatever `signal` means here: nothing to write.
		if node.Text() == "signal" {
			return nil, ""
		}
		return parent, node.Text() + ": signal"
	}
	return node, "signal"
}

// Whether pattern (an identifier or binding pattern) binds name.
func BindsName(pattern *ast.Node, name string) bool {
	if pattern == nil {
		return false
	}
	switch {
	case ast.IsIdentifier(pattern):
		return pattern.Text() == name
	case ast.IsObjectBindingPattern(pattern) || ast.IsArrayBindingPattern(pattern):
		for _, element := range pattern.AsBindingPattern().Elements.Nodes {
			if ast.IsBindingElement(element) && BindsName(element.Name(), name) {
				return true
			}
		}
	}
	return false
}

// Whether a `signal` written at node would resolve to something declared
// between node and method's own body: a nested function's parameter, or a
// `const signal` of an inner block. The method body's own top-level
// declarations do not count; the caller decides what to do with those.
func ShadowsSignal(node *ast.Node, method *ast.Node) bool {
	body := method.Body()
	for current := node.Parent; current != nil && current != method; current = current.Parent {
		if ast.IsFunctionLikeDeclaration(current) {
			for _, parameter := range current.Parameters() {
				if BindsName(parameter.Name(), "signal") {
					return true
				}
			}
			continue
		}
		if ast.IsBlock(current) && current != body {
			for _, statement := range current.AsBlock().Statements.Nodes {
				if !ast.IsVariableStatement(statement) {
					continue
				}
				for _, declaration := range statement.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes {
					if BindsName(declaration.Name(), "signal") {
						return true
					}
				}
			}
		}
	}
	return false
}

// `const signal = <caller signal>` at the top of method's body: the raw
// parameter under the name the linked signal is about to take.
func FindSignalAlias(typeChecker *checker.Checker, method *ast.Node) *ast.Node {
	body := method.Body()
	if body == nil || !ast.IsBlock(body) {
		return nil
	}
	for _, statement := range body.AsBlock().Statements.Nodes {
		if !ast.IsVariableStatement(statement) {
			continue
		}
		declarations := statement.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes
		if len(declarations) != 1 {
			continue
		}
		declaration := declarations[0]
		name := declaration.Name()
		initializer := declaration.Initializer()
		if name == nil || !ast.IsIdentifier(name) || name.Text() != "signal" || initializer == nil {
			continue
		}
		if ast.IsVarConst(declaration) && ReadsParameterOf(typeChecker, ast.SkipParentheses(initializer), method) {
			return statement
		}
	}
	return nil
}

// SignalRead is one place method's body reads the caller's signal.
type SignalRead struct {
	Node *ast.Node
	// The whole object parameter handed on as-is (`this.run(options)`), which
	// carries the raw signal inside it.
	WholeParameter bool
}

// Every read of the caller's signal in method's body: references to the
// destructured binding, `options.signal` off an object parameter, or that
// object parameter forwarded whole. A read that is the argument of
// `super.linkSignal({ signal: <read> })` is returned as that call instead,
// since the whole call is what gets replaced.
func CallerSignalReads(typeChecker *checker.Checker, method *ast.Node) []SignalRead {
	params := RealParameters(method.Parameters())
	body := method.Body()
	if len(params) == 0 || body == nil {
		return nil
	}
	parameter := params[0]
	name := parameter.Name()
	if name == nil {
		return nil
	}
	var bindingSymbol *ast.Symbol
	var parameterSymbol *ast.Symbol
	property := ""
	switch {
	case ast.IsIdentifier(name):
		parameterSymbol = typeChecker.GetSymbolAtLocation(name)
		parameterType := typeChecker.GetTypeAtLocation(parameter)
		if parameterType == nil || IsIntrinsicErrorType(parameterType) {
			return nil
		}
		if IsAbortSignalType(parameterType) {
			bindingSymbol = parameterSymbol
			parameterSymbol = nil
		} else {
		search:
			for _, part := range UnionTypeParts(parameterType) {
				for _, member := range checker.Checker_getPropertiesOfType(typeChecker, part) {
					if IsSymbolFlagSet(member, ast.SymbolFlagsOptional) {
						continue
					}
					if IsAbortSignalType(typeChecker.GetTypeOfSymbolAtLocation(member, parameter)) {
						property = member.Name
						break search
					}
				}
			}
			if property == "" {
				return nil
			}
		}
	case ast.IsObjectBindingPattern(name):
		for _, element := range name.AsBindingPattern().Elements.Nodes {
			if !ast.IsBindingElement(element) {
				continue
			}
			bindingName := element.Name()
			if bindingName != nil && ast.IsIdentifier(bindingName) && IsAbortSignalType(typeChecker.GetTypeAtLocation(bindingName)) {
				bindingSymbol = typeChecker.GetSymbolAtLocation(bindingName)
				break
			}
		}
		if bindingSymbol == nil {
			return nil
		}
	default:
		return nil
	}

	reads := []SignalRead{}
	seen := map[*ast.Node]bool{}
	add := func(read *ast.Node, whole bool) {
		// `{ signal: <read> }` handed to `super.linkSignal`: the call is the read.
		if call := linkSignalCallOf(read); call != nil {
			read = call
		}
		if !seen[read] {
			seen[read] = true
			reads = append(reads, SignalRead{Node: read, WholeParameter: whole})
		}
	}
	var walk func(node *ast.Node)
	walk = func(node *ast.Node) {
		node.ForEachChild(func(child *ast.Node) bool {
			switch {
			case bindingSymbol != nil && ast.IsIdentifier(child) && (isShorthandName(child) || !ast.IsDeclarationName(child)):
				if ValueSymbolOf(typeChecker, child) == bindingSymbol {
					add(child, false)
				}
			case parameterSymbol != nil && ast.IsPropertyAccessExpression(child):
				access := child.AsPropertyAccessExpression()
				if ast.IsIdentifier(access.Expression) && typeChecker.GetSymbolAtLocation(access.Expression) == parameterSymbol {
					if access.Name().Text() == property {
						add(child, false)
					}
					// Any other `options.x` is not a read of the signal.
					return false
				}
			case parameterSymbol != nil && ast.IsIdentifier(child) && isForwardedArgument(child):
				if typeChecker.GetSymbolAtLocation(child) == parameterSymbol {
					add(child, true)
				}
			}
			walk(child)
			return false
		})
	}
	walk(body)
	return reads
}

// Whether node is passed as a call argument as-is: `run(options)`.
func isForwardedArgument(node *ast.Node) bool {
	parent := node.Parent
	if parent == nil || !ast.IsCallExpression(parent) {
		return false
	}
	for _, argument := range parent.AsCallExpression().Arguments.Nodes {
		if argument == node {
			return true
		}
	}
	return false
}

// The `super.linkSignal(...)` call whose `{ signal }` / `{ signal: x }`
// argument read is, or nil.
func linkSignalCallOf(read *ast.Node) *ast.Node {
	property := read.Parent
	if property == nil {
		return nil
	}
	switch {
	case property.Kind == ast.KindShorthandPropertyAssignment && property.Name() == read:
	case property.Kind == ast.KindPropertyAssignment && property.Initializer() == read:
	default:
		return nil
	}
	object := property.Parent
	if object == nil || object.Parent == nil || !IsSuperLinkSignalCall(object.Parent) {
		return nil
	}
	if object.Parent.AsCallExpression().Arguments.Nodes[0] != object {
		return nil
	}
	return object.Parent
}

// A shorthand `{ signal }` names a property, but reads a variable.
func isShorthandName(node *ast.Node) bool {
	parent := node.Parent
	return parent != nil && parent.Kind == ast.KindShorthandPropertyAssignment && parent.Name() == node
}
