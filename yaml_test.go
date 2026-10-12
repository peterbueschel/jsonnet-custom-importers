package importer

import (
	"encoding/json"
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
			name:    "multi_document_yaml",
			file:    "caller_multi_document.jsonnet",
			wantErr: ErrMalformedImport,
			errText: "multi-document YAML is not supported",
		},
		{
			name: "glob_imports",
			file: "caller_glob_imports.jsonnet",
			want: `{"fromLibsonnet":true,"yamlA":1,"yamlB":2}`,
		},
		{
			name: "custom_key_and_wrapper",
			file: "caller_custom.jsonnet",
			setup: func(y *YamlImporter) {
				y.ImportsKey("include")
				y.Wrapper("function(base, doc, file) { base: base, file: file, doc: doc }")
			},
			want: `{"base":{"fromJsonnet":true,"shared":"jsonnet"},` +
				`"doc":{"include":["base.libsonnet"],"x":1},"file":"testdata/yaml/custom.yaml"}`,
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
		"(function(base, doc, file, refs={}) base + { [k]: doc[k] for k in std.objectFields(doc) if k != 'imports' })("+
			"base = (import 'base.libsonnet') + (import 'yaml://sub/child.yml') + (import 'yaml://lib.yaml'), "+
			"doc = std.parseYaml(importstr 'mixed.yaml'), file = 'testdata/yaml/mixed.yaml')",
		c.String())
}

func TestYamlImporter_RepeatedImports(t *testing.T) {
	y := NewYamlImporter("testdata/yaml")
	vm := jsonnet.MakeVM()
	vm.Importer(NewMultiImporter(y, NewGlobImporter(), NewFallbackFileImporter()))

	got, err := vm.EvaluateAnonymousSnippet(
		"caller.jsonnet",
		"[import 'yaml://plain.yaml', import 'yaml://plain.yaml']",
	)
	require.NoError(t, err)
	assert.JSONEq(t, `[{"list":[1,2],"name":"plain"},{"list":[1,2],"name":"plain"}]`, got)
}

func TestYamlImporter_Reset(t *testing.T) {
	y := NewYamlImporter()
	y.fs = afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(y.fs, "doc.yaml", []byte("imports: [first.libsonnet]\n"), 0o600))

	first, _, err := y.Import("", "yaml://doc.yaml")
	require.NoError(t, err)
	assert.Contains(t, first.String(), "import 'first.libsonnet'")

	require.NoError(t, afero.WriteFile(y.fs, "doc.yaml", []byte("imports: [second.libsonnet]\n"), 0o600))
	cached, _, err := y.Import("", "yaml://doc.yaml")
	require.NoError(t, err)
	assert.Equal(t, first.String(), cached.String())

	y.Reset()
	updated, _, err := y.Import("", "yaml://doc.yaml")
	require.NoError(t, err)
	assert.Contains(t, updated.String(), "import 'second.libsonnet'")
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

func TestYamlImporter_ToImport(t *testing.T) {
	y := NewYamlImporter()
	assert.Equal(t, "glob+://sub/*.yaml", y.toImport("project.yaml", "glob+://sub/*.yaml"))
	assert.Equal(t, "glob+://sub/*.libsonnet", y.toImport("project.yaml", "glob+://sub/*.libsonnet"))
	assert.Equal(t, "yaml://sub/file.yaml", y.toImport("project.yaml", "sub/file.yaml"))
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
			"base = {}, doc = std.parseYaml(importstr 'plain.yaml'), file = 'testdata/yaml/plain.yaml')",
		c.String())

	got, err := evalYamlFile(y, "caller_plain.jsonnet")
	require.NoError(t, err)
	assert.JSONEq(t, `{"base":{},"doc":{"list":[1,2],"name":"plain"},"file":"testdata/yaml/plain.yaml"}`, got)
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

