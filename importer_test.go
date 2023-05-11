package importer

import (
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/dominikbraun/graph"
	"github.com/dominikbraun/graph/draw"
	"github.com/google/go-jsonnet"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestMultiImporter_parseInFileConfigs(t *testing.T) {
	type args struct {
		rawQuery string
	}
	tests := []struct {
		name                  string
		wantLogLevel          string
		wantImportGraphFile   string
		args                  args
		wantEnableImportGraph bool
		wantErr               bool
		wantErrType           error
	}{
		{
			name: "empty_query",
			args: args{
				rawQuery: "",
			},
			wantImportGraphFile: importGraphFileName,
		},
		{
			name: "debug_level",
			args: args{
				rawQuery: "logLevel=debug",
			},
			wantImportGraphFile: importGraphFileName,
			wantLogLevel:        "debug",
		},
		{
			name: "info_level",
			args: args{
				rawQuery: "logLevel=info",
			},
			wantImportGraphFile: importGraphFileName,
			wantLogLevel:        "info",
		},
		{
			name: "unknown_level_error",
			args: args{
				rawQuery: "logLevel=unknown",
			},
			wantErr:             true,
			wantErrType:         ErrUnknownConfig,
			wantImportGraphFile: importGraphFileName,
			wantLogLevel:        "unknown",
		},
		{
			name: "combined_importGraph_debug",
			args: args{
				rawQuery: "logLevel=debug&importGraph=graph.gv",
			},
			wantImportGraphFile:   "graph.gv",
			wantLogLevel:          "debug",
			wantEnableImportGraph: true,
		},
		{
			name: "semicolon_error",
			args: args{
				rawQuery: "logLevel=debug;",
			},
			wantErr:             true,
			wantErrType:         ErrMalformedQuery,
			wantImportGraphFile: importGraphFileName,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMultiImporter()
			err := m.parseInFileConfigs(tt.args.rawQuery)
			if (err != nil) != tt.wantErr {
				t.Errorf("MultiImporter.parseInFileConfigs() %v", err)
				return
			}
			if tt.wantErr {
				assert.ErrorIs(t, err, tt.wantErrType)
			}

			assert.Equal(t, tt.wantLogLevel, m.logLevel)
			assert.Equal(t, tt.wantImportGraphFile, m.importGraphFile)
			assert.Equal(t, tt.wantEnableImportGraph, m.enableImportGraph)
		})
	}
}

func TestMultiImporter_InFileConfigs(t *testing.T) {
	wantGraphLines := []string{
		`strict digraph {`,
		``,
		``,
		``,
		`	"testdata/inFileConfigs/importGraph.jsonnet" [ shape="house",  weight=0 ];`,
		``,
		`	"testdata/inFileConfigs/importGraph.jsonnet" -> "caller.jsonnet" [  weight=1 ];`,
		``,
		`	"caller.jsonnet" [ shape="house",  weight=0 ];`,
		``,
		`	"caller.jsonnet" -> "testdata/inFileConfigs/caller.jsonnet" [  weight=1 ];`,
		``,
		`	"testdata/inFileConfigs/caller.jsonnet" [  weight=0 ];`,
		``,
		`	"testdata/inFileConfigs/caller.jsonnet" -> "libs/host.libsonnet" [  weight=3 ];`,
		``,
		`	"glob.stem+://libs/*.libsonnet" [ color="grey", fontcolor="grey", shape="rect", style="dashed",  weight=0 ];`,
		``,
		`	"glob.stem+://libs/*.libsonnet" -> "libs/host.libsonnet" [ color="grey", style="dashed",  weight=3 ];`,
		``,
		`	"libs/host.libsonnet" [ color="grey", fontcolor="grey", shape="rect", style="dashed",  weight=0 ];`,
		``,
		`	"libs/host.libsonnet" -> "testdata/inFileConfigs/libs/host.libsonnet" [  weight=3 ];`,
		``,
		`	"testdata/inFileConfigs/libs/host.libsonnet" [  weight=0 ];`,
		``,
		`	"." [ shape="invhouse",  weight=0 ];`,
		``,
		`	"." -> "testdata/inFileConfigs/importGraph.jsonnet" [  weight=0 ];`,
		``,
		`}`,
	}
	sort.Strings(wantGraphLines)

	tests := []struct {
		name         string
		callerFile   string
		wantLogLevel string
		wantGraph    []string
		wantErr      bool
	}{
		{
			name:         "logLevel_info",
			callerFile:   "testdata/inFileConfigs/logLevel_info.jsonnet",
			wantLogLevel: "info",
		},
		{
			name:         "logLevel_debug",
			callerFile:   "testdata/inFileConfigs/logLevel_debug.jsonnet",
			wantLogLevel: "debug",
		},
		{
			name:         "importGraph",
			callerFile:   "testdata/inFileConfigs/importGraph.jsonnet",
			wantLogLevel: "info",
			wantGraph:    wantGraphLines,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMultiImporter()
			fs := afero.NewMemMapFs()
			m.fs = fs

			vm := jsonnet.MakeVM()
			vm.Importer(m)
			_, err := vm.EvaluateFile(tt.callerFile)
			if (err != nil) != tt.wantErr {
				t.Errorf("vm.EvaluateFile(%s) %v", tt.callerFile, err)
				return
			}
			assert.Equal(t, tt.wantLogLevel, m.logLevel)
			if len(tt.wantGraph) > 0 {
				cnt, err := afero.ReadFile(fs, m.importGraphFile)
				if err != nil {
					t.Errorf("read importGraph in %s: %v", tt.callerFile, err)
					return
				}
				lines := strings.Split(string(cnt), "\n")
				sort.Strings(lines)
				assert.Equal(t, tt.wantGraph, lines)
			}
		})
	}
}

