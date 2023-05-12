// Package importer implements custom importers for go-jsonnet.
//
// Custom importers extend the original importers with extra functionality, like
// the support for glob pattern, so that a user can import multiple files at
// once.
package importer

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"

	"github.com/dominikbraun/graph"
	"github.com/dominikbraun/graph/draw"
	"github.com/google/go-jsonnet"
	"github.com/spf13/afero"
	"go.uber.org/zap"
)

const (
	importGraphFileName = "import_graph.gv"
)

var (
	ErrNoImporter           = errors.New("no importer")
	ErrUnknownPrefix        = errors.New("unknown prefix")
	ErrMalformedAlias       = errors.New("malformed alias")
	ErrMalformedGlobPattern = errors.New("malformed glob pattern")
	ErrImportCycle          = errors.New("import cycle")
	ErrEmptyResult          = errors.New("empty result")
	ErrUnknownConfig        = errors.New("unknown config")
	ErrMalformedImport      = errors.New("malformed import string")
	ErrMalformedQuery       = errors.New("malformed query parameter(s)")
)

type (

	// Importer extends the jsonnet importer interface and adds a method to get
	// the right importer for a given path.
	Importer interface {
		jsonnet.Importer
		// CanHandle will be used to decide if an importer can handle the given
		// import path.
		CanHandle(path string) bool
		// Logger can be used to set a zap.Logger for the importer.
		// (see https://pkg.go.dev/go.uber.org/zap)
		Logger(*zap.Logger)
		// Prefixa returns the list of prefixa, which will trigger the specific
		// importer. An empty list means no prefix used/needed.
		Prefixa() []string
		setImportGraph(graph.Graph[string, string], int)
	}

	// FallbackFileImporter is a wrapper for the original go-jsonnet FileImporter.
	// The idea is to provide a chain for importers in the MultiImporter, with
	// the FileImporter as fallback, if nothing else can handle the given
	// import prefix (and of course also no prefix).
	FallbackFileImporter struct {
		*jsonnet.FileImporter
	}

	// MultiImporter supports multiple importers and tries to find the right
	// importer from a list of importers.
	MultiImporter struct {
		importers          []Importer
		logger             *zap.Logger
		logLevel           string
		ignoreImportCycles bool
		importGraph        graph.Graph[string, string]
		importCounter      int
		importGraphFile    string
		enableImportGraph  bool
		fs                 afero.Fs
	}
)

func (f *FallbackFileImporter) setImportGraph(_ graph.Graph[string, string], _ int) {}

// NewFallbackFileImporter returns finally the original go-jsonnet FileImporter.
// As optional parameters extra library search paths (aka. jpath) can be provided too.
func NewFallbackFileImporter(jpaths ...string) *FallbackFileImporter {
	return &FallbackFileImporter{FileImporter: &jsonnet.FileImporter{JPaths: jpaths}}
}

// CanHandle method of the FallbackFileImporter returns always true.
func (f *FallbackFileImporter) CanHandle(_ string) bool {
	return true
}

// Logger implements the Logger interface method, but does not do anything as
// the FallbackFileImporter is just a wrapper for the go-jsonnet FileImporter.
func (f *FallbackFileImporter) Logger(_ *zap.Logger) {}

// Prefixa for the FallbackFileImporter returns an empty list.
func (f *FallbackFileImporter) Prefixa() []string {
	return []string{""}
}

// NewMultiImporter returns an instance of a MultiImporter with default settings,
// like all custom importers + fallback importer.
func NewMultiImporter(importers ...Importer) *MultiImporter {
	multiImporter := &MultiImporter{
		importers: importers,
		logger:    zap.New(nil),
		importGraph: graph.New(
			graph.StringHash, graph.Tree(), graph.Directed(), graph.Weighted(),
		),
		importGraphFile:    importGraphFileName,
		fs:                 afero.NewOsFs(),
		logLevel:           "",
		ignoreImportCycles: false,
		importCounter:      0,
		enableImportGraph:  false,
	}
	if len(multiImporter.importers) == 0 {
		multiImporter.importers = []Importer{
			NewGlobImporter(),
			NewFallbackFileImporter(),
		}
	}

	return multiImporter
}

// Logger method can be used to set a zap.Logger for all importers at once.
// (see https://pkg.go.dev/go.uber.org/zap)
func (m *MultiImporter) Logger(logger *zap.Logger) {
	if logger != nil {
		m.logger = logger
		for _, i := range m.importers {
			i.Logger(logger)
		}
	}
}

func (m *MultiImporter) SetImportGraphFile(name string) {
	m.importGraphFile = name
	m.enableImportGraph = true
}

// IgnoreImportCycles disables the test for import cycles and therefore also any
// error in that regard.
func (m *MultiImporter) IgnoreImportCycles() {
	m.ignoreImportCycles = true
}

// Import is used by go-jsonnet to run this importer. It implements the go-jsonnet
// Importer interface method.
func (m *MultiImporter) Import(importedFrom, importedPath string) (jsonnet.Contents, string, error) {
	logger := m.logger.Named("MultiImporter")
	logger.Debug("Import()",
		zap.String("importedFrom", importedFrom),
		zap.String("importedPath", importedPath),
	)

	prefix, err := m.parseImportString(importedFrom, importedPath)
	if err != nil {
		return jsonnet.MakeContents(""), "", err
	}

	if prefix == "config" {
		return jsonnet.MakeContents("{}"), "", nil
	}

	for _, importer := range m.importers {
		if importer.CanHandle(prefix) {
			logger.Info("found importer for importedPath",
				zap.String("importer", fmt.Sprintf("%T", importer)),
				zap.String("importedPath", importedPath),
				zap.String("prefix", prefix),
			)
			importer.setImportGraph(m.importGraph, m.importCounter)

			contents, foundAt, err := importer.Import(importedFrom, importedPath)
			if err != nil {
				return jsonnet.MakeContents(""), "",
					fmt.Errorf("custom importer '%T' returns error: %w", importer, err)
			}

			return contents, foundAt, nil
		}
	}

	return jsonnet.MakeContents(""), "",
		fmt.Errorf("%w can handle given path: '%s'", ErrNoImporter, importedPath)
}

