package importer

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/google/go-jsonnet"
	"go.uber.org/zap"
)

var (
	excludeSeparator = "!"
)

type (
	// globCacheKey is used for the globCache and helps also to identify
	// "import cycles".
	globCacheKey struct {
		importedFrom string
		importedPath string
	}

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

		logger    *zap.Logger
		separator string
		// used in the CanHandle() and to store a possible alias.
		prefixa map[string]string
		aliases map[string]string
		// lastFiles holds the last resolved files to enrich the import cycle
		// error output and to set them as ignoreFiles in the go-glob options.
		lastFiles []string
		// when this cache get hit, a caller import uses the same import path
		// inside the same filepath. Which means, there is an import cycle.
		// A cycle ends up in a "max stack frames exceeded" and is tolerated.
		// ( see also: https://github.com/google/go-jsonnet/issues/353 )
		cycleCache map[globCacheKey]struct{}
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
)

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

// NewGlobImporter returns a GlobImporter with a no-op logger, an initialized
// cycleCache and the default prefixa.
func NewGlobImporter(jpaths ...string) *GlobImporter {
	return &GlobImporter{
		separator: "://",
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
		aliases:    make(map[string]string),
		logger:     zap.New(nil),
		cycleCache: make(map[globCacheKey]struct{}),
		lastFiles:  []string{},
		JPaths:     jpaths,
	}
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
	p := strings.Repeat("./", len(g.cycleCache))
	foundAt := p + "./" + importedFrom

	cacheKey := globCacheKey{importedFrom, importedPath}
	if _, exists := g.cycleCache[cacheKey]; !exists {
		fmt.Printf("g.cycleCache = %+#v\n", g.cycleCache)
	}

	if _, exists := g.cycleCache[cacheKey]; exists {
		return contents, "",
			fmt.Errorf(
				"%w for import path '%s' in '%s'. Possible cycle in [%s]",
				ErrImportCycle,
				importedPath, importedFrom, strings.Join(g.lastFiles, " <-> "),
			)
	}
	// Add everything to the cache at the end
	defer func() {
		g.cycleCache[cacheKey] = struct{}{}
	}()
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

	resolvedFiles, err := g.resolveFilesFrom(append([]string{cwd}, g.JPaths...), pattern)
	if err != nil {
		return contents, foundAt, err
	}

	logger.Debug("glob library returns", zap.Strings("files", resolvedFiles))

	g.lastFiles = resolvedFiles

	files := []string{}
	afiles := allowedFiles(resolvedFiles, importedFrom)

	basepath, _ := filepath.Split(importedFrom)
	for _, f := range afiles {
		rf, _ := filepath.Rel(basepath, f)
		files = append(files, rf)
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
// and returns the output of the used go-glob.Glob function.
// Each file in every searchpath will be returned and not just the first
// one, which will be found in one of the search paths.
func (g *GlobImporter) resolveFilesFrom(searchPaths []string, pattern string) ([]string, error) {
	resolvedFiles := []string{}

	for _, p := range searchPaths {
		files, err := doublestar.FilepathGlob(filepath.Join(p, pattern))
		if err != nil {
			return []string{}, err
		}
		resolvedFiles = append(resolvedFiles, files...)
	}

	if len(resolvedFiles) == 0 {
		return []string{},
			fmt.Errorf("%w for the glob pattern '%s'", ErrEmptyResult, pattern)
	}
	sort.Strings(resolvedFiles)
	// handle excludes
	if len(g.excludePattern) > 0 {
		return g.removeExcludesFrom(resolvedFiles)
	}
	return resolvedFiles, nil

}

func (g *GlobImporter) removeExcludesFrom(files []string) ([]string, error) {
	keep := []string{}
	for _, f := range files {
		match, err := doublestar.PathMatch(g.excludePattern, f)
		if err != nil {
			return []string{}, err
		}
		if !match {
			keep = append(keep, f)
		}
	}
	return keep, nil
}

func (g *GlobImporter) parse(importedPath string) (string, string, error) {
	globPrefix, pattern, found := strings.Cut(importedPath, g.separator)
	if !found {
		return "", "",
			fmt.Errorf("%w: missing separator '%s' in import path: %s",
				ErrMalformedGlobPattern, g.separator, importedPath)
	}
	// handle excludePattern, if exists
	prefix, excludePattern, _ := strings.Cut(globPrefix, excludeSeparator)
	g.excludePattern = excludePattern

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
	if strings.HasPrefix(prefix, "-str") {
		prefix = strings.TrimPrefix(prefix, "-str")
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

	out.WriteString("\n}")

	return out.String()
}
