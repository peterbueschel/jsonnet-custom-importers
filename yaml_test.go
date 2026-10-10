package importer

import (
	"path/filepath"
	"testing"

	"github.com/google/go-jsonnet"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func evalYamlFile(y *YamlImporter, file string) (string, error) {
	vm := jsonnet.MakeVM()
	vm.Importer(NewMultiImporter(y, NewGlobImporter(), NewFallbackFileImporter()))

	return vm.EvaluateFile(filepath.Join("testdata/yaml", file))
}

func TestYamlImporter_Behavior(t *testing.T) {
	jpaths := []string{"testdata/yaml/lib"}

	tests := []struct {
		name    string
		file    string
		setup   func(*YamlImporter)
		want    string
		wantErr error
		errText string
	}{
		{
			name: "plain_yaml_without_imports",
			file: "caller_plain.jsonnet",
			want: `{"list":[1,2],"name":"plain"}`,
		},
		{
			name: "mixed_imports_relative_and_jpath",
			file: "caller_mixed.jsonnet",
			want: `{"child":true,"fromJPath":true,"fromJsonnet":true,"name":"mixed","shared":"child"}`,
		},
		{
			name: "recursion",
			file: "caller_recursion.jsonnet",
			want: `{"leaf":1,"mid":1,"top":1}`,
		},
		{
			name:    "cycle",
			file:    "caller_cycle.jsonnet",
			wantErr: ErrImportCycle,
		},
		{
			name:    "self_cycle",
			file:    "caller_self_cycle.jsonnet",
			wantErr: ErrImportCycle,
		},
		{
			name:    "missing_yaml_import",
			file:    "caller_missing_yaml.jsonnet",
			errText: "no match locally or in the Jsonnet library paths",
		},
		{
			name:    "missing_jsonnet_import",
			file:    "caller_missing_jsonnet.jsonnet",
			errText: "nothere.libsonnet",
		},
		{
			name:    "missing_file",
			file:    "caller_missing_file.jsonnet",
			errText: "no match locally or in the Jsonnet library paths",
		},
		{
			name: "custom_key_and_wrapper",
			file: "caller_custom.jsonnet",
			setup: func(y *YamlImporter) {
				y.ImportsKey("include")
				y.Wrapper("function(base, doc, file) { base: base, file: file, doc: doc }")
			},
			want: `{"base":{"fromJsonnet":true,"shared":"jsonnet"},` +
				`"doc":{"include":["base.libsonnet"],"x":1},"file":"custom.yaml"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			y := NewYamlImporter(jpaths...)
			if tt.setup != nil {
				tt.setup(y)
			}

			got, err := evalYamlFile(y, tt.file)
			if tt.wantErr != nil || tt.errText != "" {
				require.Error(t, err)

				if tt.wantErr != nil {
					assert.Contains(t, err.Error(), tt.wantErr.Error())
				}

				assert.Contains(t, err.Error(), tt.errText)

				return
			}

			require.NoError(t, err)
			assert.JSONEq(t, tt.want, got)
		})
	}
}

func TestYamlImporter_Contents(t *testing.T) {
	y := NewYamlImporter()

	c, foundAt, err := y.Import("testdata/yaml/x.jsonnet", "yaml://plain.yaml")
	require.NoError(t, err)
	assert.Equal(t, "std.parseYaml(importstr 'plain.yaml')", c.String())
	assert.Contains(t, foundAt, "plain.yaml")

	c, _, err = y.Import("testdata/yaml/x.jsonnet", "yaml://mixed.yaml")
	require.NoError(t, err)
	assert.Equal(t,
		"(function(base, doc, file) base + { [k]: doc[k] for k in std.objectFields(doc) if k != 'imports' })("+
			"base = (import 'base.libsonnet') + (import 'yaml://sub/child.yml') + (import 'yaml://lib.yaml'), "+
			"doc = std.parseYaml(importstr 'mixed.yaml'), file = 'mixed.yaml')",
		c.String())
}

func TestYamlImporter_GlobMatches(t *testing.T) {
	tests := []struct {
		name string
		file string
		want string
	}{
		{
			name: "glob_plus_mixed_matches",
			file: "caller_glob.jsonnet",
			want: `{"a":1,"b":2,"c":3,"d":4,"fromJsonnet":true,"shared":"jsonnet"}`,
		},
		{
			name: "glob_stem_mixed_matches",
			file: "caller_glob_stem.jsonnet",
			want: `{"a":{"a":1},"b":{"b":2},"c":{"c":3},"d":{"d":4,"fromJsonnet":true,"shared":"jsonnet"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := evalYamlFile(NewYamlImporter(), tt.file)
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, got)
		})
	}
}

func TestGlobImporter_handleYaml(t *testing.T) {
	files := []string{"a.libsonnet", "b.yaml", "c.yml"}

	g := NewGlobImporter()
	got, err := g.handle(files, "glob+")
	require.NoError(t, err)
	assert.Equal(t, "(import 'a.libsonnet')+(import 'b.yaml')+(import 'c.yml')", got)

	// registering a YamlImporter enables the yaml:// prefix for glob matches.
	NewMultiImporter(NewYamlImporter(), g, NewFallbackFileImporter())

	got, err = g.handle(files, "glob+")
	require.NoError(t, err)
	assert.Equal(t, "(import 'a.libsonnet')+(import 'yaml://b.yaml')+(import 'yaml://c.yml')", got)

	got, err = g.handle(files, "glob-str+")
	require.NoError(t, err)
	assert.Equal(t, "(importstr 'a.libsonnet')+(importstr 'b.yaml')+(importstr 'c.yml')", got)
}

func TestYamlImporter_WrapperForPlainFile(t *testing.T) {
	y := NewYamlImporter()
	y.Wrapper("function(base, doc, file) { base: base, doc: doc, file: file }")

	c, _, err := y.Import("testdata/yaml/x.jsonnet", "yaml://plain.yaml")
	require.NoError(t, err)
	assert.Equal(t,
		"(function(base, doc, file) { base: base, doc: doc, file: file })("+
			"base = {}, doc = std.parseYaml(importstr 'plain.yaml'), file = 'plain.yaml')",
		c.String())

	got, err := evalYamlFile(y, "caller_plain.jsonnet")
	require.NoError(t, err)
	assert.JSONEq(t, `{"base":{},"doc":{"list":[1,2],"name":"plain"},"file":"plain.yaml"}`, got)
}

func TestYamlImporter_QuotedImportsKey(t *testing.T) {
	y := NewYamlImporter()
	y.ImportsKey("it's")

	c, _, err := y.Import("testdata/yaml/x.jsonnet", "yaml://plain.yaml")
	require.NoError(t, err)
	assert.Equal(t, "std.parseYaml(importstr 'plain.yaml')", c.String())

	assert.Equal(t, `'it\'s'`, quote(y.key))
}

func TestYamlImporter_ImportGraph(t *testing.T) {
	y := NewYamlImporter("testdata/yaml/lib")
	m := NewMultiImporter(y, NewGlobImporter(), NewFallbackFileImporter())
	m.fs = afero.NewMemMapFs()
	m.SetImportGraphFile("graph.gv")

	vm := jsonnet.MakeVM()
	vm.Importer(m)

	_, err := vm.EvaluateFile("testdata/yaml/caller_mixed.jsonnet")
	require.NoError(t, err)

	edges, err := m.importGraph.Edges()
	require.NoError(t, err)

	type edge struct{ from, to string }

	got := map[edge]bool{}
	for _, e := range edges {
		got[edge{e.Source, e.Target}] = true
	}

	assert.True(t, got[edge{"testdata/yaml/caller_mixed.jsonnet", foundAt("testdata/yaml/mixed.yaml")}])
	assert.True(t, got[edge{foundAt("testdata/yaml/mixed.yaml"), foundAt("testdata/yaml/sub/child.yml")}])
	assert.True(t, got[edge{foundAt("testdata/yaml/mixed.yaml"), "base.libsonnet"}])

	dot, err := afero.ReadFile(m.fs, "graph.gv")
	require.NoError(t, err)
	assert.Contains(t, string(dot), "mixed.yaml#yaml")
}
