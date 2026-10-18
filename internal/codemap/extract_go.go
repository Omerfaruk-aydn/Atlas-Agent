package codemap

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strconv"
	"strings"
)

// extractGo reads a Go file with the standard library's parser, which
// handles Go exactly.
func extractGo(out *File, content string) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, out.Path, content, parser.SkipObjectResolution)
	if file == nil {
		return
	}
	_ = err // A file with syntax errors still yields what parsed.
	lines := strings.Split(content, "\n")
	goEmbeds(out, lines)
	line := func(p token.Pos) int { return fset.Position(p).Line }
	signature := func(n int) string {
		if n < 1 || n > len(lines) {
			return ""
		}
		return clipSignature(lines[n-1])
	}
	for _, spec := range file.Imports {
		value, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		imp := Import{Spec: value, Line: line(spec.Pos()), Module: path.Base(value)}
		if spec.Name != nil {
			switch spec.Name.Name {
			case ".":
				// A dot import brings the package's names into scope.
				imp.Module = ""
			case "_":
				imp.Module = "_"
			default:
				imp.Module = spec.Name.Name
			}
		}
		out.Imports = append(out.Imports, imp)
	}
	ref := func(name, qualifier, kind string, p token.Pos) {
		if len(out.Refs) < maxRefsPerFile {
			out.Refs = append(out.Refs, Ref{Name: name, Qualifier: qualifier, Kind: kind, Line: line(p)})
		}
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			sym := Symbol{Name: d.Name.Name, Kind: "function", Line: line(d.Pos()), EndLine: line(d.End()), Signature: signature(line(d.Name.Pos())), Exported: d.Name.IsExported()}
			if d.Recv != nil && len(d.Recv.List) > 0 {
				sym.Kind = "method"
				sym.Container = receiverName(d.Recv.List[0].Type)
			}
			out.Symbols = append(out.Symbols, sym)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				kind := "type"
				switch t := ts.Type.(type) {
				case *ast.StructType:
					kind = "struct"
					for _, field := range t.Fields.List {
						if len(field.Names) == 0 {
							if name, q := typeName(field.Type); name != "" {
								ref(name, q, RefInherit, field.Pos())
							}
						}
					}
				case *ast.InterfaceType:
					kind = "interface"
					for _, method := range t.Methods.List {
						if len(method.Names) == 0 {
							if name, q := typeName(method.Type); name != "" {
								ref(name, q, RefInherit, method.Pos())
							}
						}
					}
				}
				out.Symbols = append(out.Symbols, Symbol{Name: ts.Name.Name, Kind: kind, Line: line(ts.Pos()), EndLine: line(ts.End()), Signature: signature(line(ts.Pos())), Exported: ts.Name.IsExported()})
			}
		}
	}
	tags := map[*ast.BasicLit]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.CallExpr:
			switch fun := e.Fun.(type) {
			case *ast.Ident:
				ref(fun.Name, "", RefCall, fun.Pos())
			case *ast.SelectorExpr:
				ref(fun.Sel.Name, qualifierOf(fun.X), RefCall, fun.Sel.Pos())
			case *ast.IndexExpr:
				if name, q := typeName(fun.X); name != "" {
					ref(name, q, RefCall, fun.Pos())
				}
			}
		case *ast.CompositeLit:
			if name, q := typeName(e.Type); name != "" {
				ref(name, q, RefNew, e.Pos())
			}
		case *ast.ImportSpec:
			return false
		case *ast.Field:
			if e.Tag != nil {
				// A struct tag is not a path or a query.
				tags[e.Tag] = true
			}
		case *ast.BasicLit:
			if e.Kind == token.STRING && !tags[e] {
				if value, err := strconv.Unquote(e.Value); err == nil {
					scanString(out, value, line(e.Pos()))
				}
			}
		}
		return true
	})
}

// receiverName is the type name of a method receiver: T in (t *T[K]).
func receiverName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return receiverName(t.X)
	case *ast.IndexExpr:
		return receiverName(t.X)
	case *ast.IndexListExpr:
		return receiverName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return ""
}

// typeName returns the name and package qualifier of a named type.
func typeName(expr ast.Expr) (string, string) {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name, ""
	case *ast.StarExpr:
		return typeName(t.X)
	case *ast.SelectorExpr:
		return t.Sel.Name, qualifierOf(t.X)
	case *ast.IndexExpr:
		return typeName(t.X)
	case *ast.IndexListExpr:
		return typeName(t.X)
	}
	return "", ""
}

// qualifierOf is the last name in the expression before a selector: pkg
// in pkg.F, client in s.client.Do.
func qualifierOf(expr ast.Expr) string {
	switch x := expr.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	case *ast.CallExpr:
		return ""
	}
	return ""
}