func TestYamlImporter_IgnoreImportCycles(t *testing.T) {
	// imports are lazy: the cycle is only found if the wrapper uses base.
	for _, tt := range []struct {
		name    string
		wrapper string
		ignore  bool
		wantErr bool
	}{
		{name: "cycle_error_by_default", wrapper: "function(base, doc, file) base + doc", wantErr: true},
		{name: "cycle_ignored", wrapper: "function(base, doc, file) doc", ignore: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			y := NewYamlImporter()
			y.Wrapper(tt.wrapper)
			m := NewMultiImporter(y, NewGlobImporter(), NewFallbackFileImporter())
			m.fs = afero.NewMemMapFs()

			if tt.ignore {
				m.IgnoreImportCycles()
			}

			vm := jsonnet.MakeVM()
			vm.Importer(m)

			got, err := vm.EvaluateFile("testdata/yaml/caller_cycle.jsonnet")
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), ErrImportCycle.Error())

				return
			}

			require.NoError(t, err)
			assert.JSONEq(t, `{"a":1,"imports":["b.yaml"]}`, got)
		})
	}
}

func TestYamlImporter_FileRefs(t *testing.T) {
	newImporter := func() *YamlImporter {
		y := NewYamlImporter()
		y.FileRefs("project.components[].file")
		y.Wrapper("function(base, doc, file, refs={}) { file: file, refs: refs }")

		return y
	}

	t.Run("entries_relative_to_the_yaml_file", func(t *testing.T) {
		got, err := evalYamlFile(newImporter(), "caller_refs.jsonnet")
		require.NoError(t, err)

		var out struct {
			File string
			Refs map[string]struct{ File string }
		}
		require.NoError(t, json.Unmarshal([]byte(got), &out))

		p := "testdata/yaml/refs/"
		assert.Equal(t, p+"app.yaml", out.File)
		assert.Equal(t, map[string]struct{ File string }{
			p + "app.yaml::a.yaml":            {p + "a.yaml"},
			p + "app.yaml::b.jsonnet":         {""},
			p + "app.yaml::sub/c.yml":         {p + "sub/c.yml"},
			p + "app.yaml::glob+://sub/*.yml": {p + "sub/c.yml"},
		}, out.Refs)
	})

	t.Run("code", func(t *testing.T) {
		c, _, err := newImporter().Import("testdata/yaml/x.jsonnet", "yaml://refs/app.yaml")
		require.NoError(t, err)
		assert.Contains(t, c.String(), "refs = { 'testdata/yaml/refs/app.yaml::a.yaml': (import 'yaml://a.yaml'), "+
			"'testdata/yaml/refs/app.yaml::b.jsonnet': (import 'b.jsonnet'), ")
	})

	t.Run("missing_file_names_the_yaml_file", func(t *testing.T) {
		_, err := evalYamlFile(newImporter(), "caller_refs_missing.jsonnet")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nothere.yaml")
		assert.Contains(t, err.Error(), "testdata/yaml/refs/missing.yaml")
	})

	t.Run("not_set", func(t *testing.T) {
		y := NewYamlImporter()
		y.Wrapper("function(base, doc, file) file")

		got, err := evalYamlFile(y, "caller_refs.jsonnet")
		require.NoError(t, err)
		assert.JSONEq(t, `"testdata/yaml/refs/app.yaml"`, got)
	})
}

func TestYamlImporter_JsonnetEntryWrapper(t *testing.T) {
	y := NewYamlImporter()
	y.JsonnetEntryWrapper("function(x, f) { entry: f, value: x }")
	y.Wrapper("function(base, doc, file, refs={}) base + doc { imports:: [] }")

	c, _, err := y.Import("testdata/yaml/x.jsonnet", "yaml://entrywrap/top.yaml")
	require.NoError(t, err)
	assert.Equal(t,
		"(function(base, doc, file, refs={}) base + doc { imports:: [] })("+
			"base = (function(x, f) { entry: f, value: x })((import 'model.libsonnet'), 'testdata/yaml/entrywrap/model.libsonnet')"+
			" + (import 'yaml://other.yaml'), doc = std.parseYaml(importstr 'top.yaml'), file = 'testdata/yaml/entrywrap/top.yaml')",
		c.String())

	got, err := evalYamlFile(y, "caller_entrywrap.jsonnet")
	require.NoError(t, err)
	assert.JSONEq(t, `{"entry":"testdata/yaml/entrywrap/model.libsonnet","name":"top","other":true,"value":{"model":1}}`, got)
}

