package importer

import (
	"os"
	"testing"

	"github.com/google/go-jsonnet"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestGlobImporter_resolveFilesFrom(t *testing.T) {
	type fields struct {
		excludePattern string
	}
	type args struct {
		searchPaths []string
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
			name:   "existing folder given and should return files without error",
			fields: fields{},
			args: args{
				searchPaths: []string{"testdata/globPlus"},
				pattern:     "*.libsonnet",
			},
			want: []string{"testdata/globPlus/host.libsonnet"},
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
			name: "existing folder given with excludePattern and should return files without error",
			fields: fields{
				excludePattern: "**/*host.libsonnet",
			},
			args: args{
				searchPaths: []string{"testdata/globPlus"},
				pattern:     "*.libsonnet",
			},
			want: []string{},
		},
		{
			name:   "only none-existing folder and should return error",
			fields: fields{},
			args: args{
				searchPaths: []string{"globPlus"},
				pattern:     "*.jsonnet",
			},
			want:    []string{},
			wantErr: true,
		},
		{
			name:   "one none-existing folder and one existing folder, but glob pattern doesn't match - should return empty result error",
			fields: fields{},
			args: args{
				searchPaths: []string{"globPlus", "testdata"},
				pattern:     "*.xsonnet",
			},
			want:    []string{},
			wantErr: true,
		},
		{
			name:   "two jpath set and resolvedFiles are merged",
			fields: fields{},
			args: args{
				searchPaths: []string{"testdata/globPlus", "testdata/globDot"},
				pattern:     "*.libsonnet",
			},
			want:    []string{"testdata/globDot/host.libsonnet", "testdata/globPlus/host.libsonnet"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &GlobImporter{
				excludePattern: tt.fields.excludePattern,
			}
			got, err := g.resolveFilesFrom(tt.args.searchPaths, tt.args.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("GlobImporter.resolveFilesFrom() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGlobImporter_AddAliasPrefix(t *testing.T) {
	type fields struct {
		JPaths         []string
		logger         *zap.Logger
		separator      string
		prefixa        map[string]string
		aliases        map[string]string
		lastFiles      []string
		cycleCache     map[globCacheKey]struct{}
		excludePattern string
	}
	type args struct {
		alias  string
		prefix string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &GlobImporter{
				JPaths:         tt.fields.JPaths,
				logger:         tt.fields.logger,
				separator:      tt.fields.separator,
				prefixa:        tt.fields.prefixa,
				aliases:        tt.fields.aliases,
				lastFiles:      tt.fields.lastFiles,
				cycleCache:     tt.fields.cycleCache,
				excludePattern: tt.fields.excludePattern,
			}
			if err := g.AddAliasPrefix(tt.args.alias, tt.args.prefix); (err != nil) != tt.wantErr {
				t.Errorf("GlobImporter.AddAliasPrefix() error = %v, wantErr %v", err, tt.wantErr)
			}
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

	type args struct {
		importedFrom string
		importedPath string
	}
	tests := []struct {
		name        string
		jpaths      []string
		args        args
		want        jsonnet.Contents
		wantFoundAt string
		wantErr     bool
	}{
		// ----------------------------------------------------- jpath handling
		{
			name:   "with jpath set",
			jpaths: []string{"testdata/globPlus"},
			args: args{
				importedFrom: "",
				importedPath: "glob+://*.libsonnet",
			},
			want:        jsonnet.MakeContents("(import 'testdata/globPlus/host.libsonnet')"),
			wantFoundAt: "./",
		},
		{
			name:   "with jpath set to a models folder inside and outside vendor folder - contents are merged",
			jpaths: []string{"testdata/globJPaths/vendor", "testdata/globJPaths/"},
			args: args{
				importedFrom: "",
				importedPath: "glob+://models/*.jsonnet",
			},
			want: jsonnet.MakeContents(
				"(import 'testdata/globJPaths/models/x.jsonnet')+(import 'testdata/globJPaths/vendor/models/y.jsonnet')",
			),
			wantFoundAt: "./",
		},
		//{
		//	name:   "with jpath set to a models folder inside vendor folder and same folder in cwd - contents are merged",
		//	jpaths: []string{"testvendor"},
		//	args: args{
		//		importedFrom: "",
		//		importedPath: "glob+://models/*.jsonnet",
		//	},
		//	want: jsonnet.MakeContents(
		//		"(import 'models/x.jsonnet')+(import 'vendor/models/y.jsonnet')",
		//	),
		//	wantFoundAt: "./",
		//},
		{
			name:   "with jpath set and same file found via glob - no duplication",
			jpaths: []string{"testdata/globPlus"},
			args: args{
				importedFrom: "",
				importedPath: "glob+://testdata/globPlus/*.libsonnet",
			},
			want:        jsonnet.MakeContents("(import 'testdata/globPlus/host.libsonnet')"),
			wantFoundAt: "./",
		},
		{
			name:   "with jpath set for cwd and file found via glob - even that is nonsens",
			jpaths: []string{"."},
			args: args{
				importedFrom: "",
				importedPath: "glob+://testdata/globPlus/*.libsonnet",
			},
			want:        jsonnet.MakeContents("(import 'testdata/globPlus/host.libsonnet')+(import 'testdata/globPlus/host.libsonnet')"),
			wantFoundAt: "./",
		},
		{
			name:   "without jpath set - should return error",
			jpaths: []string{},
			args: args{
				importedFrom: "",
				importedPath: "glob+://*.libsonnet",
			},
			want:        jsonnet.MakeContents(""),
			wantFoundAt: "./",
			wantErr:     true,
		},
		{
			name:   "without jpath set, but right path in import string",
			jpaths: []string{},
			args: args{
				importedFrom: "",
				importedPath: "glob+://testdata/globPlus/*.libsonnet",
			},
			want:        jsonnet.MakeContents("(import 'testdata/globPlus/host.libsonnet')"),
			wantFoundAt: "./",
			wantErr:     false,
		},
		{
			name:   "two jpath set and contents are merged",
			jpaths: []string{"testdata/globPlus", "testdata/globDot"},
			args: args{
				importedFrom: "",
				importedPath: "glob+://*.libsonnet",
			},
			want: jsonnet.MakeContents(
				"(import 'testdata/globDot/host.libsonnet')+(import 'testdata/globPlus/host.libsonnet')",
			),
			wantFoundAt: "./",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGlobImporter(tt.jpaths...)
			g.Logger(logger)
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

func TestGlobImporter_parse(t *testing.T) {
	type fields struct {
		JPaths         []string
		logger         *zap.Logger
		separator      string
		prefixa        map[string]string
		aliases        map[string]string
		lastFiles      []string
		cycleCache     map[globCacheKey]struct{}
		excludePattern string
	}
	type args struct {
		importedPath string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    string
		want1   string
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &GlobImporter{
				JPaths:         tt.fields.JPaths,
				logger:         tt.fields.logger,
				separator:      tt.fields.separator,
				prefixa:        tt.fields.prefixa,
				aliases:        tt.fields.aliases,
				lastFiles:      tt.fields.lastFiles,
				cycleCache:     tt.fields.cycleCache,
				excludePattern: tt.fields.excludePattern,
			}
			got, got1, err := g.parse(tt.args.importedPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("GlobImporter.parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GlobImporter.parse() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("GlobImporter.parse() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestGlobImporter_handle(t *testing.T) {
	type fields struct {
		JPaths         []string
		logger         *zap.Logger
		separator      string
		prefixa        map[string]string
		aliases        map[string]string
		lastFiles      []string
		cycleCache     map[globCacheKey]struct{}
		excludePattern string
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
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := GlobImporter{
				JPaths:         tt.fields.JPaths,
				logger:         tt.fields.logger,
				separator:      tt.fields.separator,
				prefixa:        tt.fields.prefixa,
				aliases:        tt.fields.aliases,
				lastFiles:      tt.fields.lastFiles,
				cycleCache:     tt.fields.cycleCache,
				excludePattern: tt.fields.excludePattern,
			}
			got, err := g.handle(tt.args.files, tt.args.prefix)
			if (err != nil) != tt.wantErr {
				t.Errorf("GlobImporter.handle() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GlobImporter.handle() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_createGlobDotImportsFrom(t *testing.T) {
	type args struct {
		resolvedFiles *orderedMap
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := createGlobDotImportsFrom(tt.args.resolvedFiles); got != tt.want {
				t.Errorf("createGlobDotImportsFrom() = %v, want %v", got, tt.want)
			}
		})
	}
}
