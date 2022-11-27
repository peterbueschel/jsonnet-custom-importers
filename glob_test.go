package importer

import (
	"os"
	"testing"

	"github.com/google/go-jsonnet"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestGlobImporter_resolveFilesFrom(t *testing.T) {
	type fields struct {
		excludePattern string
		testFolders    []string
		testFiles      map[string]string
	}
	type args struct {
		searchPaths []string
		cwd         string
		pattern     string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    []string
		wantErr bool
	}{
		{
			name: "existing folder given and should return files without error",
			fields: fields{
				testFolders: []string{"vendor"},
				testFiles: map[string]string{
					"vendor/a.jsonnet": "{a: 1}",
				},
			},
			args: args{
				searchPaths: []string{"vendor"},
				pattern:     "*.jsonnet",
			},
			want:    []string{"vendor/a.jsonnet"},
			wantErr: false,
		},
		{
			name:   "malformed glob pattern - should return error",
			fields: fields{},
			args: args{
				searchPaths: []string{"testdata"},
				pattern:     "[",
			},
			want:    []string{},
			wantErr: true,
		},
		{
			name: "existing folder given with excludePattern for everything and should return empty result error",
			fields: fields{
				excludePattern: "**/*.libsonnet",
				testFolders:    []string{"vendor"},
				testFiles: map[string]string{
					"vendor/ignoreMe.libsonnet": "{b: 2}",
					"vendor/meToo.libsonnet":    "{a: 1}",
				},
			},
			args: args{
				searchPaths: []string{"vendor"},
				pattern:     "*.libsonnet",
			},
			want:    []string{},
			wantErr: true,
		},
		{
			name: "none-existing folder - should return empty result error",
			fields: fields{
				testFolders: []string{"vendor"},
				testFiles: map[string]string{
					"vendor/a.jsonnet": "{a: 1}",
				},
			},
			args: args{
				searchPaths: []string{"rodnev"},
				pattern:     "*.jsonnet",
			},
			want:    []string{},
			wantErr: true,
		},
		{
			name: "glob pattern doesn't match - should return empty result error",
			fields: fields{
				testFolders: []string{"vendor"},
				testFiles: map[string]string{
					"vendor/a.jsonnet": "{a: 1}",
				},
			},
			args: args{
				searchPaths: []string{"vendor"},
				pattern:     "*.xxxxx",
			},
			want:    []string{},
			wantErr: true,
		},
		{
			name: "two jpath set and resolvedFiles are merged",
			fields: fields{
				testFolders: []string{"vendor/models", "vendor/monaco"},
				testFiles: map[string]string{
					"vendor/monaco/a.libsonnet": "{a: 1}",
					"vendor/models/b.libsonnet": "{b: 2}",
				},
			},
			args: args{
				searchPaths: []string{"vendor/models", "vendor/monaco"},
				pattern:     "*.libsonnet",
			},
			want:    []string{"vendor/models/b.libsonnet", "vendor/monaco/a.libsonnet"},
			wantErr: false,
		},
		{
			name: "one jpath set and resolvedFiles are merged - local path will be behind jpath",
			fields: fields{
				testFolders: []string{"vendor/models", "models"},
				testFiles: map[string]string{
					"models/a.jsonnet":        "{a: 1}",
					"vendor/models/b.jsonnet": "{b: 2}",
				},
			},
			args: args{
				searchPaths: []string{"vendor/"},
				cwd:         ".",
				pattern:     "models/*.jsonnet",
			},
			want:    []string{"vendor/models/b.jsonnet", "models/a.jsonnet"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGlobImporter()
			g.excludePattern = tt.fields.excludePattern

			fs := afero.NewMemMapFs()
			for _, tF := range tt.fields.testFolders {
				if err := fs.MkdirAll(tF, 0o755); err != nil {
					t.Errorf("GlobImporter.resolveFilesFrom() error = %v", err)
					return
				}
			}
			for file, cnt := range tt.fields.testFiles {
				if err := afero.WriteFile(fs, file, []byte(cnt), 0o644); err != nil {
					t.Errorf("GlobImporter.resolveFilesFrom() error = %v", err)
					return
				}
			}
			g.fs = fs

			got, err := g.resolveFilesFrom(tt.args.searchPaths, tt.args.cwd, tt.args.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("GlobImporter.resolveFilesFrom() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGlobImporter_Import(t *testing.T) {
	lvl := zap.NewAtomicLevel()
	cfg := zap.NewDevelopmentEncoderConfig()
	cfg.TimeKey = ""

	lvl.SetLevel(zap.DebugLevel)

	logger := zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(cfg),
		zapcore.Lock(os.Stdout),
		lvl,
	))

	if testing.Short() {
		// disable logging output in tests
		logger = zap.New(nil)
	}

	type fields struct {
		testFolders []string
		testFiles   map[string]string
	}

	type args struct {
		importedFrom string
		importedPath string
	}
	tests := []struct {
		name        string
		jpaths      []string
		fields      fields
		args        args
		want        jsonnet.Contents
		wantFoundAt string
		wantErr     bool
	}{
		{
			name:   "glob matches - simple",
			jpaths: []string{},
			fields: fields{
				testFiles: map[string]string{
					"a.jsonnet": "{a: 1}",
				},
			},
			args: args{
				importedFrom: "",
				importedPath: "glob+://*.jsonnet",
			},
			want:        jsonnet.MakeContents("(import 'a.jsonnet')"),
			wantFoundAt: "./",
			wantErr:     false,
		},
		{
			name:   "glob does not match any file - should return error",
			jpaths: []string{},
			fields: fields{
				testFiles: map[string]string{
					"a.jsonnet": "{a: 1}",
				},
			},
			args: args{
				importedFrom: "",
				importedPath: "glob+://*.libsonnet",
			},
			want:        jsonnet.MakeContents(""),
			wantFoundAt: "./",
			wantErr:     true,
		},
		{
			name:   "jpath set - same file in cwd found - cwd file has higher priority",
			jpaths: []string{"vendor"},
			fields: fields{
				testFolders: []string{"vendor"},
				testFiles: map[string]string{
					"b.jsonnet":        "{b: 1}",
					"vendor/b.jsonnet": "{b: 2}",
				},
			},
			args: args{
				importedFrom: "",
				importedPath: "glob+://*.jsonnet",
			},
			want:        jsonnet.MakeContents("(import 'vendor/b.jsonnet')+(import 'b.jsonnet')"),
			wantFoundAt: "./",
		},
		{
			name:   "jpath and cwd file given - imports have correct lexicographical and hierachically order",
			jpaths: []string{"vendor/b/dev", "vendor/a/prod/canary", "vendor/a/prod"},
			fields: fields{
				testFolders: []string{"vendor/b/dev", "vendor/a/prod/canary", "vendor/a/prod"},
				testFiles: map[string]string{
					"a.jsonnet":                      "{a: 1}",
					"vendor/a/prod/a.jsonnet":        "{a: 2}",
					"vendor/a/prod/canary/a.jsonnet": "{a: 3}",
					"vendor/b/dev/b.jsonnet":         "{b: 1}",
				},
			},
			args: args{
				importedFrom: "",
				importedPath: "glob+://*.jsonnet",
			},
			want: jsonnet.MakeContents(
				"(import 'vendor/a/prod/a.jsonnet')+(import 'vendor/a/prod/canary/a.jsonnet')+(import 'vendor/b/dev/b.jsonnet')+(import 'a.jsonnet')",
			),
			wantFoundAt: "./",
		},
		{
			name:   "jpath set to cwd - duplicates imports",
			jpaths: []string{"."},
			fields: fields{
				testFiles: map[string]string{
					"a.jsonnet": "{a: 1}",
				},
			},
			args: args{
				importedFrom: "",
				importedPath: "glob+://*.jsonnet",
			},
			want:        jsonnet.MakeContents("(import 'a.jsonnet')+(import 'a.jsonnet')"),
			wantFoundAt: "./",
		},
		{
			name:   "two jpath set and contents are merged",
			jpaths: []string{"vendor/a", "vendor/b"},
			fields: fields{
				testFolders: []string{"vendor/a", "vendor/b"},
				testFiles: map[string]string{
					"vendor/a/b.jsonnet": "{b: 1}",
					"vendor/b/b.jsonnet": "{b: 2}",
				},
			},
			args: args{
				importedFrom: "",
				importedPath: "glob+://*.jsonnet",
			},
			want: jsonnet.MakeContents(
				"(import 'vendor/a/b.jsonnet')+(import 'vendor/b/b.jsonnet')",
			),
			wantFoundAt: "./",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGlobImporter(tt.jpaths...)
			g.Logger(logger)

			fs := afero.NewMemMapFs()
			for _, tF := range tt.fields.testFolders {
				if err := fs.MkdirAll(tF, 0o755); err != nil {
					t.Errorf("GlobImporter.Import() error = %v", err)
					return
				}
			}
			for file, cnt := range tt.fields.testFiles {
				if err := afero.WriteFile(fs, file, []byte(cnt), 0o644); err != nil {
					t.Errorf("GlobImporter.Import() error = %v", err)
					return
				}
			}
			g.fs = fs

			got, gotFoundAt, err := g.Import(tt.args.importedFrom, tt.args.importedPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("GlobImporter.Import() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantFoundAt, gotFoundAt)
		})
	}
}

func TestGlobImporter_handle(t *testing.T) {
	type fields struct {
		aliases map[string]string
	}
	type args struct {
		files  []string
		prefix string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "glob-str+",
			args: args{
				files:  []string{"a.jsonnet", "b.jsonnet"},
				prefix: "glob-str+",
			},
			want:    `(importstr 'a.jsonnet')+(importstr 'b.jsonnet')`,
			wantErr: false,
		},
		{
			name: "glob+",
			args: args{
				files:  []string{"a.jsonnet", "b.jsonnet"},
				prefix: "glob+",
			},
			want:    `(import 'a.jsonnet')+(import 'b.jsonnet')`,
			wantErr: false,
		},
		// ---------------------------------------------------------- glob.file
		{
			name: "glob.file",
			args: args{
				files:  []string{"a.jsonnet", "b.jsonnet"},
				prefix: "glob.file",
			},
			want:    "{\n'a.jsonnet': (import 'a.jsonnet'),\n'b.jsonnet': (import 'b.jsonnet'),\n}",
			wantErr: false,
		},
		{
			name: "glob-str.file",
			args: args{
				files:  []string{"a.jsonnet", "b.jsonnet"},
				prefix: "glob-str.file",
			},
			want:    "{\n'a.jsonnet': (importstr 'a.jsonnet'),\n'b.jsonnet': (importstr 'b.jsonnet'),\n}",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGlobImporter()
			g.aliases = tt.fields.aliases

			got, err := g.handle(tt.args.files, tt.args.prefix)
			if (err != nil) != tt.wantErr {
				t.Errorf("GlobImporter.handle() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			assert.Equal(t, tt.want, got)
		})
	}
}
