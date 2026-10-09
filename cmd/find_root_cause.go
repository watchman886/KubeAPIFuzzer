package cmd

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"k-fuzz/values"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/tools/cover"
	"golang.org/x/tools/go/packages"
)

var (
	coverPath string
	modRoot   string
	start     string
)

var findRootCause = &cobra.Command{
	Use:   "find-root-cause [file]",
	Short: "Replay requests that caused 500 responses, then output filtered call stacks",
	RunE: func(cmd *cobra.Command, args []string) error {

		profiles, err := parseCover(coverPath)
		if err != nil {
			logrus.Fatalf("parse cover: %v", err)
		}

		pkgs, fset, err := loadPackages(modRoot, nil)
		if err != nil {
			logrus.Fatalf("load packages: %v", err)
		}
		fmt.Printf("Loaded %d packages\n", len(pkgs))

		execByFile := normalizeProfilesToAbs(profiles, pkgs, fset)

		funcs, err := mapExecutedFuncs(pkgs, fset, execByFile)
		if err != nil {
			logrus.Fatalf("map executed funcs: %v", err)
		}
		fmt.Printf("Discovered %d functions in packages\n", len(funcs))

		// Build call graph
		err = buildCallGraph(funcs)
		if err != nil {
			logrus.Fatalf("build call graph: %v", err)
		}

		// Analyze error return points
		err = analyzeErrorReturns(funcs)
		if err != nil {
			logrus.Fatalf("analyze error returns: %v", err)
		}

		// Find root causes starting from specified function
		paths := findErrorPaths(funcs, start)

		// Output results
		fmt.Printf("\nFound %d error propagation paths:\n", len(paths))
		for i, path := range paths {
			fmt.Printf("\nPath %d:\n", i+1)
			for j, funcKey := range path {
				fmt.Printf("  %d. %s\n", j+1, funcKey)
			}
		}

		return nil
	},
}

func init() {
	findRootCause.Flags().StringVarP(&coverPath, values.TextfmtCoverageFilePath, "p", "", "path to the textfmt coverage file")
	findRootCause.Flags().StringVarP(&modRoot, values.KubernetesRootDir, "k", "", "path to the kubernetes root directory")
	findRootCause.Flags().StringVarP(&start, values.StartFunctionKey, "s", "", "function name to start search, eg: 'k8s.io/apimachinery/pkg/runtime/serializer/versioning::Decode'")

	if err := findRootCause.MarkFlagRequired(values.TextfmtCoverageFilePath); err != nil {
		panic(err)
	}
	if err := findRootCause.MarkFlagRequired(values.KubernetesRootDir); err != nil {
		panic(err)
	}
	if err := findRootCause.MarkFlagRequired(values.StartFunctionKey); err != nil {
		panic(err)
	}
	rootCmd.AddCommand(findRootCause)
}

type FuncID struct {
	PkgPath string
	Name    string
	Pos     token.Pos
	End     token.Pos
	File    string
	StartLn int
	EndLn   int
}

type FuncInfo struct {
	ID            FuncID
	Decl          *ast.FuncDecl
	Pkg           *packages.Package
	TypeObj       *types.Func
	Called        map[string]struct{} // set of callee FuncKey strings
	CalledFull    []*CallSite         // detailed call sites
	Exec          bool                // was executed per coverage
	CoveredRanges [][2]int            // covered line ranges [startLine, endLine]
	ErrReturns    []ErrReturnPoint
}

type CallSite struct {
	CalleeKey string
	CallExpr  ast.Node
	Pos       token.Pos
	File      string
	Line      int
}

type ErrReturnPoint struct {
	File     string
	Line     int
	Column   int
	ExprText string
	Kind     string // "nil", "call", "ident", "composite", "unknown", "fmtErrorf"
}

func funcKey(pkgPath, name string) string { return pkgPath + "::" + name }

