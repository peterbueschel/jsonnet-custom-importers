# Jsonnet Custom-Importers

[![Go Reference](https://pkg.go.dev/badge/github.com/peterbueschel/jsonnet-custom-importers.svg)](https://pkg.go.dev/github.com/peterbueschel/jsonnet-custom-importers)
[![Go Report Card](https://goreportcard.com/badge/github.com/peterbueschel/jsonnet-custom-importers)](https://goreportcard.com/report/github.com/peterbueschel/jsonnet-custom-importers)
[![Coverage Status](https://coveralls.io/repos/github/peterbueschel/jsonnet-custom-importers/badge.svg?branch=main)](https://coveralls.io/github/peterbueschel/jsonnet-custom-importers?branch=main)

- Extend the jsonnet `import` and `importstr` functions with the help of custom importers.
- Choose either a specific importer or use the `NewMultiImporter()` function to enable all together. The prefix of an import path will route to the right importer. As a fallback the default [go-jsonnet](https://github.com/google/go-jsonnet) `FileImporter` will be used.
- A custom importer can be set for a [jsonnet VM](https://pkg.go.dev/github.com/google/go-jsonnet?utm_source=godoc#VM) like:

```go
    m := NewMultiImporter()
    vm := jsonnet.MakeVM()
    vm.Importer(m)
```

---

## Custom Importers

The main idea is to add a kind of [intrinsic functionality](https://en.wikipedia.org/wiki/Intrinsic_function) into the import path string by adding extra strings. The custom importers can parse these extra strings and act on them.

The pattern for the import path is

```jsonnet
import '<importer prefix><separator><glob-pattern or filepath or url>'
```

⚠️ The default `<separator>` is `://`

<details>
  <summary>Example patterns</summary>


#### Original [go-jsonnet](https://github.com/google/go-jsonnet/blob/master/imports.go#L219) FileImporter

```jsonnet
import 'example.jsonnet'
```

where the `<importer prefix>` and the `<separator>`  are empty strings. The `<filepath>` is `example.jsonnet`

#### Custom GlobImporter

```jsonnet
import 'glob.stem+://**/*.jsonnet'
```

where `glob.stem+` is one of the possible `<importer prefixa>`, the `://` is the `<separator>` between the intrinsic functions and the `<glob pattern>` (here: `**/*.jsonnet`)

</details>


### List of available custom importers and the supported prefixa

| Name            | prefix in `import` path               | prefix in `importstr` path                   | extra intrinsic functionality                                                            |
| ----            | ---                                   | ---                                          | ---                                                                                      |
| `MultiImporter` | any - will address the right importer | any                                          |                                                                                          |
| `GlobImporter`  | `glob.<?>`, `glob.<?>+`, `glob+`      | `glob-str.<?>`, `glob-str.<?>+`, `glob-str+` | a `!` after the prefix followed by another `<globpattern>`  can be used to exclude files |

---

## MultiImporter

- This importer **includes all custom importers** and as fallback the default [go-jsonnet](https://github.com/google/go-jsonnet) `FileImporter`. The *MultiImporter* tries to find the right custom importer with the help of the `<importer prefix>`. If it found one, the import string will be forwarded to this custom importer, which in turn takes care of the string.
- Optionally, custom importers can be chosen via: 

``` go
  m := NewMultiImporter(NewGlobImporter(), NewFallbackFileImporter())
```

## GlobImporter

- Is a custom importer, which:
	- **Imports multiple files at once** with [glob patterns](https://en.wikipedia.org/wiki/Glob_(programming)) handled by the [doublestar](https://github.com/bmatcuk/doublestar) library.
	- Supports **Continuous** imports: If inside the resolved files other glob-patterns will be found, the *GlobImporter* will also take these *glob-imports* and resolves the underlying files.
	- Can **Exclude** imports: use the `!<glob pattern>` right after a prefix to exclude files from further handlings.
- Activate the _glob-import_ via the prefix **`glob.<?>`**  or **`glob.<?>+`** to get the content of the resolved files as object. The content of each file will be available under its resolved **path**, **file**name, **stem** (filename with file extension) or **dir**name. (see also table in section "Prefix `glob.<?>` And `glob.<?>+`")
- Use the prefix **`glob+`** to merge the returned imports. (similar to the jsonnet `+:` functionality)



### Examples

(More examples can be found in the [testings](testings.md) file.)

Folder structure:

``` console
models
├── blackbox_exporter.json
├── blackbox_exporter.libsonnet
├── node_exporter.json
├── node_exporter.libsonnet
├── development
│   └── grafana.libsonnet
├── production
│   ├── victor_ops.libsonnet
│   └── grafana.libsonnet
└── wavefront.libsonnet
```


<details>
  <summary><h4>Prefix `glob.<?>` And `glob.<?>+`</h4></summary>

- Each resolved file, which matched the glob pattern, will be handled individually and will be available in the code under a specific variable name. The variable name can be specified in the `<?>` part.
- `<?>` can be one of the following options:
  
  | option       | example result |
  |------------|------------------|
  | `path`       | `/foo/bar/baa.jsonnet` |
  | `file`   | `baa.jsonnet`        |
  | `stem`       | `baa`             |
  | `dir`        | `/foo/bar/`        |

- ⚠️ On colliding `file`|`stem`|`dir` -names, only the last resolved result in the hierarchy (shortest path first) will be used. Use the `glob.<?>+` (extra `+`) prefix to merge colliding names instead. The imports will be merged in hierarchical and [lexicographical](https://pkg.go.dev/sort#Strings) order similar to `glob+`. (also note: `glob.path` and `glob.path+` are the same)

##### Example Input `glob.path`

``` jsonnet
import 'glob://models/**/*.libsonnet';
```

##### Example Result `glob.path`

Code which will be evaluated in jsonnet:
``` jsonnet
 {
   'models/blackbox_exporter.libsonnet': import 'models/blackbox_exporter.libsonnet',
   'models/node_exporter.libsonnet': import 'models/node_exporter.libsonnet',
   'models/wavefront.libsonnet': import 'models/wavefront.libsonnet',
   'models/development/grafana.libsonnet': import 'models/development/grafana.libsonnet',
   'models/production/grafana.libsonnet': import 'models/production/grafana.libsonnet',
   'models/production/victor_ops.libsonnet': import 'models/production/victor_ops.libsonnet',
 }
```

##### Example Input `glob.stem`

```jsonnet
import 'glob.stem://models/**/*.libsonnet'
```

##### Example Result `glob.stem`

Code which will be evaluated in jsonnet:
``` jsonnet
  {
    blackbox_exporter: import 'models/blackbox_exporter.libsonnet',
    node_exporter: import 'models/node_exporter.libsonnet',
    wavefront: import 'models/wavefront.libsonnet',
    grafana: import 'models/production/grafana.libsonnet',
    victor_ops: import 'models/production/victor_ops.libsonnet',
  }
```

##### Example Input `glob.stem+`

```jsonnet
import 'glob.stem+://models/**/*.libsonnet'
```

##### Example Result `glob.stem+`

Code which will be evaluated in jsonnet:
```jsonnet
 {
   blackbox_exporter: import 'models/blackbox_exporter.libsonnet',
   node_exporter: import 'models/node_exporter.libsonnet',
   wavefront: import 'models/wavefront.libsonnet',
   grafana: (import 'models/development/grafana.libsonnet') + (import 'models/production/grafana.libsonnet'),
   victor_ops: import 'models/production/victor_ops.libsonnet',
 }
```

##### Example Input `glob.stem+!`

```jsonnet
import 'glob.stem+!models/**/*grafana*://models/**/*.libsonnet'
```

##### Example Result `glob.stem+!`

```jsonnet
{
  blackbox_exporter: import 'models/blackbox_exporter.libsonnet',
  node_exporter: import 'models/node_exporter.libsonnet',
  wavefront: import 'models/wavefront.libsonnet',
 
  victor_ops: import 'models/production/victor_ops.libsonnet',
}
```

</details>


<details>
  <summary><h4>Prefix `glob+`</h4></summary>


These files will be merged in the hierarchical (shortest path first) and [lexicographical](https://pkg.go.dev/sort#Strings) order.

##### Example Input

``` jsonnet
import 'glob+://models/**/*.libsonnet'
```

#### Example Result

Code which will be evaluated in jsonnet:
``` jsonnet
(import 'models/blackbox_exporter.libsonnet') +
(import 'models/node_exporter.libsonnet') +
(import 'models/wavefront.libsonnet') +
(import 'models/sub-folder-2/grafana.libsonnet')
```

</details>


## Options

### Logging

Enable/add a [zap.Logger](https://github.com/uber-go/zap) via `<Importer>.Logger()` per Importer or just use the this method on the `MultiImporter` instance to enable this of all underlying custom importers:

```go
import (
  ...
  "go.uber.org/zap"
)
...
l := zap.Must(zap.NewDevelopment()) // or use zap.NewProduction() to avoid debug messages
m := NewMultiImporter()
m.Logger(l)
...
```

### Aliases

Add an alias for an importer prefix, like:
```go
 g := NewGlobImporter()
 if err := g.SetAliasPrefix("glob", "glob.stem+"); err != nil {
   return err
 }
 m := NewMultiImporter(g)
```

The `SetAliasPrefix()` can be used multiple times, whereby only the last setting for an alias-prefix pair will be used.

## Dependencies

- https://github.com/google/go-jsonnet the reason for everything :-)
- https://github.com/bmatcuk/doublestar support for double star (`**`) glob patterns
- https://github.com/uber-go/zap for structured logging

## Other Projects

- another glob–importer https://qbec.io/reference/jsonnet-glob-importer/
	- example usage https://github.com/splunk/qbec/blob/main/vm/internal/importers/glob_test.go#L32

## Follow-Up Tasks

- [X] **importstr**: add support for `importstr`
- [X] **Ignore paths**: add support in the GlobImporter for ignore paths
- [X] **Alias**: add a prefix to a custom importer via `<Importer>.AddPrefix(string)`
- [ ] **HTTP** support: loads single files per url
- [ ] **Git** support: loads files in branches from repositories
