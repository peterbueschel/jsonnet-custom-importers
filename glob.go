package importer

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/dominikbraun/graph"
	"github.com/google/go-jsonnet"
	"github.com/spf13/afero"
	"go.uber.org/zap"
)

type (
	// GlobImporter can be used to allow import-paths with glob patterns inside.
	// Continuous imports are also possible and allow glob pattern in resolved
	// file/contents.
	// Activate the glob-import via the following prefixa in front of the import
	// path definition (see README file):
	//   - `glob.<?>://`, where <?> can be one of [path, file, dir, stem]
	//   - `glob.<?>+://`, where <?> can be one of [file, dir, stem]
	//   - `glob+://`
	//
	// For `glob.<?>://` all resolved files will stored under its
	// path, file(name), dir(name), stem (filename without extension). If multiple
	// files would fit for the file, dirs or stem, only the last one will be used.
	// Example:
	//  - Folders/files:
	//    - a.libsonnet
	//    - subfolder/a.libsonnet
	//  - Import path:
	//    - import 'glob.stem://**/*.libsonnet'
	//  - Result:
	//      {
	//        a: (import 'subfolder/a.libsonnet');
	//      }
	//
	GlobImporter struct {
		// JPaths stores extra search paths.
		JPaths []string
		// A FileSystem abstraction; useful for tests
		fs     afero.Fs
		logger *zap.Logger

		importGraph   graph.Graph[string, string]
		importCounter int

		// used in the CanHandle() and to store a possible alias.
		prefixa map[string]string
		aliases map[string]string
		// excludePattern is used in the GlobImporter to ignore files matching
		// the given pattern in '.gitIgnore' .
		excludePattern string
	}

	// orderedMap takes the glob.<?>:// and glob.<?>+:// results,
	// unifies them & keeps the order.
	orderedMap struct {
		items map[string][]string
		keys  []string
	}
	// hierachically sort the resolved files.
	hierachically []string
)

func (s hierachically) Len() int {
	return len(s)
}

func (s hierachically) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s hierachically) Less(i, j int) bool {
	s1 := strings.ReplaceAll(s[i], "/", "\x00")
	s2 := strings.ReplaceAll(s[j], "/", "\x00")

	return s1 < s2
}

// newOrderedMap initialize a new orderedMap.
func newOrderedMap() *orderedMap {
	return &orderedMap{
		items: make(map[string][]string),
		keys:  []string{},
	}
}

// add is used to either really add a single value to a key or with `extend == true`
// extend the list of values with the new value for the given key.
func (o *orderedMap) add(key, value string, extend bool) {
	item, exists := o.items[key]

	switch {
	case !exists:
		o.keys = append(o.keys, key)
		o.items[key] = []string{value}
	case extend:
		o.items[key] = append(item, value)
	case !extend:
		o.items[key] = []string{value}
	}
}

// NewGlobImporter returns a GlobImporter with default prefixa.
func NewGlobImporter(jpaths ...string) *GlobImporter {
	return &GlobImporter{
		prefixa: map[string]string{
			"glob.path":      "",
			"glob.path+":     "",
			"glob-str.path":  "",
			"glob-str.path+": "",
			"glob.file":      "",
			"glob.file+":     "",
			"glob-str.file":  "",
			"glob-str.file+": "",
			"glob.dir":       "",
			"glob.dir+":      "",
			"glob-str.dir":   "",
			"glob-str.dir+":  "",
			"glob.stem":      "",
			"glob.stem+":     "",
			"glob-str.stem":  "",
			"glob-str.stem+": "",
			"glob+":          "",
			"glob-str+":      "",
		},
		aliases:        make(map[string]string),
		logger:         zap.New(nil),
		JPaths:         jpaths,
		excludePattern: "",
		importGraph:    graph.New(graph.StringHash, graph.Tree(), graph.Directed(), graph.PreventCycles()),
		importCounter:  0,
		fs:             afero.NewOsFs(),
	}
}