func parseCover(profilePath string) ([]*cover.Profile, error) {
	f, err := os.Open(profilePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return cover.ParseProfilesFromReader(f)
}

func loadPackages(modRoot string, patterns []string) ([]*packages.Package, *token.FileSet, error) {
	// Check if modRoot is a directory containing multiple Go modules
	// by looking for subdirectories with go.mod files
	if len(patterns) == 0 {
		// Walk the directory to find all subdirectories with go.mod files
		var allPkgs []*packages.Package
		fset := token.NewFileSet()

		// Check if modRoot itself contains a go.mod file
		goModPath := filepath.Join(modRoot, "go.mod")
		if _, err := os.Stat(goModPath); err == nil {
			// modRoot is a single Go module, load it normally
			cfg := &packages.Config{
				Mode:  packages.LoadSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
				Dir:   modRoot,
				Env:   os.Environ(),
				Fset:  fset,
				Tests: false,
			}
			pkgs, err := packages.Load(cfg, "./...")
			if err != nil {
				return nil, nil, err
			}
			return pkgs, fset, nil
		}

		// modRoot is a directory containing multiple Go modules
		// Walk the directory to find all subdirectories with go.mod files
		err := filepath.Walk(modRoot, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			logrus.Debugf("Parsing package: %s", path)
			if info.IsDir() && path != modRoot {
				// Check if this directory contains a go.mod file
				goModPath := filepath.Join(path, "go.mod")
				if _, err := os.Stat(goModPath); err == nil {
					// This is a Go module directory, load it
					cfg := &packages.Config{
						Mode:  packages.LoadSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
						Dir:   path,
						Env:   os.Environ(),
						Fset:  fset,
						Tests: false,
					}
					pkgs, err := packages.Load(cfg, "./...")
					if err != nil {
						return err
					}
					allPkgs = append(allPkgs, pkgs...)
					// Skip walking subdirectories of this module
					return filepath.SkipDir
				}
			}
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
		return allPkgs, fset, nil
	} else {
		// Use provided patterns
		cfg := &packages.Config{
			Mode:  packages.LoadSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
			Dir:   modRoot,
			Env:   os.Environ(),
			Fset:  token.NewFileSet(),
			Tests: false,
		}
		pkgs, err := packages.Load(cfg, patterns...)
		if err != nil {
			return nil, nil, err
		}
		return pkgs, cfg.Fset, nil
	}
}

// Map coverage executed ranges to functions using token positions.
func mapExecutedFuncs(pkgs []*packages.Package, fset *token.FileSet, execByFile map[string][][2]int) (map[string]*FuncInfo, error) {

	// helper to test if a [s,e] intersects any executed block
	intersects := func(absFilename string, sline, eline int) bool {
		items := execByFile[filepath.Clean(absFilename)]
		for _, b := range items {
			if !(eline < b[0] || sline > b[1]) {
				return true
			}
		}
		return false
	}

	// helper to get covered ranges that intersect with function
	getCoveredRanges := func(absFilename string, sline, eline int) [][2]int {
		var ranges [][2]int
		items := execByFile[filepath.Clean(absFilename)]
		for _, b := range items {
			// Check if ranges intersect
			if !(eline < b[0] || sline > b[1]) {
				// Add the intersecting part
				start := b[0]
				if start < sline {
					start = sline
				}
				end := b[1]
				if end > eline {
					end = eline
				}
				ranges = append(ranges, [2]int{start, end})
			}
		}
		return ranges
	}

	funcs := map[string]*FuncInfo{}

	for _, pkg := range pkgs {
		for fiIdx, file := range pkg.Syntax {
			filename := filepath.Clean(fset.Position(file.Pos()).Filename)
			_ = fiIdx
			// find FuncDecls
			for _, decl := range file.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				start := fset.Position(fd.Pos()).Line
				end := fset.Position(fd.End()).Line
				exec := intersects(filename, start, end)

				if !exec {
					continue
				}

				var typObj *types.Func
				if obj := pkg.TypesInfo.Defs[fd.Name]; obj != nil {
					if tf, ok := obj.(*types.Func); ok {
						typObj = tf
					}
				}
				pkgPath := pkg.PkgPath
				key := funcKey(pkgPath, fd.Name.Name)
				coveredRanges := getCoveredRanges(filename, start, end)
				fi := &FuncInfo{
					ID: FuncID{
						PkgPath: pkgPath,
						Name:    fd.Name.Name,
						Pos:     fd.Pos(),
						End:     fd.End(),
						File:    filename,
						StartLn: start,
						EndLn:   end,
					},
					Decl:          fd,
					Pkg:           pkg,
					TypeObj:       typObj,
					Called:        map[string]struct{}{},
					Exec:          exec,
					CoveredRanges: coveredRanges,
				}
				funcs[key] = fi
			}
		}
	}
	return funcs, nil
}

// buildCallGraph builds the call graph by analyzing function calls within each function body
func buildCallGraph(funcs map[string]*FuncInfo) error {
	for _, fi := range funcs {
		if fi.Decl == nil || fi.Decl.Body == nil {
			continue
		}

		// Walk the function body to find function calls
		ast.Inspect(fi.Decl.Body, func(n ast.Node) bool {
			if callExpr, ok := n.(*ast.CallExpr); ok {
				// Check if the call expression line is covered
				callLine := fi.Pkg.Fset.Position(callExpr.Pos()).Line
				if !isLineCovered(fi, callLine) {
					return true // Skip uncovered calls
				}

				// Try to resolve the called function
				if sel, ok := callExpr.Fun.(*ast.SelectorExpr); ok {
					// Method call or package function
					if ident, ok := sel.X.(*ast.Ident); ok && ident.Obj != nil {
						// Local variable method call - skip for now
						return true
					}

					selName := sel.Sel.Name
					for calleeKey := range funcs {
						// FIXME: this is a simplification that may lead to false positives
						// in case of multiple packages having functions with the same name
						// e.g., pkg1.FuncA and pkg2.FuncA
						// A more robust approach would involve type analysis
						parts := strings.Split(calleeKey, "::")
						if len(parts) == 2 && parts[1] == selName {
							// if fi.called already has calleeKey, skip
							if _, exists := fi.Called[calleeKey]; exists {
								continue
							}

							// Record the call
							fi.Called[calleeKey] = struct{}{}

							// Also record detailed call site info
							pos := fi.Pkg.Fset.Position(callExpr.Pos())
							fi.CalledFull = append(fi.CalledFull, &CallSite{
								CalleeKey: calleeKey,
								CallExpr:  callExpr,
								Pos:       callExpr.Pos(),
								File:      pos.Filename,
								Line:      pos.Line,
							})
						}
					}
				} else if ident, ok := callExpr.Fun.(*ast.Ident); ok {
					// Direct function call
					if fi.Pkg != nil && fi.Pkg.TypesInfo != nil {
						if obj := fi.Pkg.TypesInfo.Uses[ident]; obj != nil {
							if funcObj, ok := obj.(*types.Func); ok {
								// Found a function call
								pkgPath := funcObj.Pkg().Path()
								funcName := funcObj.Name()
								calleeKey := funcKey(pkgPath, funcName)

								// Only record the call if the callee function is also covered
								if calleeFi, exists := funcs[calleeKey]; exists && calleeFi.Exec {
									// Record the call
									fi.Called[calleeKey] = struct{}{}

									// Also record detailed call site info
									pos := fi.Pkg.Fset.Position(callExpr.Pos())
									fi.CalledFull = append(fi.CalledFull, &CallSite{
										CalleeKey: calleeKey,
										CallExpr:  callExpr,
										Pos:       callExpr.Pos(),
										File:      pos.Filename,
										Line:      pos.Line,
									})
								}
							}
						}
					}
				}
			}
			return true
		})
	}
	return nil
}

// analyzeErrorReturns analyzes each function to find error return points
func analyzeErrorReturns(funcs map[string]*FuncInfo) error {
	for _, fi := range funcs {
		if fi.Decl == nil || fi.Decl.Body == nil {
			continue
		}

		// Check if function returns error
		returnsError := false
		if fi.TypeObj != nil {
			sig := fi.TypeObj.Type().(*types.Signature)
			results := sig.Results()
			for i := 0; i < results.Len(); i++ {
				if isErrorType(results.At(i).Type()) {
					returnsError = true
					break
				}
			}
		}

		if !returnsError {
			continue
		}

		// Find return statements that return errors
		ast.Inspect(fi.Decl.Body, func(n ast.Node) bool {
			if retStmt, ok := n.(*ast.ReturnStmt); ok {
				pos := fi.Pkg.Fset.Position(retStmt.Pos())

				// Check if the return statement line is covered
				if !isLineCovered(fi, pos.Line) {
					return true // Skip uncovered return statements
				}

				// Analyze each return expression
				for _, expr := range retStmt.Results {
					if isErrorType(fi.Pkg.TypesInfo.TypeOf(expr)) {
						// This return statement returns an error
						errReturn := ErrReturnPoint{
							File:     pos.Filename,
							Line:     pos.Line,
							Column:   pos.Column,
							ExprText: exprToString(expr),
							Kind:     classifyErrorExpr(expr),
						}
						fi.ErrReturns = append(fi.ErrReturns, errReturn)
					}
				}
			}
			return true
		})
	}
	return nil
}

// isErrorType checks if a type is or implements the error interface
func isErrorType(t types.Type) bool {
	if t == nil {
		return false
	}

	// Check if it's the built-in error type
	if named, ok := t.(*types.Named); ok {
		if named.Obj().Name() == "error" && named.Obj().Pkg() == nil {
			return true
		}
	}

	// Check if it implements the error interface
	if implementsError(t) {
		return true
	}

	return false
}

// implementsError checks if a type implements the error interface
func implementsError(t types.Type) bool {
	errorType := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	return types.Implements(t, errorType) || types.Implements(types.NewPointer(t), errorType)
}

// exprToString converts an AST expression to a string representation
func exprToString(expr ast.Expr) string {
	// Simple implementation - in production you'd want a more robust version
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.CallExpr:
		if ident, ok := e.Fun.(*ast.Ident); ok {
			return ident.Name + "(...)"
		}
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
			if x, ok := sel.X.(*ast.Ident); ok {
				return x.Name + "." + sel.Sel.Name + "(...)"
			}
		}
		return "call(...)"
	case *ast.BasicLit:
		return e.Value
	default:
		return "expr"
	}
}