func createGraph(a, b string, counter int, withErr bool) graph.Graph[string, string] {
	testGraph := graph.New(
		graph.StringHash, graph.Tree(), graph.Directed(), graph.Weighted(),
	)
	_ = testGraph.AddVertex(a, graph.VertexAttribute("shape", "invhouse"))
	_ = testGraph.AddVertex(b, graph.VertexAttribute("shape", "house"))
	if withErr {
		_ = testGraph.AddEdge(a, b, graph.EdgeWeight(counter), graph.EdgeAttribute("color", "red"))
		return testGraph
	}
	_ = testGraph.AddEdge(a, b, graph.EdgeWeight(counter))
	return testGraph
}

func addRelativesToGraph(
	g graph.Graph[string, string], given, relative string, counter int, withErr bool,
) graph.Graph[string, string] {
	_ = g.AddVertex(relative)
	if withErr {
		_ = g.AddEdge(given, relative, graph.EdgeWeight(counter), graph.EdgeAttribute("color", "red"))
	}
	_ = g.AddEdge(given, relative, graph.EdgeWeight(counter))
	return g
}

func TestMultiImporter_findImportCycle(t *testing.T) {
	type args struct {
		importedFrom string
		importedPath string
	}

	type fields struct {
		importGraph   graph.Graph[string, string]
		importCounter int
	}

	tests := []struct {
		name        string
		args        args
		fields      fields
		want        graph.Graph[string, string]
		wantErr     bool
		wantErrType error
		showMe      bool
	}{
		{
			name: "empty_graph",
			args: args{
				importedFrom: "caller.jsonnet",
				importedPath: "host.libsonnet",
			},
			fields: fields{
				importGraph: graph.New(
					graph.StringHash, graph.Tree(), graph.Directed(), graph.Weighted(),
				),
				importCounter: 0,
			},
			want: createGraph("caller.jsonnet", "host.libsonnet", 0, false),
		},
		{
			name: "cycle_directly_on_self",
			args: args{
				importedFrom: "caller.jsonnet",
				importedPath: "caller.jsonnet",
			},
			fields: fields{
				importGraph: graph.New(
					graph.StringHash, graph.Tree(), graph.Directed(), graph.Weighted(),
				),
				importCounter: 0,
			},
			wantErr:     true,
			wantErrType: ErrImportCycle,
			want:        createGraph("caller.jsonnet", "caller.jsonnet", 0, true),
		},
		{
			name: "cycle_indirectly_through_third_file",
			args: args{
				importedFrom: "receiver.libsonnet",
				importedPath: "caller.jsonnet",
			},
			fields: fields{
				importGraph: addRelativesToGraph(
					createGraph("caller.jsonnet", "proxy.libsonnet", 0, false),
					"proxy.libsonnet", "receiver.libsonnet", 0, false,
				),
				importCounter: 0,
			},
			//
			// [caller.jsonnet] --> [proxy.libsonnet] --> [receiver.libsonnet]
			//          ^                                            /
			//          +-------------------------------------------+
			want: addRelativesToGraph(
				addRelativesToGraph(
					createGraph("caller.jsonnet", "proxy.libsonnet", 0, false),
					"proxy.libsonnet", "receiver.libsonnet", 0, false,
				),
				"receiver.libsonnet", "caller.jsonnet", 0, true,
			),
			wantErr:     true,
			wantErrType: ErrImportCycle,
		},
		{
			name: "cycle_indirectly_through_third_file_in_subfolder",
			args: args{
				importedFrom: "sub/receiver.libsonnet",
				importedPath: "../caller.jsonnet",
			},
			fields: fields{
				importGraph: addRelativesToGraph(
					createGraph("caller.jsonnet", "proxy.libsonnet", 0, false),
					"proxy.libsonnet", "sub/receiver.libsonnet", 0, false,
				),
				importCounter: 0,
			},
			//
			// [caller.jsonnet] --> [proxy.libsonnet] --> [sub/receiver.libsonnet]
			//          ^                                            /
			//          +-------------------------------------------+
			want: addRelativesToGraph(
				addRelativesToGraph(
					addRelativesToGraph(
						createGraph("caller.jsonnet", "proxy.libsonnet", 0, false),
						"proxy.libsonnet", "sub/receiver.libsonnet", 0, false,
					),
					"sub/receiver.libsonnet", "../caller.jsonnet", 0, false,
				),
				"../caller.jsonnet", "caller.jsonnet", 0, true,
			),
			wantErr:     true,
			wantErrType: ErrImportCycle,
			showMe:      false,
		},
		{
			name: "importedPath_is_relative",
			args: args{
				importedFrom: "testdata/caller.jsonnet",
				importedPath: "host.libsonnet",
			},
			fields: fields{
				importGraph: graph.New(
					graph.StringHash, graph.Tree(), graph.Directed(), graph.Weighted(),
				),
				importCounter: 0,
			},
			//
			// [testdata/caller.jsonnet] --> [host.libsonnet] --> [testdata/host.libsonnet]
			//
			want: addRelativesToGraph(
				createGraph("testdata/caller.jsonnet", "host.libsonnet", 0, false),
				"host.libsonnet", "testdata/host.libsonnet", 0, false,
			),
		},
		{
			name: "cycle_through_resolved_importedPath",
			args: args{
				//
				importedFrom: "testdata/caller.jsonnet",
				importedPath: "caller.jsonnet",
			},
			fields: fields{
				importGraph: graph.New(
					graph.StringHash, graph.Tree(), graph.Directed(), graph.Weighted(),
				),
				importCounter: 0,
			},
			//
			// [testdata/caller.jsonnet] --> [caller.jsonnet]
			//          ^                      /
			//          +---------------------+
			want: addRelativesToGraph(
				createGraph("testdata/caller.jsonnet", "caller.jsonnet", 0, false),
				"caller.jsonnet", "testdata/caller.jsonnet", 0, true,
			),
			wantErr:     true,
			wantErrType: ErrImportCycle,
		},
		{
			name: "cycle_indirectly_through_resolved_importPath",
			args: args{
				importedFrom: "testdata/host.libsonnet",
				importedPath: "caller.jsonnet",
			},
			fields: fields{
				importGraph: addRelativesToGraph(
					createGraph("testdata/caller.jsonnet", "host.libsonnet", 0, false),
					"host.libsonnet", "testdata/host.libsonnet", 0, false,
				),
				importCounter: 0,
			},
			//
			// [testdata/caller.jsonnet] --> [host.libsonnet] --> [testdata/host.libsonnet]
			//          ^                                            /
			//          +-------------------[caller.jsonnet]--------+
			want: addRelativesToGraph(
				addRelativesToGraph(
					addRelativesToGraph(
						createGraph("testdata/caller.jsonnet", "host.libsonnet", 0, false),
						"host.libsonnet", "testdata/host.libsonnet", 0, false,
					),
					"testdata/host.libsonnet", "caller.jsonnet", 0, false,
				),
				"caller.jsonnet", "testdata/caller.jsonnet", 0, true,
			),
			wantErr:     true,
			wantErrType: ErrImportCycle,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMultiImporter()
			m.importGraph = tt.fields.importGraph
			if tt.showMe {

				image, _ := m.fs.Create("init.gv")
				_ = draw.DOT(m.importGraph, image)

				image2, _ := m.fs.Create("want.gv")
				_ = draw.DOT(tt.want, image2)
			}

			fs := afero.NewMemMapFs()
			m.fs = fs
			err := m.findImportCycle(tt.args.importedFrom, tt.args.importedPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("MultiImporter.parseInFileConfigs() %v", err)
				return
			}
			if tt.showMe {
				image3, _ := m.fs.Create("got")
				_ = draw.DOT(tt.want, image3)
			}
			if tt.wantErr {
				assert.ErrorIs(t, err, tt.wantErrType)
			}

			want, _ := tt.want.AdjacencyMap()
			got, _ := m.importGraph.AdjacencyMap()
			assert.Equal(t, want, got)
		})
	}
}

