package importer

import (
	"os"
	"testing"

	"github.com/google/go-jsonnet"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

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
			name:       "glob_plus_single_star",
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
			name:       "glob_plus_double_star",
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
			g.Logger(logger)

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