// classifyErrorExpr classifies the type of error expression
func classifyErrorExpr(expr ast.Expr) string {
	switch expr.(type) {
	case *ast.Ident:
		return "ident"
	case *ast.CallExpr:
		if call, ok := expr.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				if sel.Sel.Name == "Errorf" {
					if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "fmt" {
						return "fmtErrorf"
					}
				}
			}
			return "call"
		}
	case *ast.CompositeLit:
		return "composite"
	case *ast.BasicLit:
		return "nil" // assuming nil literal
	}
	return "unknown"
}

// findErrorPaths performs DFS traversal to find error propagation paths
func findErrorPaths(funcs map[string]*FuncInfo, startFunc string) [][]string {
	var paths [][]string
	visited := make(map[string]bool)

	if _, exists := funcs[startFunc]; !exists {
		logrus.Printf("Warning: start function %s not found in executed functions", startFunc)
		return paths
	}

	var dfs func(stack []string, current string)
	dfs = func(stack []string, current string) {
		// Check for cycles
		if visited[current] {
			return
		}
		visited[current] = true
		defer func() { visited[current] = false }()

		fi, exists := funcs[current]
		if !exists {
			return
		}

		// Add current function to stack
		stack = append(stack, current)

		// Check if this function has error returns that are likely non-nil
		if fi.Exec && len(fi.ErrReturns) > 0 {
			for _, er := range fi.ErrReturns {
				if er.Kind == "call" || er.Kind == "fmtErrorf" || er.Kind == "unknown" || er.Kind == "ident" {
					// Record path including this function
					cp := make([]string, len(stack))
					copy(cp, stack)
					paths = append(paths, cp)
					// Continue exploring callees too (there may be deeper err)
					break
				}
			}
		}

		// Continue DFS to callees
		for callee := range fi.Called {
			if calleeFi, exists := funcs[callee]; exists && calleeFi.Exec {
				// Only traverse to functions that are executed and may return errors
				if calleeFi.TypeObj != nil {
					sig := calleeFi.TypeObj.Type().(*types.Signature)
					results := sig.Results()
					for i := 0; i < results.Len(); i++ {
						if isErrorType(results.At(i).Type()) {
							dfs(stack, callee)
							break
						}
					}
				}
			}
		}
	}

	dfs([]string{}, startFunc)
	return paths
}

