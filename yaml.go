package importer

import (
	"fmt"
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
	defaultYamlWrapper = "function(base, doc, file) base + { [k]: doc[k] for k in std.objectFields(doc) if k != '%s' }"
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
	//	  file = 'file.yaml',
	//	)
	//
	// The wrapper is a jsonnet function expression with the parameters base,
	// doc and file.
	YamlImporter struct {
		// JPaths stores extra search paths.
		JPaths []string

		fs      afero.Fs
		logger  *zap.Logger
		key     string
		wrapper string

		// deps tracks which file imports which, to detect cycles.
		deps graph.Graph[string, string]
		// files maps the foundAt values to the resolved files.
		files         map[string]string
		importCounter int
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

func (y *YamlImporter) setImportGraph(_ graph.Graph[string, string], importCounter int) {
	y.importCounter = importCounter
}

// Logger can be used to set the zap.Logger for the YamlImporter.
func (y *YamlImporter) Logger(logger *zap.Logger) {
	if logger != nil {
		y.logger = logger
	}
}

// CanHandle returns true for the prefix `yaml`.
func (y *YamlImporter) CanHandle(prefix string) bool {
	return strings.HasPrefix(prefix, yamlPrefix)
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

	base := filepath.Base(file)
	plain := fmt.Sprintf("std.parseYaml(importstr %s)", quote(base))
	code := plain

	if len(imports) > 0 {
		exprs := make([]string, 0, len(imports))

		for _, imp := range imports {
			exprs = append(exprs, fmt.Sprintf("(import %s)", quote(y.toImport(file, imp))))
		}

		wrapper := y.wrapper
		if wrapper == "" {
			wrapper = fmt.Sprintf(defaultYamlWrapper, y.key)
		}

		code = fmt.Sprintf("(%s)(base = %s, doc = %s, file = %s)",
			wrapper, strings.Join(exprs, " + "), plain, quote(base))
	}

	foundAt := foundAt(file)
	y.files[foundAt] = file

	logger.Debug("returns", zap.String("contents", code), zap.String("foundAt", foundAt))

	return jsonnet.MakeContents(code), foundAt, nil
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
		return fmt.Errorf("%w detected with adding %s to %s", ErrImportCycle, from, file)
	}

	_ = y.deps.AddEdge(from, file)

	return nil
}

// importsOf returns the list under the imports key of a YAML document.
func (y *YamlImporter) importsOf(data []byte, file string) ([]string, error) {
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

// toImport converts YAML/YML entries into yaml:// imports and keeps the rest.
// It also records the edges of the imported jsonnet files.
func (y *YamlImporter) toImport(file, entry string) string {
	if isYamlFile(entry) {
		return yamlScheme + strings.TrimPrefix(entry, yamlScheme)
	}

	if !strings.Contains(entry, "://") {
		dep := filepath.Join(filepath.Dir(file), entry)
		_ = y.deps.AddVertex(dep)
		_ = y.deps.AddEdge(file, dep)
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