func (g *GlobImporter) setImportGraph(importGraph graph.Graph[string, string], importCounter int) {
	g.importGraph = importGraph
	g.importCounter = importCounter
}

func (g *GlobImporter) Exclude(pattern string) {
	g.excludePattern = pattern
}

// AddAliasPrefix binds a given alias to a given prefix. This prefix must exist
// and only one alias per prefix is possible. An alias must have the suffix
// "://".
func (g *GlobImporter) AddAliasPrefix(alias, prefix string) error {
	if _, exists := g.prefixa[prefix]; !exists {
		return fmt.Errorf("%w '%s'", ErrUnknownPrefix, prefix)
	}

	g.prefixa[prefix] = alias
	g.aliases[alias] = prefix

	return nil
}

// Logger can be used to set the zap.Logger for the GlobImporter.
func (g *GlobImporter) Logger(logger *zap.Logger) {
	if logger != nil {
		g.logger = logger
	}
}

// CanHandle implements the interface method of the Importer and returns true,
// if the path has on of the supported prefixa. Run <Importer>.Prefixa() to get
// the supported prefixa.
func (g GlobImporter) CanHandle(path string) bool {
	for k, v := range g.prefixa {
		if strings.HasPrefix(path, k) || (strings.HasPrefix(path, v) && len(v) > 0) {
			return true
		}
	}

	return false
}

// Prefixa returns the list of supported prefixa for this importer.
func (g GlobImporter) Prefixa() []string {
	return append(stringKeysFromMap(g.prefixa), stringValuesFromMap(g.prefixa)...)
}

// Import implements the go-jsonnet iterface method and converts the resolved
// paths into readable paths for the original go-jsonnet FileImporter.
func (g *GlobImporter) Import(importedFrom, importedPath string) (jsonnet.Contents, string, error) {
	logger := g.logger.Named("GlobImporter")
	logger.Debug("Import()",
		zap.String("importedFrom", importedFrom),
		zap.String("importedPath", importedPath),
		zap.Strings("jpaths", g.JPaths),
	)

	contents := jsonnet.MakeContents("")

	// Hack-ish !!!:
	// The resolved glob-imports are still found inside the same file (importedFrom)
	// But the "foundAt" value is not allowed to be the same for multiple importer runs,
	// causing different contents.
	// Related:
	// - https://github.com/google/go-jsonnet/issues/349
	// - https://github.com/google/go-jsonnet/issues/374
	// - https://github.com/google/go-jsonnet/issues/329
	// So I have to put for example a simple self-reference './' in front of the "importedFrom" path
	// to fake the foundAt value. (tried multiple things, but even flushing the importerCache of
	// the VM via running vm.Importer(...) again, couldn't solve this)
	p := strings.Repeat("./", g.importCounter)
	foundAt := p + "./" + importedFrom

	prefix, pattern, err := g.parse(importedPath)
	if err != nil {
		return contents, foundAt, err
	}
	// this is the path of the import caller
	cwd, _ := filepath.Split(importedFrom)
	cwd = filepath.Clean(cwd)

	logger.Debug("parsed parameters from importedPath",
		zap.String("prefix", prefix),
		zap.String("pattern", pattern),
		zap.String("cwd", cwd),
	)
	// g.JPaths will be used first, before the cwd - this will give cwd higher
	// priority at the end.
	resolvedFiles, err := g.resolveFilesFrom(g.JPaths, cwd, pattern)
	if err != nil {
		return contents, foundAt, err
	}

	logger.Debug("glob library returns", zap.Strings("files", resolvedFiles))

	files := []string{}
	afiles := allowedFiles(resolvedFiles, importedFrom)
	basepath, _ := filepath.Split(importedFrom)

	if err := g.importGraph.AddVertex(importedPath,
		graph.VertexAttribute("shape", "rect"),
		graph.VertexAttribute("style", "dashed"),
		graph.VertexAttribute("color", "grey"),
		graph.VertexAttribute("fontcolor", "grey"),
	); err != nil {
		logger.Warn(err.Error())
	}

	for _, f := range afiles {
		relf, _ := filepath.Rel(basepath, f)
		files = append(files, relf)

		if err := g.importGraph.AddVertex(relf,
			graph.VertexAttribute("shape", "rect"),
			graph.VertexAttribute("color", "grey"),
			graph.VertexAttribute("fontcolor", "grey"),
			graph.VertexAttribute("style", "dashed"),
		); err != nil {
			logger.Warn(err.Error())
		}

		if err := g.importGraph.AddEdge(importedPath, relf,
			graph.EdgeAttribute("color", "grey"),
			graph.EdgeAttribute("style", "dashed"),
			graph.EdgeWeight(g.importCounter),
		); err != nil {
			logger.Warn(err.Error())
		}
	}

	joinedImports, err := g.handle(files, prefix)
	if err != nil {
		return contents, foundAt, err
	}

	contents = jsonnet.MakeContents(joinedImports)

	logger.Debug("returns", zap.String("contents", joinedImports), zap.String("foundAt", foundAt))

	return contents, foundAt, nil
}

