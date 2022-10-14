package importer

import (
	"errors"
	"fmt"

	"github.com/google/go-jsonnet"
	"go.uber.org/zap"
)

var (
	ErrNoImporter           = errors.New("no importer")
	ErrUnknownPrefix        = errors.New("unknown prefix")
	ErrMalformedAlias       = errors.New("malformed alias")
	ErrMalformedGlobPattern = errors.New("malformed glob pattern")
	ErrImportCycle          = errors.New("import cycle")
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
		importers []Importer
		logger    *zap.Logger
	}
)

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

// Import is used by go-jsonnet to run this importer. It implements the go-jsonnet
// Importer interface method.
func (m *MultiImporter) Import(importedFrom, importedPath string) (jsonnet.Contents, string, error) {
	logger := m.logger.Named("MultiImporter")
	logger.Debug(
		"Import()",
		zap.String("importedFrom", importedFrom),
		zap.String("importedPath", importedPath),
	)

	importerTypes := make([]string, 0, len(m.importers))
	prefixa := make(map[string][]string, len(m.importers))

	for _, importer := range m.importers {
		t := fmt.Sprintf("%T", importer)
		importerTypes = append(importerTypes, t)
		prefixa[t] = importer.Prefixa()
	}

	logger.Debug(
		"trying to find the right importer for importedPath",
		zap.Strings("importers", importerTypes),
		zap.String("importedPath", importedPath),
	)

	for _, importer := range m.importers {
		if importer.CanHandle(importedPath) {
			logger.Info(
				"found importer for importedPath",
				zap.String("importer", fmt.Sprintf("%T", importer)),
				zap.String("importedPath", importedPath),
			)

			contents, foundAt, err := importer.Import(importedFrom, importedPath)
			if err != nil {
				return jsonnet.MakeContents(""),
					"",
					fmt.Errorf("%w for importer: %T", err, importer)
			}

			return contents, foundAt, nil
		}
	}

	logger.Error(
		"found no importer for importedPath",
		zap.String("importedPath", importedPath),
		zap.Any("supported prefixa per loaded importer", prefixa),
	)

	return jsonnet.MakeContents(""),
		"",
		fmt.Errorf("%w can handle given path: '%s'", ErrNoImporter, importedPath)
}

// stringKeysFromMap returns the keys from a map as slice
func stringKeysFromMap(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	return keys
}

// stringValuesFromMap returns the values from a map as slice
func stringValuesFromMap(m map[string]string) []string {
	values := make([]string, 0, len(m))
	for _, v := range m {
		values = append(values, v)
	}

	return values
}