func TestMultiImporter_Behavior(t *testing.T) {
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

	tests := []struct {
		name       string
		callerFile string
		want       string
		wantErr    bool
	}{
		// -------------------------------------------------------- error cases
		{
			name:       "glob_empty",
			callerFile: "testdata/errorCases/glob_empty.fake",
			want:       "",
			wantErr:    true,
		},
		{
			name:       "glob_no_results",
			callerFile: "testdata/errorCases/glob_no_results.fake",
			want:       "",
			wantErr:    true,
		},
		{
			name:       "glob_malformed_pattern",
			callerFile: "testdata/errorCases/glob_malformed_pattern.fake",
			want:       "",
			wantErr:    true,
		},
		// -------------------------------------------------------- glob.<?>://
		{
			name:       "glob_dot_path",
			callerFile: "testdata/globDot/caller_dot_path.jsonnet",
			want: `{
   "checksum": 1,
   "imports": [
      "testdata/globDot/caller_dot_path.jsonnet",
      "testdata/globDot/host.libsonnet"
   ],
   "names": [
      "host.libsonnet"
   ]
}
`,
		},
		{
			name:       "glob_dot_stem_double_star",
			callerFile: "testdata/globDot/caller_dot_stem_double_star.jsonnet",
			want: `{
   "checksum": 1,
   "imports": [
      "testdata/globDot/caller_dot_stem_double_star.jsonnet",
      "testdata/globDot/subfolder/subsubfolder/host.libsonnet"
   ],
   "names": [
      "host"
   ]
}
`,
		},
		{
			name:       "glob_dot_stem_double_star_plus",
			callerFile: "testdata/globDot/caller_dot_stem_double_star_plus.jsonnet",
			want: `{
   "checksum": 3,
   "imports": [
      "testdata/globDot/caller_dot_stem_double_star_plus.jsonnet",
      "testdata/globDot/host.libsonnet",
      "testdata/globDot/subfolder/host.libsonnet",
      "testdata/globDot/subfolder/subsubfolder/host.libsonnet"
   ],
   "names": [
      "host"
   ]
}
`,
		},
		// ----------------------------------------------------------- glob+://
		{
			name:       "glob_plus_single_star - no error",
			callerFile: "testdata/globPlus/caller_plus_single_star.jsonnet",
			want: `{
   "checksum": 1,
   "imports": [
      "testdata/globPlus/caller_plus_single_star.jsonnet",
      "testdata/globPlus/host.libsonnet"
   ]
}
`,
		},
		{
			name:       "glob_plus_single_star_continuous",
			callerFile: "testdata/globPlus/caller_plus_single_star_continuous.jsonnet",
			want: `{
   "checksum": 1,
   "imports": [
      "testdata/globPlus/caller_plus_single_star_continuous.jsonnet",
      "testdata/globPlus/caller_plus_single_star.jsonnet",
      "testdata/globPlus/host.libsonnet"
   ]
}
`,
		},
		{
			name:       "glob_plus_double_star - no error",
			callerFile: "testdata/globPlus/caller_plus_double_star.jsonnet",
			want: `{
   "checksum": 3,
   "imports": [
      "testdata/globPlus/caller_plus_double_star.jsonnet",
      "testdata/globPlus/host.libsonnet",
      "testdata/globPlus/subfolder/host.libsonnet",
      "testdata/globPlus/subfolder/subsubfolder/host.libsonnet"
   ]
}
`,
		},
		{
			name:       "glob_plus_double_star_continuous",
			callerFile: "testdata/globPlus/caller_plus_double_star_continuous.jsonnet",
			want: `{
   "checksum": 3,
   "imports": [
      "testdata/globPlus/caller_plus_double_star_continuous.jsonnet",
      "testdata/globPlus/caller_plus_double_star.jsonnet",
      "testdata/globPlus/host.libsonnet",
      "testdata/globPlus/subfolder/host.libsonnet",
      "testdata/globPlus/subfolder/subsubfolder/host.libsonnet"
   ]
}
`,
		},
		{
			name:       "glob_plus_diamondtest",
			callerFile: "testdata/globPlus/diamondtest.jsonnet",
			wantErr:    true,
			want:       ``,
		},
		// ------------------------------------------------------------ complex
		{
			name:       "complex test with multiple prefixa",
			callerFile: "example.jsonnet",
			want:       excpectedComplexOutput,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGlobImporter()
			err := g.AddAliasPrefix("stem", "glob.stem")
			if err != nil {
				t.Errorf("AddAliasPrefix() failed: %v", err)
				return
			}
			m := NewMultiImporter(g, NewFallbackFileImporter())
			m.Logger(logger)

			// _, file := filepath.Split(tt.callerFile)
			// m.SetImportGraphFile(fmt.Sprintf("%s.gv", file))
			vm := jsonnet.MakeVM()
			vm.Importer(m)
			got, err := vm.EvaluateFile(tt.callerFile)
			if (err != nil) != tt.wantErr {
				t.Errorf("vm.EvaluateFile(%s) %v", tt.callerFile, err)
				return
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

var excpectedComplexOutput = `{
   "dot": {
      "host": {
         "checksum": 3,
         "imports": [
            "testdata/globDot/host.libsonnet",
            "testdata/globDot/subfolder/host.libsonnet",
            "testdata/globDot/subfolder/subsubfolder/host.libsonnet"
         ]
      }
   },
   "dot_continuous": {
      "caller_dot_path": {
         "checksum": 1,
         "imports": [
            "testdata/globDot/caller_dot_path.jsonnet",
            "testdata/globDot/host.libsonnet"
         ],
         "names": [
            "host.libsonnet"
         ]
      },
      "caller_dot_stem_double_star": {
         "checksum": 1,
         "imports": [
            "testdata/globDot/caller_dot_stem_double_star.jsonnet",
            "testdata/globDot/subfolder/subsubfolder/host.libsonnet"
         ],
         "names": [
            "host"
         ]
      },
      "caller_dot_stem_double_star_plus": {
         "checksum": 3,
         "imports": [
            "testdata/globDot/caller_dot_stem_double_star_plus.jsonnet",
            "testdata/globDot/host.libsonnet",
            "testdata/globDot/subfolder/host.libsonnet",
            "testdata/globDot/subfolder/subsubfolder/host.libsonnet"
         ],
         "names": [
            "host"
         ]
      }
   },
   "plus": {
      "checksum": 3,
      "imports": [
         "testdata/globPlus/host.libsonnet",
         "testdata/globPlus/subfolder/host.libsonnet",
         "testdata/globPlus/subfolder/subsubfolder/host.libsonnet"
      ]
   },
   "plus_continuous": {
      "checksum": 8,
      "imports": [
         "testdata/globPlus/caller_plus_single_star_continuous.jsonnet",
         "testdata/globPlus/caller_plus_single_star.jsonnet",
         "testdata/globPlus/caller_plus_single_star.jsonnet",
         "testdata/globPlus/caller_plus_double_star_continuous.jsonnet",
         "testdata/globPlus/caller_plus_double_star.jsonnet",
         "testdata/globPlus/caller_plus_double_star.jsonnet",
         "testdata/globPlus/host.libsonnet",
         "testdata/globPlus/subfolder/host.libsonnet",
         "testdata/globPlus/subfolder/subsubfolder/host.libsonnet",
         "testdata/globPlus/host.libsonnet",
         "testdata/globPlus/subfolder/host.libsonnet",
         "testdata/globPlus/subfolder/subsubfolder/host.libsonnet",
         "testdata/globPlus/host.libsonnet",
         "testdata/globPlus/host.libsonnet"
      ]
   }
}
`
