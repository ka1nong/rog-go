package main

import (
	"bytes"
	"code.google.com/p/rog-go/exp/go/ast"
	"code.google.com/p/rog-go/exp/go/parser"
	"code.google.com/p/rog-go/exp/go/printer"
	"code.google.com/p/rog-go/exp/go/types"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

var readStdin = flag.Bool("i", false, "read file from stdin")
var offset = flag.Int("o", -1, "file offset of identifier in stdin")
var debug = flag.Bool("debug", false, "debug mode")
var tflag = flag.Bool("t", false, "print type information")
var aflag = flag.Bool("a", false, "print public type and member information")
var Aflag = flag.Bool("A", false, "print all type and members information")
var fflag = flag.String("f", "", "Go source filename")
var acmeFlag = flag.Bool("acme", false, "use current acme window")

func fail(s string, a ...interface{}) {
	fmt.Fprint(os.Stderr, "godef: "+fmt.Sprintf(s, a...)+"\n")
	os.Exit(2)
}

func init() {
	// take GOPATH, set types.GoPath to it if it's not empty.
	p := os.Getenv("GOPATH")
	if p == "" {
		return
	}
	gopath := strings.Split(p, ":")
	for i, d := range gopath {
		gopath[i] = filepath.Join(d, "src")
	}
	r := os.Getenv("GOROOT")
	if r != "" {
		gopath = append(gopath, r+"/src/pkg")
	}
	types.GoPath = gopath
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: godef [flags] [expr]\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() > 1 {
		flag.Usage()
		os.Exit(2)
	}
	types.Debug = *debug
	*tflag = *tflag || *aflag || *Aflag
	searchpos := *offset
	filename := *fflag

	var afile *acmeFile
	var src []byte
	if *acmeFlag {
		var err error
		if afile, err = acmeCurrentFile(); err != nil {
			fail("%v", err)
		}
		filename, src, searchpos = afile.name, afile.body, afile.offset
	} else if *readStdin {
		src, _ = ioutil.ReadAll(os.Stdin)
	} else {
		// TODO if there's no filename, look in the current
		// directory and do something plausible.
		b, err := ioutil.ReadFile(filename)
		if err != nil {
			fail("cannot read %s: %v", filename, err)
		}
		src = b
	}
	pkgScope := ast.NewScope(parser.Universe)
	f, err := parser.ParseFile(types.FileSet, filename, src, 0, pkgScope)
	if f == nil {
		fail("cannot parse %s: %v", filename, err)
	}

	var e ast.Expr
	switch {
	case flag.NArg() > 0:
		e = parseExpr(f.Scope, flag.Arg(0))

	case searchpos >= 0:
		e = findIdentifier(f, searchpos)

	default:
		fmt.Fprintf(os.Stderr, "no expression or offset specified\n")
		flag.Usage()
		os.Exit(2)
	}
	// print old source location to facilitate backtracking
	if *acmeFlag {
		fmt.Printf("\t%s:#%d\n", afile.name, afile.runeOffset)
	}
	if !*tflag {
		// try local declarations only
		if obj, typ := types.ExprType(e, types.DefaultImporter); obj != nil {
			done(obj, typ)
		}
	}
	// add declarations from other files in the local package and try again
	pkg, _ := parseLocalPackage(filename, f, pkgScope)
	if pkg == nil && !*tflag {
		fmt.Printf("parseLocalPackage error: %v\n", err)
	}
	if obj, typ := types.ExprType(e, types.DefaultImporter); obj != nil {
		done(obj, typ)
	}
	fail("no declaration found for %v", pretty{e})
}

// findIdentifier looks for an identifier at byte-offset searchpos
// inside the parsed source represented by node.
// If it is part of a selector expression, it returns
// that expression rather than the identifier itself.
//
func findIdentifier(f *ast.File, searchpos int) ast.Expr {
	ec := make(chan ast.Expr)
	go func() {
		var visit FVisitor = func(n ast.Node) bool {
			var id *ast.Ident
			switch n := n.(type) {
			case *ast.Ident:
				id = n
			case *ast.SelectorExpr:
				id = n.Sel
			default:
				return true
			}

			pos := types.FileSet.Position(id.NamePos)
			if pos.Offset <= searchpos && pos.Offset+len(id.Name) >= searchpos {
				ec <- n.(ast.Expr)
				runtime.Goexit()
			}
			return true
		}
		ast.Walk(visit, f)
		ec <- nil
	}()
	ev := <-ec
	if ev == nil {
		fail("no identifier found")
	}
	return ev
}

type orderedObjects []*ast.Object

func (o orderedObjects) Less(i, j int) bool { return o[i].Name < o[j].Name }
func (o orderedObjects) Len() int           { return len(o) }
func (o orderedObjects) Swap(i, j int)      { o[i], o[j] = o[j], o[i] }

func done(obj *ast.Object, typ types.Type) {
	defer os.Exit(0)
	pos := types.FileSet.Position(types.DeclPos(obj))
	fmt.Printf("%v\n", pos)
	if typ.Kind == ast.Bad || !*tflag {
		return
	}
	fmt.Printf("%s\n", strings.Replace(typeStr(obj, typ), "\n", "\n\t", -1))
	if *aflag || *Aflag {
		var m orderedObjects
		for obj := range typ.Iter(types.DefaultImporter) {
			m = append(m, obj)
		}
		sort.Sort(m)
		for _, obj := range m {
			// Ignore unexported members unless Aflag is set.
			if !*Aflag && (typ.Pkg != "" || !ast.IsExported(obj.Name)) {
				continue
			}
			id := ast.NewIdent(obj.Name)
			id.Obj = obj
			_, mt := types.ExprType(id, types.DefaultImporter)
			fmt.Printf("\t%s\n", strings.Replace(typeStr(obj, mt), "\n", "\n\t\t", -1))
			fmt.Printf("\t\t%v\n", types.FileSet.Position(types.DeclPos(obj)))
		}
	}
}

func typeStr(obj *ast.Object, typ types.Type) string {
	switch typ.Kind {
	case ast.Fun, ast.Var:
		return fmt.Sprintf("%s %v", obj.Name, pretty{typ.Node})
	case ast.Pkg:
		return fmt.Sprintf("import (%s %s)", obj.Name, typ.Node.(*ast.ImportSpec).Path.Value)
	case ast.Con:
		return fmt.Sprintf("const %s %v", obj.Name, pretty{typ.Node})
	case ast.Lbl:
		return fmt.Sprintf("label %s", obj.Name)
	case ast.Typ:
		typ = typ.Underlying(false, types.DefaultImporter)
		return fmt.Sprintf("type %s %v", obj.Name, pretty{typ.Node})
	}
	return fmt.Sprintf("unknown %s %v", obj.Name, typ.Kind)
}

func parseExpr(s *ast.Scope, expr string) ast.Expr {
	n, err := parser.ParseExpr(types.FileSet, "<arg>", expr, s)
	if err != nil {
		fail("cannot parse expression: %v", err)
	}
	switch n := n.(type) {
	case *ast.Ident, *ast.SelectorExpr:
		return n
	}
	fail("no identifier found in expression")
	return nil
}

type FVisitor func(n ast.Node) bool

func (f FVisitor) Visit(n ast.Node) ast.Visitor {
	if f(n) {
		return f
	}
	return nil
}

var errNoPkgFiles = errors.New("no more package files found")

// parseLocalPackage reads and parses all go files from the
// current directory that implement the same package name
// the principal source file, except the original source file
// itself, which will already have been parsed.
//
func parseLocalPackage(filename string, src *ast.File, pkgScope *ast.Scope) (*ast.Package, error) {
	pkg := &ast.Package{src.Name.Name, pkgScope, nil, map[string]*ast.File{filename: src}}
	d, f := filepath.Split(filename)
	if d == "" {
		d = "./"
	}
	fd, err := os.Open(d)
	if err != nil {
		return nil, errNoPkgFiles
	}
	defer fd.Close()

	list, err := fd.Readdirnames(-1)
	if err != nil {
		return nil, errNoPkgFiles
	}

	for _, pf := range list {
		file := filepath.Join(d, pf)
		if !strings.HasSuffix(pf, ".go") ||
			pf == f ||
			pkgName(file) != pkg.Name {
			continue
		}
		src, err := parser.ParseFile(types.FileSet, file, nil, 0, pkg.Scope)
		if err == nil {
			pkg.Files[file] = src
		}
	}
	if len(pkg.Files) == 1 {
		return nil, errNoPkgFiles
	}
	return pkg, nil
}

// pkgName returns the package name implemented by the
// go source filename.
//
func pkgName(filename string) string {
	prog, _ := parser.ParseFile(types.FileSet, filename, nil, parser.PackageClauseOnly, nil)
	if prog != nil {
		return prog.Name.Name
	}
	return ""
}

func hasSuffix(s, suff string) bool {
	return len(s) >= len(suff) && s[len(s)-len(suff):] == suff
}

type pretty struct {
	n interface{}
}

func (p pretty) String() string {
	var b bytes.Buffer
	printer.Fprint(&b, types.FileSet, p.n)
	return b.String()
}
