package importer

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/dominikbraun/graph"
	"github.com/google/go-jsonnet"
	"github.com/spf13/afero"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

const (
	yamlPrefix         = "yaml"
	yamlScheme         = yamlPrefix + "://"
	defaultYamlImports = "imports"
	// defaultYamlWrapper merges the resolved imports (base) with the document,
	// without the imports key itself.
	defaultYamlWrapper = "function(base, doc, file, refs={}) base + { [k]: doc[k] for k in std.objectFields(doc) if k != %s }"
)

type (
	// YamlImporter resolves imports with the prefix `yaml://` and returns the
	// parsed YAML file as jsonnet code via std.parseYaml(importstr '<file>').
	//
	// A YAML file can list other files under a configurable key (default
	// `imports`). Entries ending with .yaml or .yml are imported via `yaml://`,
	// all other entries stay normal jsonnet imports. For such files the code
	// looks like:
	//
	//	(<wrapper>)(
	//	  base = (import 'a.libsonnet') + (import 'yaml://b.yaml'),
	//	  doc = std.parseYaml(importstr 'file.yaml'),
	//	  file = '/project/file.yaml',
	//	)
	//
	// The wrapper is a jsonnet function expression with the parameters base,
	// doc and file. With FileRefs it also gets the parameter refs.
	YamlImporter struct {
		// JPaths stores extra search paths.
		JPaths []string

		fs      afero.Fs
		logger  *zap.Logger
		key     string
		wrapper string
		// entryWrapper wraps the jsonnet entries of `imports`.
		entryWrapper string
		// refPath locates file names in the document, e.g. project.components[].file.
		refPath string

		// deps tracks which file imports which, to detect cycles.
		deps graph.Graph[string, string]
		// files maps the foundAt values to the resolved files.
		files         map[string]string
		contents      map[string]jsonnet.Contents
		importCounter int
		// shared is the import graph of the MultiImporter.
		shared graph.Graph[string, string]
		// ignoreCycles follows the option of the MultiImporter.
		ignoreCycles bool
	}
)

// NewYamlImporter returns a YamlImporter with the default imports key
// `imports` and the default wrapper.
func NewYamlImporter(jpaths ...string) *YamlImporter {
	y := &YamlImporter{
		JPaths: jpaths,
		fs:     afero.NewOsFs(),
		logger: zap.New(nil),
		key:    defaultYamlImports,
	}
	y.reset()

	return y
}

func (y *YamlImporter) reset() {
	y.deps = graph.New(graph.StringHash, graph.Directed())
	y.files = make(map[string]string)
	y.contents = make(map[string]jsonnet.Contents)
}

// Reset clears cached YAML imports and import-cycle state.
func (y *YamlImporter) Reset() {
	y.reset()
}

// ImportsKey sets the key in the YAML files, which lists the imports.
func (y *YamlImporter) ImportsKey(key string) {
	if key != "" {
		y.key = key
	}
}

// Wrapper sets the jsonnet function expression, which gets the parameters
// base, doc and file for YAML files with imports.
func (y *YamlImporter) Wrapper(expression string) {
	y.wrapper = expression
}

// JsonnetEntryWrapper sets a jsonnet function expression for the entries of
// `imports` which are not YAML files. Such an entry becomes
// `(<expression>)((import '<entry>'), '<entry>')` instead of `(import '<entry>')`.
// The function gets the imported value and the entry as it is imported. It can
// e.g. mark the value, so that the wrapper of the YAML file knows its place in
// the list. YAML entries (`yaml://`) are not changed.
func (y *YamlImporter) JsonnetEntryWrapper(expression string) {
	y.entryWrapper = expression
}

// FileRefs sets a path in the YAML documents, where file names are listed, e.g.
// `project.components[].file`. A `[]` after a key walks over the list. The value
// at the path is a string or a list of strings. Every entry is imported relative
// to the YAML file (.yaml and .yml via `yaml://`) and the wrapper gets them as
// the parameter `refs`, an object with one import per entry. The key is
// `<file>::<entry>`, with file as given to the wrapper. A missing file returns an
// error, which names the YAML file. The wrapper must accept the parameter refs.
func (y *YamlImporter) FileRefs(path string) {
	y.refPath = path
}

func (y *YamlImporter) setImportGraph(importGraph graph.Graph[string, string], importCounter int) {
	y.shared = importGraph
	y.importCounter = importCounter
}

func (y *YamlImporter) setIgnoreImportCycles(ignore bool) {
	y.ignoreCycles = ignore
}

// Logger can be used to set the zap.Logger for the YamlImporter.
func (y *YamlImporter) Logger(logger *zap.Logger) {
	if logger != nil {
		y.logger = logger
	}
}