// resolveFilesFrom takes a list of paths together with a glob pattern
// and returns the output of the used doublestar.Glob function.
func (g *GlobImporter) resolveFilesFrom(searchPaths []string, cwd, pattern string) ([]string, error) {
	executeGlob := func(dir, pattern string) (matches []string, err error) {
		pathPattern := filepath.Join(dir, pattern)
		pathPattern = filepath.Clean(pathPattern)
		pathPattern = filepath.ToSlash(pathPattern)
		base, file := doublestar.SplitPattern(pathPattern)

		fs, err := afero.NewIOFS(g.fs).Sub(base)
		if err != nil {
			return
		}

		if matches, err = doublestar.Glob(fs, file, doublestar.WithNoFollow(), doublestar.WithFailOnIOErrors()); err != nil {
			return
		}

		for i := range matches {
			matches[i] = filepath.FromSlash(path.Join(base, matches[i]))
		}

		return
	}

	resolvedFiles := []string{}

	for _, p := range searchPaths {
		matches, err := executeGlob(p, pattern)
		if err != nil {
			return []string{}, err
		}

		resolvedFiles = append(resolvedFiles, matches...)
	}
	// sort the JPaths results first
	sort.Sort(hierachically(resolvedFiles))

	// CWD must be last in resolvedFiles
	matches, err := executeGlob(cwd, pattern)
	if err != nil {
		return []string{}, err
	}

	sort.Sort(hierachically(matches))
	resolvedFiles = append(resolvedFiles, matches...)

	if len(resolvedFiles) == 0 {
		return []string{},
			fmt.Errorf("%w for the glob pattern '%s'", ErrEmptyResult, pattern)
	}
	// handle excludes
	if len(g.excludePattern) > 0 {
		return g.removeExcludesFrom(resolvedFiles, pattern)
	}

	return resolvedFiles, nil
}

func (g *GlobImporter) removeExcludesFrom(files []string, pattern string) ([]string, error) {
	keep := []string{}

	for _, file := range files {
		match, err := doublestar.PathMatch(g.excludePattern, file)
		if err != nil {
			return []string{}, fmt.Errorf("while remove excluded file %s ,error: %w", file, err)
		}

		if !match {
			keep = append(keep, file)
		}
	}

	if len(keep) == 0 {
		return []string{},
			fmt.Errorf(
				"%w, exclude pattern '%s' removed all matches for the glob pattern '%s'",
				ErrEmptyResult, g.excludePattern, pattern)
	}

	return keep, nil
}

