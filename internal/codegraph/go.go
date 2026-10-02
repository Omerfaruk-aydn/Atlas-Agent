package codegraph

import (
	"context"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"path"
	"sort"
	"strconv"
	"strings"
)

type goUnit struct {
	source Source
	file   *ast.File
	info   *types.Info
}

type packageGraph struct {
	units    map[string][]*goUnit
	checked  map[string]*types.Package
	checking map[string]bool
	fset     *token.FileSet
	graph    *CodeGraph
	ctx      context.Context
}

func (loader *packageGraph) Import(importPath string) (*types.Package, error) {
	if err := loader.ctx.Err(); err != nil {
		return nil, err
	}
	if pkg := loader.checked[importPath]; pkg != nil {
		return pkg, nil
	}
	units, ok := loader.units[importPath]
	if !ok {
		return importer.Default().Import(importPath)
	}
	if loader.checking[importPath] {
		return nil, fmt.Errorf("cyclic local import %s", importPath)
	}
	loader.checking[importPath] = true
	defer delete(loader.checking, importPath)
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}, Types: map[ast.Expr]types.TypeAndValue{}}
	files := make([]*ast.File, len(units))
	for i, unit := range units {
		unit.info = info
		files[i] = unit.file
	}
	checker := types.Config{Importer: loader, Error: func(err error) { loader.graph.gap(err.Error()) }}
	pkg, err := checker.Check(importPath, loader.fset, files, info)
	if pkg != nil {
		loader.checked[importPath] = pkg
	}
	return pkg, err
}

func (graph *CodeGraph) gap(message string) {
	graph.Partial = true
	if len(graph.Gaps) < 32 {
		graph.Gaps = append(graph.Gaps, strings.ToValidUTF8(message[:min(len(message), 512)], ""))
	}
}