// CanHandle returns true for the prefix `yaml`.
func (y *YamlImporter) CanHandle(prefix string) bool {
	return prefix == yamlPrefix
}

// Prefixa returns the list of supported prefixa for this importer.
func (y *YamlImporter) Prefixa() []string {
	return []string{yamlPrefix}
}

// Import implements the go-jsonnet Importer interface method.
func (y *YamlImporter) Import(importedFrom, importedPath string) (jsonnet.Contents, string, error) {
	logger := y.logger.Named("YamlImporter")
	logger.Debug("Import()",
		zap.String("importedFrom", importedFrom),
		zap.String("importedPath", importedPath),
	)

	empty := jsonnet.MakeContents("")
	name := strings.TrimPrefix(importedPath, yamlScheme)

	file, err := y.find(filepath.Dir(y.realPath(importedFrom)), name)
	if err != nil {
		return empty, "", err
	}

	if err := y.trackImport(importedFrom, file); err != nil {
		return empty, "", err
	}

	data, err := afero.ReadFile(y.fs, file)
	if err != nil {
		return empty, "", fmt.Errorf("while reading '%s': %w", file, err)
	}

	imports, err := y.importsOf(data, file)
	if err != nil {
		return empty, "", err
	}

	refs, err := y.refsOf(data, file)
	if err != nil {
		return empty, "", err
	}

	base := filepath.Base(file)
	plain := fmt.Sprintf("std.parseYaml(importstr %s)", quote(base))
	code := plain

	foundAt := foundAt(file)
	if y.shared != nil {
		y.addSharedEdge(importedFrom, foundAt, importedPath)
	}

	wrapper := y.wrapper
	if len(imports) > 0 || len(refs) > 0 || wrapper != "" {
		exprs := make([]string, 0, len(imports))

		for _, imp := range imports {
			target := y.toImport(file, imp)
			expr := fmt.Sprintf("(import %s)", quote(target))
			if y.entryWrapper != "" && !strings.HasPrefix(target, yamlScheme) {
				expr = fmt.Sprintf("(%s)(%s, %s)", y.entryWrapper, expr, quote(target))
			}

			exprs = append(exprs, expr)
		}

		if wrapper == "" {
			wrapper = fmt.Sprintf(defaultYamlWrapper, quote(y.key))
		}

		baseExpr := "{}"
		if len(exprs) > 0 {
			baseExpr = strings.Join(exprs, " + ")
		}

		refsArg := ""

		if len(refs) > 0 {
			items := make([]string, 0, len(refs))
			for _, r := range refs {
				items = append(items, fmt.Sprintf("%s: (import %s)", quote(file+"::"+r), quote(y.toImport(file, r))))
			}

			refsArg = ", refs = { " + strings.Join(items, ", ") + " }"
		}

		code = fmt.Sprintf("(%s)(base = %s, doc = %s, file = %s%s)",
			wrapper, baseExpr, plain, quote(file), refsArg)
	}

	y.files[foundAt] = file
	if cached, ok := y.contents[foundAt]; ok {
		return cached, foundAt, nil
	}

	contents := jsonnet.MakeContents(code)
	y.contents[foundAt] = contents
	logger.Debug("returns", zap.String("contents", code), zap.String("foundAt", foundAt))

	return contents, foundAt, nil
}

// addSharedEdge mirrors the import of a YAML file into the import graph of the
// MultiImporter, where the YAML code is stored under its foundAt name.
func (y *YamlImporter) addSharedEdge(importedFrom, found, importedPath string) {
	from := filepath.Clean(importedFrom)
	if importedFrom == "" {
		from = importedPath
	}

	attrs := []func(*graph.VertexProperties){
		graph.VertexAttribute("shape", "note"),
		graph.VertexAttribute("label", found),
	}
	_ = y.shared.AddVertex(from)
	_ = y.shared.AddVertex(found, attrs...)
	_ = y.shared.AddEdge(from, found, graph.EdgeWeight(y.importCounter), graph.EdgeAttribute("label", importedPath))
}

// realPath returns the real file behind a foundAt value created by this importer.
func (y *YamlImporter) realPath(importedFrom string) string {
	if f, ok := y.files[importedFrom]; ok {
		return f
	}

	return importedFrom
}