// normalizeProfilesToAbs attempts to map profile file names to absolute file paths
// observed in the loaded packages (via fset). Returns a map absPath -> list of blocks [start,end].
func normalizeProfilesToAbs(profiles []*cover.Profile, pkgs []*packages.Package, fset *token.FileSet) map[string][][2]int {
	// collect all absolute source files from loaded pkgs
	absFiles := map[string]struct{}{}
	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			fn := fset.Position(file.Pos()).Filename
			absFiles[filepath.Clean(fn)] = struct{}{}
		}
	}
	// make a slice for easier iteration
	absList := make([]string, 0, len(absFiles))
	for fn := range absFiles {
		absList = append(absList, fn)
	}
	// build mapping
	result := map[string][][2]int{}
	for _, p := range profiles {
		for _, block := range p.Blocks {
			if block.NumStmt == 0 || block.Count == 0 {
				continue
			}
			profName := filepath.Clean(p.FileName) // profile path form (module-like or relative)
			// try exact match first (maybe profile already contains an absolute or relative path matching one file)
			found := ""
			for _, af := range absList {
				if af == profName {
					found = af
					break
				}
			}
			if found == "" {
				// try suffix match: choose candidate where abs endsWith profName
				candidates := []string{}
				for _, af := range absList {
					if strings.HasSuffix(af, profName) {
						candidates = append(candidates, af)
					}
				}
				if len(candidates) == 1 {
					found = candidates[0]
				} else if len(candidates) > 1 {
					panic("multiple candidates found for profile file: " + profName)
					// prefer exact file name match (basename)
					base := filepath.Base(profName)
					for _, c := range candidates {
						if filepath.Base(c) == base {
							found = c
							break
						}
					}
					// otherwise choose the shortest candidate path (heuristic)
					if found == "" {
						sort.Slice(candidates, func(i, j int) bool {
							return len(candidates[i]) < len(candidates[j])
						})
						found = candidates[0]
					}
				}
			}
			//if found == "" {
			//	// fallback: try to match by basename only (less precise)
			//	base := filepath.Base(profName)
			//	for _, af := range absList {
			//		if filepath.Base(af) == base {
			//			found = af
			//			break
			//		}
			//	}
			//}
			if found == "" {
				// cannot find mapping; skip but log for debugging
				// (do not crash — continue)
				// NOTE: in production code you may want to record these for manual inspection
				continue
			}

			result[found] = append(result[found], [2]int{block.StartLine, block.EndLine})
		}

	}
	return result
}

// Helper function to check if a line is covered in a function
func isLineCovered(fi *FuncInfo, line int) bool {
	for _, r := range fi.CoveredRanges {
		if line >= r[0] && line <= r[1] {
			return true
		}
	}
	return false
}
