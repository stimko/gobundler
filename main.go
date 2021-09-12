package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/printer"
	"go/token"
	"go/types"
	"io/ioutil"
	"log"
	"os"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// optional arguments passed into command line
var (
	fileName    = flag.String("fileName", "plugin.go", "name of output file")
	packageName = flag.String("packageName", "main", "name of output package")
	rootModule  = flag.String("rootModule", "{your root module here}", "name of root module")
	rootPath    = flag.String("destinationRoot", "build", "name of root output folder")
)

func main() {
	flag.Usage = usage
	flag.Parse()
	args := flag.Args()
	if len(args) != 2 {
		usage()
		os.Exit(2)
	}

	wd, _ := os.Getwd()
	targetPackageName := args[1]

	var importsOut bytes.Buffer
	importsOut = handlePackageName(importsOut, *packageName)
	codeOut, pkgStd := traverseDependencyTree(args[0], wd)
	importsOut = handleImports(importsOut, pkgStd)

	// combine bytes from imports and code
	importsOut.Write(codeOut.Bytes())
	// Now format the entire thing.
	result, err := format.Source(importsOut.Bytes())
	if err != nil {
		log.Fatalf("formatting failed: %v", err)
	}

	buildPath := fmt.Sprintf("%s/%s/%s", *rootPath, targetPackageName, *fileName)

	// file permissions for the output file: equivalent to drw-rw-rw
	const permissions os.FileMode = 0666

	ioutil.WriteFile(buildPath, result, permissions)
}

func sortedImports(imports map[string]*packages.Package) []string {
	var keys []string
	for k := range imports {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}

// traverseDependencyTree - recursively loads imports of a package starting with a root package and then handles each type of import accordingly
// marks the usage of the import in the ast of the package and writes to the output buffer
// returns the output buffer and standard packages that were found in the traversal
func traverseDependencyTree(pkgName string, wd string) (bytes.Buffer, map[string]bool) {

	var (
		pkgStd  = make(map[string]bool)
		pkgExt  = make(map[string]bool)
		codeOut bytes.Buffer
	)

	var recurseDependencyTree func(pkg *packages.Package)
	recurseDependencyTree = func(pkg *packages.Package) {

		imports := sortedImports(pkg.Imports)

		for _, imp := range imports {
			if isStandardImportPath(imp) || isTorbitDependency(imp) {
				pkgStd[imp] = true
			} else if isSharedDependency(imp) || isVendorDependency(imp) {
				if !pkgExt[imp] {
					depPkg := loadFilesOfPackage(imp)
					modifyEachFileAST(pkg, imp, depPkg.Name+"_")
					objs := getObjectsToUpdate(depPkg)
					renamePackageDeclarations(depPkg, objs, depPkg.Name+"_")
					recurseDependencyTree(depPkg)
				}
			}
		}
		pkgExt[pkgName] = true
		codeOut = writeFilesToOutput(pkg, codeOut)
	}

	pkg := loadFilesOfPackage(pkgName)
	recurseDependencyTree(pkg)

	return codeOut, pkgStd
}

// handlePackageName - writes the package name to output buffer
func handlePackageName(importsOut bytes.Buffer, name string) bytes.Buffer {
	fmt.Fprintf(&importsOut, "package %s\n\n", name)
	return importsOut
}

func renamePackageDeclarations(packageInfo *packages.Package, objsToUpdate map[types.Object]bool, prefix string) {
	for id, obj := range packageInfo.TypesInfo.Defs {
		if objsToUpdate[obj] {
			id.Name = prefix + obj.Name()
		}
	}
	for id, obj := range packageInfo.TypesInfo.Uses {
		if objsToUpdate[obj] {
			id.Name = prefix + obj.Name()
		}
	}
}

// modifyEachFileAST - iterates through each file syntax and modifies the ast where it finds a usage of the used package
func modifyEachFileAST(packageInfo *packages.Package, usedPackage string, prefix string) {
	for _, f := range packageInfo.Syntax {
		// For each qualified identifier that refers to the
		// destination package, remove the qualifier.
		// The "@@@." strings are removed in postprocessing.
		// and prefix the package declaration with the package name 'package_{declaration}'
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok {
					if obj, ok := packageInfo.TypesInfo.Uses[id].(*types.PkgName); ok {
						if obj.Imported().Path() == usedPackage {
							sel.Sel.Name = prefix + sel.Sel.Name
							id.Name = "@@@"
						}
					}
				}
			}
			return true
		})
	}
}