// find looks for the file relative to cwd first and then in the JPaths.
func (y *YamlImporter) find(cwd, name string) (string, error) {
	candidates := []string{}

	if filepath.IsAbs(name) {
		candidates = append(candidates, name)
	} else {
		candidates = append(candidates, filepath.Join(cwd, name))

		for _, p := range y.JPaths {
			candidates = append(candidates, filepath.Join(p, name))
		}
	}

	for _, c := range candidates {
		if info, err := y.fs.Stat(c); err == nil && !info.IsDir() {
			return filepath.Clean(c), nil
		}
	}

	return "", fmt.Errorf("couldn't open import %q: no match locally or in the Jsonnet library paths: %w",
		name, os.ErrNotExist)
}

// trackImport adds the import edge to the dependency graph and returns an
// error if this edge closes a cycle.
func (y *YamlImporter) trackImport(importedFrom, file string) error {
	from := filepath.Clean(y.realPath(importedFrom))
	_ = y.deps.AddVertex(from)
	_ = y.deps.AddVertex(file)

	if cycle, _ := graph.CreatesCycle(y.deps, from, file); cycle {
		if y.ignoreCycles {
			return nil
		}

		return fmt.Errorf("%w detected with adding %s to %s", ErrImportCycle, from, file)
	}

	_ = y.deps.AddEdge(from, file)

	return nil
}

// importsOf returns the list under the imports key of a YAML document.
func (y *YamlImporter) importsOf(data []byte, file string) ([]string, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var first yaml.Node
	if err := decoder.Decode(&first); err != nil {
		return nil, nil //nolint:nilerr
	}
	var second yaml.Node
	if err := decoder.Decode(&second); err == nil {
		return nil, fmt.Errorf("%w: multi-document YAML is not supported in '%s'",
			ErrMalformedImport, file)
	} else if err != io.EOF {
		return nil, nil //nolint:nilerr
	}

	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		// no mapping, so no imports. Syntax errors show up in std.parseYaml.
		return nil, nil //nolint:nilerr
	}

	raw, exists := doc[y.key]
	if !exists || raw == nil {
		return nil, nil
	}

	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: key '%s' in '%s' must be a list of strings",
			ErrMalformedImport, y.key, file)
	}

	imports := make([]string, 0, len(list))

	for _, item := range list {
		s, ok := item.(string)
		if !ok || s == "" {
			return nil, fmt.Errorf("%w: key '%s' in '%s' must be a list of strings",
				ErrMalformedImport, y.key, file)
		}

		imports = append(imports, s)
	}

	return imports, nil
}

// refsOf returns the distinct entries found at the refPath of a YAML document.
// An entry without a scheme must exist, else the error names the YAML file.
func (y *YamlImporter) refsOf(data []byte, file string) ([]string, error) {
	if y.refPath == "" {
		return nil, nil
	}

	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil //nolint:nilerr
	}

	var refs []string

	seen := map[string]bool{}

	for _, e := range walkRefs(doc, strings.Split(y.refPath, ".")) {
		if seen[e] {
			continue
		}

		seen[e] = true

		if !strings.Contains(e, "://") || strings.HasPrefix(e, yamlScheme) {
			if _, err := y.find(filepath.Dir(file), strings.TrimPrefix(e, yamlScheme)); err != nil {
				return nil, fmt.Errorf("file '%s' referenced in '%s': %w", e, file, err)
			}
		}

		refs = append(refs, e)
	}

	return refs, nil
}

// walkRefs follows the path segments and returns the strings at the end.
func walkRefs(node any, segs []string) []string {
	if len(segs) == 0 {
		switch v := node.(type) {
		case string:
			return []string{v}
		case []any:
			out := []string{}

			for _, item := range v {
				if s, ok := item.(string); ok && s != "" {
					out = append(out, s)
				}
			}

			return out
		}

		return nil
	}

	m, ok := node.(map[string]any)
	if !ok {
		return nil
	}

	name, isList := strings.CutSuffix(segs[0], "[]")
	next := m[name]

	if !isList {
		return walkRefs(next, segs[1:])
	}

	list, ok := next.([]any)
	if !ok {
		return nil
	}

	out := []string{}
	for _, item := range list {
		out = append(out, walkRefs(item, segs[1:])...)
	}

	return out
}

// toImport converts YAML/YML entries into yaml:// imports and keeps the rest.
func (y *YamlImporter) toImport(file, entry string) string {
	if strings.Contains(entry, "://") && !strings.HasPrefix(entry, yamlScheme) {
		return entry
	}

	if isYamlFile(entry) {
		return yamlScheme + strings.TrimPrefix(entry, yamlScheme)
	}

	return entry
}

// foundAt returns a path next to the file, which differs from the file itself.
// Otherwise the importstr of the file would clash with the code in the import
// cache and would look like an import cycle to the MultiImporter.
func foundAt(file string) string {
	return file + "#yaml"
}

func isYamlFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))

	return ext == ".yaml" || ext == ".yml"
}

func quote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}