func TestYamlImporter_NoJsonnetEntryWrapper(t *testing.T) {
	y := NewYamlImporter()

	c, _, err := y.Import("testdata/yaml/x.jsonnet", "yaml://entrywrap/top.yaml")
	require.NoError(t, err)
	assert.Contains(t, c.String(), "base = (import 'model.libsonnet') + (import 'yaml://other.yaml')")
}

func TestYamlImporter_JsonnetEntryWrapperSubfolderAndNested(t *testing.T) {
	y := NewYamlImporter()
	y.JsonnetEntryWrapper("function(x, f) x")
	y.Wrapper("function(base, doc, file, refs={}) base + doc { imports:: [] }")

	got, err := evalYamlFile(y, "caller_entrywrap_sub.jsonnet")
	require.NoError(t, err)
	assert.JSONEq(t, `{"nested":true,"sub":true}`, got)

	c, _, err := y.Import("testdata/yaml/x.jsonnet", "yaml://entrywrap/subtop.yaml")
	require.NoError(t, err)
	assert.Contains(t, c.String(), "((import 'sub/model.libsonnet'), 'testdata/yaml/entrywrap/sub/model.libsonnet')")

	c, _, err = y.Import("testdata/yaml/x.jsonnet", "yaml://entrywrap/sub/nested.yaml")
	require.NoError(t, err)
	assert.Contains(t, c.String(), "((import 'model.libsonnet'), 'testdata/yaml/entrywrap/sub/model.libsonnet')")
}

func TestYamlImporter_JsonnetEntryWrapperNestedResult(t *testing.T) {
	y := NewYamlImporter()
	y.JsonnetEntryWrapper("function(x, f) { path: f, v: x }")
	y.Wrapper("function(base, doc, file, refs={}) { file: file, base: base }")

	got, err := evalYamlFile(y, "caller_entrywrap_sub.jsonnet")
	require.NoError(t, err)
	assert.JSONEq(t, `{"file":"testdata/yaml/entrywrap/subtop.yaml","base":{"path":"testdata/yaml/entrywrap/sub/model.libsonnet","v":{"sub":true},"file":"testdata/yaml/entrywrap/sub/nested.yaml","base":{"path":"testdata/yaml/entrywrap/sub/model.libsonnet","v":{"sub":true}}}}`, got)
}

func TestYamlImporter_JsonnetEntryWrapperScheme(t *testing.T) {
	y := NewYamlImporter()
	y.JsonnetEntryWrapper("function(x, f) { path: f, v: x }")

	c, _, err := y.Import("testdata/yaml/x.jsonnet", "yaml://entrywrap/globtop.yaml")
	require.NoError(t, err)
	assert.Contains(t, c.String(), "((import 'glob+://globdir/*.libsonnet'), 'glob+://globdir/*.libsonnet')")

	got, err := evalYamlFile(y, "caller_entrywrap_glob.jsonnet")
	require.NoError(t, err)
	assert.JSONEq(t, `{"path":"glob+://globdir/*.libsonnet","v":{"g1":1}}`, got)
}

func TestYamlImporter_JsonnetEntryWrapperMissingFile(t *testing.T) {
	y := NewYamlImporter()
	y.JsonnetEntryWrapper("function(x, f) x")

	_, err := evalYamlFile(y, "caller_missing_jsonnet.jsonnet")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nothere.libsonnet")
}