// writesFilesToOutput - iterates through each file syntax and writes to the output buffer removing the marked package usages
func writeFilesToOutput(packageInfo *packages.Package, codeOut bytes.Buffer) bytes.Buffer {

	for _, f := range packageInfo.Syntax {

		last := f.Package
		if len(f.Imports) > 0 {
			imp := f.Imports[len(f.Imports)-1]
			last = imp.End()
			if imp.Comment != nil {
				if e := imp.Comment.End(); e > last {
					last = e
				}
			}
		}

		// Pretty-print package-level declarations.
		// but no package or import declarations.
		var buf bytes.Buffer
		for _, decl := range f.Decls {
			// skip imports
			if decl, ok := decl.(*ast.GenDecl); ok && decl.Tok == token.IMPORT {
				continue
			}

			beg, end := sourceRange(decl)

			printComments(&codeOut, f.Comments, last, beg)

			buf.Reset()
			format.Node(&buf, packageInfo.Fset, &printer.CommentedNode{Node: decl, Comments: f.Comments})
			// Remove each "@@@." in the output.
			// TODO not hygienic.
			codeOut.Write(bytes.Replace(buf.Bytes(), []byte("@@@."), nil, -1))

			last = printSameLineComment(&codeOut, f.Comments, packageInfo.Fset, end)

			codeOut.WriteString("\n\n")
		}

		printLastComments(&codeOut, f.Comments, last)
	}

	return codeOut
}

// getObjectsToUpdate - iterates through package info, recursively traverses ast node objects and returns them
func getObjectsToUpdate(packageInfo *packages.Package) map[types.Object]bool {

	objsToUpdate := make(map[types.Object]bool)
	var traverse func(from types.Object)
	traverse = func(from types.Object) {
		if !objsToUpdate[from] {
			objsToUpdate[from] = true
			// check if object is a type name
			// then iterate through and find uses of the object and then recurse
			if _, ok := from.(*types.TypeName); ok {
				for id, obj := range packageInfo.TypesInfo.Uses {
					if obj == from {
						if field := packageInfo.TypesInfo.Defs[id]; field != nil {
							traverse(field)
						}
					}
				}
			}

		}
	}

	scope := packageInfo.Types.Scope()
	for _, name := range scope.Names() {
		traverse(scope.Lookup(name))
	}

	return objsToUpdate
}

// loadFilesPackage - loads the package info with imports
func loadFilesOfPackage(src string) *packages.Package {
	const LoadNeeds = packages.NeedImports | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedDeps | packages.NeedName
	cfg := &packages.Config{Mode: LoadNeeds}

	pkgs, _ := packages.Load(cfg, src)

	// safe to assume that there will be one package loaded
	return pkgs[0]
}

// handleImports - writes imports to the output buffer
func handleImports(importsOut bytes.Buffer, pkgStd map[string]bool) bytes.Buffer {
	fmt.Fprintln(&importsOut, "import (")
	for p := range pkgStd {
		fmt.Fprintf(&importsOut, "\"%s\"\n", p)
	}
	fmt.Fprint(&importsOut, ")\n\n")

	return importsOut
}

func usage() {
	fmt.Fprintf(os.Stderr, "Must provide fully qualified package name (1) to use as starting package and target package name (2)")
}

func isSharedDependency(pkgName string) bool {
	return strings.HasPrefix(pkgName, *rootModule)
}

func isVendorDependency(pkgName string) bool {
	return strings.Contains(pkgName, "github.com")
}

func isStandardImportPath(path string) bool {
	i := strings.Index(path, "/")
	if i < 0 {
		i = len(path)
	}
	elem := path[:i]
	return !strings.Contains(elem, ".")
}

// sourceRange returns the [beg, end) interval of source code
// belonging to decl (incl. associated comments).
func sourceRange(decl ast.Decl) (beg, end token.Pos) {
	beg = decl.Pos()
	end = decl.End()

	var doc, com *ast.CommentGroup

	switch d := decl.(type) {
	case *ast.GenDecl:
		doc = d.Doc
		if len(d.Specs) > 0 {
			switch spec := d.Specs[len(d.Specs)-1].(type) {
			case *ast.ValueSpec:
				com = spec.Comment
			case *ast.TypeSpec:
				com = spec.Comment
			}
		}
	case *ast.FuncDecl:
		doc = d.Doc
	}

	if doc != nil {
		beg = doc.Pos()
	}
	if com != nil && com.End() > end {
		end = com.End()
	}

	return beg, end
}

func printComments(out *bytes.Buffer, comments []*ast.CommentGroup, pos, end token.Pos) {
	for _, cg := range comments {
		if pos <= cg.Pos() && cg.Pos() < end {
			for _, c := range cg.List {
				fmt.Fprintln(out, c.Text)
			}
			fmt.Fprintln(out)
		}
	}
}

const infinity = 1 << 30

func printLastComments(out *bytes.Buffer, comments []*ast.CommentGroup, pos token.Pos) {
	printComments(out, comments, pos, infinity)
}

func printSameLineComment(out *bytes.Buffer, comments []*ast.CommentGroup, fset *token.FileSet, pos token.Pos) token.Pos {
	tf := fset.File(pos)
	for _, cg := range comments {
		if pos <= cg.Pos() && tf.Line(cg.Pos()) == tf.Line(pos) {
			for _, c := range cg.List {
				fmt.Fprintln(out, c.Text)
			}
			return cg.End()
		}
	}
	return pos
}

type flagFunc func(string)

func (f flagFunc) Set(s string) error {
	f(s)
	return nil
}

func (f flagFunc) String() string { return "" }