// parseImportString uses the url library to parse the importedPath. Depending on the parsed
// scheme, it:
// - parses the query part of the importedPath for configurations, if the scheme is "config".
// - checks for import cycles, if the scheme is empty.
// Finally the scheme (here called "prefix") is returned.
func (m *MultiImporter) parseImportString(importedFrom, importedPath string) (string, error) {
	parsedURL, err := url.Parse(importedPath)
	if err != nil {
		return "", fmt.Errorf("%w: '%s', error: %s", ErrMalformedImport, importedPath, err)
	}

	prefix := parsedURL.Scheme
	switch prefix {
	case "config":
		if err := m.parseInFileConfigs(parsedURL.RawQuery); err != nil {
			return "", fmt.Errorf("in importedPath: '%s', error: %w", importedPath, err)
		}

		return prefix, nil
	case "": // "normal" imports
		if !m.ignoreImportCycles {
			if err := m.findImportCycle(importedFrom, importedPath); err != nil {
				return "",
					fmt.Errorf("%w detected with adding %s to %s. DOT-graph stored in '%s'",
						ErrImportCycle, importedFrom, importedPath, m.importGraphFile)
			}
		}

		if m.enableImportGraph {
			if err := m.storeImportGraph(); err != nil {
				return "", err
			}
		}
	}
	// set the level/weight inside the graph
	m.importCounter++

	return prefix, nil
}

func (m *MultiImporter) storeImportGraph() error {
	image, err := m.fs.Create(m.importGraphFile)
	if err != nil {
		return fmt.Errorf("while storing import graph to file '%s', error: %w", m.importGraphFile, err)
	}

	return draw.DOT(m.importGraph, image)
}

func (m *MultiImporter) findImportCycle(importedFrom, importedPath string) error {
	cImportedFrom := filepath.Clean(importedFrom)

	_ = m.importGraph.AddVertex(cImportedFrom, graph.VertexAttribute("shape", "invhouse"))
	_ = m.importGraph.AddVertex(importedPath, graph.VertexAttribute("shape", "house"))

	if hasCycle, _ := graph.CreatesCycle(m.importGraph, cImportedFrom, importedPath); hasCycle {
		_ = m.importGraph.AddEdge(
			cImportedFrom, importedPath, graph.EdgeWeight(m.importCounter), graph.EdgeAttribute("color", "red"),
		)

		image, _ := m.fs.Create(m.importGraphFile)
		_ = draw.DOT(m.importGraph, image)

		return fmt.Errorf("%w detected with adding %s to %s. DOT-Graph stored in '%s'",
			ErrImportCycle, cImportedFrom, importedPath, m.importGraphFile)
	}

	_ = m.importGraph.AddEdge(cImportedFrom, importedPath, graph.EdgeWeight(m.importCounter))

	// given importedPath can also be relative to caller therefore get the whole path too
	cwd, _ := filepath.Split(importedFrom)
	resolvedPath := filepath.Join(cwd, importedPath)
	// importedPath is given relative to caller ?
	if importedPath != resolvedPath {
		_ = m.importGraph.AddVertex(resolvedPath)

		if cycle, _ := graph.CreatesCycle(m.importGraph, importedPath, resolvedPath); cycle {
			_ = m.importGraph.AddEdge(
				importedPath, resolvedPath, graph.EdgeWeight(m.importCounter), graph.EdgeAttribute("color", "red"),
			)

			image, _ := m.fs.Create(m.importGraphFile)
			_ = draw.DOT(m.importGraph, image)

			return fmt.Errorf("%w detected with adding %s to %s. DOT-Graph stored in '%s'",
				ErrImportCycle, importedPath, resolvedPath, m.importGraphFile)
		}

		_ = m.importGraph.AddEdge(importedPath, resolvedPath, graph.EdgeWeight(m.importCounter))
	}

	return nil
}

func (m *MultiImporter) parseInFileConfigs(rawQuery string) error {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return fmt.Errorf("%w: '%s', got error: %s",
			ErrMalformedQuery, rawQuery, err)
	}

	if file, exists := query["importGraph"]; exists {
		m.importGraphFile = file[0]
		m.enableImportGraph = true
	}

	if _, exists := query["ignoreImportCycles"]; exists {
		m.ignoreImportCycles = true
	}

	if level, exists := query["logLevel"]; exists {
		m.logLevel = level[0]

		var logger *zap.Logger

		switch m.logLevel {
		case "debug":
			logger, err = zap.NewDevelopment()
			if err != nil {
				return fmt.Errorf("while setting debug logger: %w", err)
			}
		case "info":
			logger, err = zap.NewProduction()
			if err != nil {
				return fmt.Errorf("while setting info logger: %w", err)
			}
		default:
			return fmt.Errorf("%w: logLevel=%s, supported are 'logLevel=debug' or 'logLevel=info'",
				ErrUnknownConfig, m.logLevel)
		}

		m.Logger(logger)
	}

	return nil
}

// stringKeysFromMap returns the keys from a map as slice.
func stringKeysFromMap(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	return keys
}

// stringValuesFromMap returns the values from a map as slice.
func stringValuesFromMap(m map[string]string) []string {
	values := make([]string, 0, len(m))
	for _, v := range m {
		values = append(values, v)
	}

	return values
}