func BuildGo(ctx context.Context, root string, sources []Source) (CodeGraph, error) {
	graph := CodeGraph{Root: root, SourceFingerprint: Fingerprint(sources), Nodes: []Node{}, Edges: []Edge{}, Gaps: []string{}}
	loader := &packageGraph{ctx: ctx, units: map[string][]*goUnit{}, checked: map[string]*types.Package{}, checking: map[string]bool{}, fset: token.NewFileSet(), graph: &graph}
	module := "project"
	for _, source := range sources {
		if source.Path == "go.mod" {
			for _, line := range strings.Split(string(source.Content), "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 2 && fields[0] == "module" {
					module = strings.Trim(fields[1], `"`)
				}
			}
		}
	}
	var units []*goUnit
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return graph, err
		}
		if !strings.HasSuffix(source.Path, ".go") {
			continue
		}
		if len(source.Content) > 64*1024 || source.Hash != digest(string(source.Content)) {
			return graph, fmt.Errorf("invalid or changed graph source %s", source.Path)
		}
		parsed, err := parser.ParseFile(loader.fset, source.Path, source.Content, parser.AllErrors)
		if err != nil {
			graph.gap(err.Error())
		}
		if parsed == nil {
			continue
		}
		unit := &goUnit{source: source, file: parsed}
		units = append(units, unit)
		pkgPath := path.Join(module, path.Dir(source.Path))
		if strings.HasSuffix(parsed.Name.Name, "_test") {
			pkgPath += "_test"
		}
		loader.units[pkgPath] = append(loader.units[pkgPath], unit)
	}
	packages := make([]string, 0, len(loader.units))
	for name := range loader.units {
		packages = append(packages, name)
	}
	sort.Strings(packages)
	for _, name := range packages {
		_, _ = loader.Import(name)
		if err := ctx.Err(); err != nil {
			return graph, err
		}
	}
	objects := map[types.Object]string{}
	declarations := map[*ast.FuncDecl]string{}
	addNode := func(unit *goUnit, symbol, kind string, start, end token.Pos) string {
		if len(graph.Nodes) >= 20000 {
			graph.gap("Graph reached 20000 nodes")
			return ""
		}
		id := digest(unit.source.Path + "\x00" + symbol + "\x00" + strconv.Itoa(int(start)))
		graph.Nodes = append(graph.Nodes, Node{ID: id, Language: "go", Path: unit.source.Path, Symbol: symbol, Kind: kind, FileHash: unit.source.Hash, StartLine: loader.fset.Position(start).Line, EndLine: loader.fset.Position(end).Line})
		return id
	}
	for _, unit := range units {
		ast.Inspect(unit.file, func(node ast.Node) bool {
			switch decl := node.(type) {
			case *ast.FuncDecl:
				id := addNode(unit, decl.Name.Name, "function", decl.Pos(), decl.End())
				declarations[decl] = id
				if unit.info != nil && unit.info.Defs[decl.Name] != nil {
					objects[unit.info.Defs[decl.Name]] = id
				}
			case *ast.TypeSpec:
				id := addNode(unit, decl.Name.Name, "type", decl.Pos(), decl.End())
				if unit.info != nil && unit.info.Defs[decl.Name] != nil {
					objects[unit.info.Defs[decl.Name]] = id
				}
			}
			return true
		})
	}
	seenEdges := map[string]bool{}
	addEdge := func(from, to, kind string) {
		if from == "" || to == "" || from == to {
			return
		}
		key := from + "\x00" + to + "\x00" + kind
		if seenEdges[key] {
			return
		}
		if len(graph.Edges) >= 50000 {
			graph.gap("Graph reached 50000 edges")
			return
		}
		seenEdges[key] = true
		graph.Edges = append(graph.Edges, Edge{From: from, To: to, Kind: kind, Resolution: "resolved", Origin: "go-types"})
	}
	for _, unit := range units {
		if err := ctx.Err(); err != nil {
			return graph, err
		}
		for _, imp := range unit.file.Imports {
			name, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				continue
			}
			id := addNode(unit, name, "import", imp.Pos(), imp.End())
			for _, target := range units {
				if path.Join(module, path.Dir(target.source.Path)) == name {
					for decl, to := range declarations {
						if loader.fset.Position(decl.Pos()).Filename == target.source.Path {
							addEdge(id, to, "imports")
						}
					}
				}
			}
		}
		if unit.info == nil {
			continue
		}
		for _, declaration := range unit.file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			from := declarations[fn]
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				switch expression := node.(type) {
				case *ast.Ident:
					addEdge(from, objects[unit.info.Uses[expression]], "references")
				case *ast.CallExpr:
					var object types.Object
					switch callee := expression.Fun.(type) {
					case *ast.Ident:
						object = unit.info.Uses[callee]
					case *ast.SelectorExpr:
						if selection := unit.info.Selections[callee]; selection != nil {
							if _, dynamic := selection.Recv().Underlying().(*types.Interface); dynamic {
								graph.gap("Dynamic interface call in " + unit.source.Path)
								return true
							}
						}
						object = unit.info.Uses[callee.Sel]
					}
					if function, ok := object.(*types.Func); ok {
						addEdge(from, objects[function.Origin()], "calls")
					}
				}
				return true
			})
		}
	}
	var typeObjects []*types.TypeName
	for object := range objects {
		if name, ok := object.(*types.TypeName); ok {
			typeObjects = append(typeObjects, name)
		}
	}
	if len(typeObjects) <= 256 {
		for _, target := range typeObjects {
			if iface, ok := target.Type().Underlying().(*types.Interface); ok {
				iface.Complete()
				for _, candidate := range typeObjects {
					if candidate != target && (types.Implements(candidate.Type(), iface) || types.Implements(types.NewPointer(candidate.Type()), iface)) {
						addEdge(objects[candidate], objects[target], "implements")
					}
				}
			}
		}
	} else {
		graph.gap("Implementation analysis bounded to 256 types")
	}
	sort.Slice(graph.Nodes, func(i, j int) bool { return graph.Nodes[i].ID < graph.Nodes[j].ID })
	sort.Slice(graph.Edges, func(i, j int) bool {
		a, b := graph.Edges[i], graph.Edges[j]
		return a.From+":"+a.To+":"+a.Kind < b.From+":"+b.To+":"+b.Kind
	})
	return graph, nil
}