func (g *GlobImporter) parse(importedPath string) (string, string, error) {
	parsedURL, err := url.Parse(importedPath)
	if err != nil {
		return "", "",
			fmt.Errorf("%w: cannot parse import '%s', error: %s",
				ErrMalformedGlobPattern, importedPath, err)
	}

	prefix := parsedURL.Scheme
	pattern := strings.Join([]string{parsedURL.Host, parsedURL.Path}, "/")

	query, err := url.ParseQuery(parsedURL.RawQuery)
	if err != nil {
		return "", "",
			fmt.Errorf("%w: cannot parse the query inside the import '%s', error: %s",
				ErrMalformedGlobPattern, importedPath, err)
	}

	if excludePattern, exists := query["exclude"]; exists {
		g.excludePattern = excludePattern[0]
	}

	return prefix, pattern, nil
}

// allowedFiles removes ignoreFile from a given list of files and
// converts the rest via filepath.FromSlash().
// Used to remove self reference of a file to avoid endless loops.
func allowedFiles(files []string, ignoreFile string) []string {
	allowedFiles := []string{}

	for _, file := range files {
		if file == ignoreFile {
			continue
		}

		importPath := filepath.FromSlash(file)
		allowedFiles = append(allowedFiles, importPath)
	}

	return allowedFiles
}

// handle runs the logic behind the different glob prefixa and returns based on
// the prefix the import string.
func (g GlobImporter) handle(files []string, prefix string) (string, error) {
	resolvedFiles := newOrderedMap()

	// handle import or importstr
	importKind := "import"

	if strings.HasPrefix(prefix, "glob-str") {
		prefix = strings.Replace(prefix, "glob-str", "glob", 1)
		importKind += "str"
	}

	// handle alias prefix
	if p, exists := g.aliases[prefix]; exists {
		prefix = p
	}

	switch prefix {
	case "glob+":
		imports := make([]string, 0, len(files))

		for _, f := range files {
			i := fmt.Sprintf("(%s '%s')", importKind, f)
			imports = append(imports, i)
		}

		return strings.Join(imports, "+"), nil
	case "glob.path", "glob.path+":
		imports := make([]string, 0, len(files))

		for _, f := range files {
			imports = append(imports, fmt.Sprintf("'%s': (%s '%s'),", f, importKind, f))
		}

		return fmt.Sprintf("{\n%s\n}", strings.Join(imports, "\n")), nil
	case "glob.stem", "glob.stem+":
		for _, f := range files {
			i := fmt.Sprintf("(%s '%s')", importKind, f)
			_, filename := filepath.Split(f)
			stem, _, _ := strings.Cut(filename, ".")
			resolvedFiles.add(stem, i, strings.HasSuffix(prefix, "+"))
		}
	case "glob.file", "glob.file+":
		for _, f := range files {
			i := fmt.Sprintf("(%s '%s')", importKind, f)
			_, filename := filepath.Split(f)
			resolvedFiles.add(filename, i, strings.HasSuffix(prefix, "+"))
		}
	case "glob.dir", "glob.dir+":
		for _, f := range files {
			i := fmt.Sprintf("(%s '%s')", importKind, f)
			dir, _ := filepath.Split(f)
			resolvedFiles.add(dir, i, strings.HasSuffix(prefix, "+"))
		}
	default:
		return "", fmt.Errorf("%w: %s", ErrUnknownPrefix, prefix)
	}

	return createGlobDotImportsFrom(resolvedFiles), nil
}

// createGlobDotImportsFrom transforms the orderedMap of resolvedFiles
// into the format `{ '<?>': import '...' }`.
func createGlobDotImportsFrom(resolvedFiles *orderedMap) string {
	var out strings.Builder

	out.WriteString("{\n")

	for _, k := range resolvedFiles.keys {
		fmt.Fprintf(&out, "'%s': %s,\n", k, strings.Join(resolvedFiles.items[k], "+"))
	}

	out.WriteString("}")

	return out.String()
}
