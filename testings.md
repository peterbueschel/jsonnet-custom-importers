# Behavior Tests

Tests are also documentation, therefore the following sections provide an overview of the behavior tests per prefix. The files can be found under the `testdata` folder and the code to run the tests is inside the `importer_test.go` inside the `TestMultiImporter_Behavior()` function.

## Prefix `glob.<?>` And `glob.<?>+`

### Test Folder Structure

```console
   globDot
   ├── caller_dot_path.jsonnet
   ├── caller_dot_stem_double_star.jsonnet
   ├── caller_dot_stem_double_star_plus.jsonnet
   ├── host.libsonnet
   └── subfolder
       ├── host.libsonnet
       └── subsubfolder
           └── host.libsonnet
```

### Test File(s) Content

- `<caller object>` used in each `caller_dot_*.jsonnet` files: 
     ```jsonnet
     local files = import ...;
     {
       checksum+: std.foldl(function(sum, f) sum + files[f].checksum, std.objectFields(files), 0),
       imports+: std.foldl(function(list, f) list + files[f].imports, std.objectFields(files), [std.thisFile]),
       names: std.objectFields(files),
     }
     ```
- `caller_dot_path.jsonnet` content: `local files = import 'glob.path://*.libsonnet'; <caller object>`
- `caller_dot_stem.jsonnet` content: `local files = import 'glob.stem://**/*.libsonnet'; <caller object>`
- `caller_dot_stem_plus.jsonnet` content: `local files = import 'glob.stem+://**/*.jsonnet'; <caller object>`
- `caller_dot_dir_parent_plus.jsonnet` content: `local files = import 'glob.parent+://**/*.jsonnet'; <caller object>`
- `host.libsonnet` & `subfolder/subhost.libsonnet` & `subfolder/subsubfolder/subsubhost.libsonnet` content: `{ checksum+: 1,   imports+: [std.thisFile] }`

### Test Cases

- `glob_dot_path`:
  - `caller_dot_path.jsonnet` imports `host.libsonnet` under `'host.libsonnet': import 'host.libsonnet'` via `glob.path://*.libsonnet`
  - expected json output (order in the `"imports"` field reflects the import chain; `"checksum"` comes from the `**/host.libsonnet`; `"names"` are the variable names): 
    
    ``` json
    {
       "checksum": 1,
       "imports": [
          "testdata/globDot/caller_dot_path.jsonnet",
          "testdata/globDot/host.libsonnet"
       ],
       "names": [
          "host.libsonnet"
       ]
    }
    
    ```
- `glob_dot_stem_double_star`:
	- `caller_dot_stem.jsonnet` imports only the last resolved file `testdata/globDot/subfolder/subsubfolder/host.libsonnet` under
	  `host: import 'testdata/globDot/subfolder/subsubfolder/host.libsonnet'`via `glob.stem://**/*.libsonnet`
	- expected json output (order in the `"imports"` field reflects the import chain; `"checksum"` comes from the `**/host.libsonnet`; `"names"` are the variable names): 
	  ```json
	  {
	     "checksum": 1,
	     "imports": [
	        "testdata/globDot/caller_dot_stem_double_star.jsonnet",
	        "testdata/globDot/subfolder/subsubfolder/host.libsonnet"
	     ],
	     "names": [
	        "host"
	     ]
	  }
	  ```
- `glob_dot_stem_double_star_plus`:
	- `caller_dot_stem_plus.jsonnet` imports all resolved files and merges them under a single variable 
	  `host: (import 'testdata/globDot/host.libsonnet') + (import 'testdata/globDot/subfolder/host.libsonnet') + (import 'testdata/globDot/subfolder/subsubfolder/host.libsonnet')`
	  via `glob.stem+://**/*.libsonnet`
	- expected json output (order in the `"imports"` field reflects the import chain; `"checksum"` comes from the `**/host.libsonnet`; `"names"` are the variable names): 
	  
	  ``` json
	  {
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
	  ```

## Prefix **`glob+`**

### Test Folder Structure

- ```console
  globPlus
  ├── caller_plus_double_star.jsonnet
  ├── caller_plus_double_star_continuous.jsonnet
  ├── caller_plus_single_star.jsonnet
  ├── caller_plus_single_star_continuous.jsonnet
  ├── host.libsonnet
  └── subfolder
      ├── host.libsonnet
      └── subsubfolder
          └── host.libsonnet
  ```

### Test File(s) Content

- `<caller object>` used in each `caller_plus_*.jsonnet` files: `(import ...) + { checksum: super.checksum, imports: [std.thisFile] + super.imports }`
- `caller_plus_double_star.jsonnet` content: `(import 'glob+://**/*.libsonnet') + <caller object>`
- `caller_plus_double_star_continuous.jsonnet` content: `(import 'glob+://**/*double*.jsonnet') + <caller object>`
- `caller_plus_single_star.jsonnet` content: `(import 'glob+://*.libsonnet') + <caller object>`
- `caller_plus_single_star_continuous.jsonnet` content: `(import 'glob+://*single*.jsonnet') + <caller object>`
- TODO `caller_plus_single_star_relative.jsonnet` content: `(import 'glob+://../*.libsonnet') + <caller object>`
- `host.libsonnet` & `subfolder/subhost.libsonnet` & `subfolder/subsubfolder/subsubhost.libsonnet` content: `{ checksum+: 1,   imports+: [std.thisFile] }`

### Test Cases:

- `glob_plus_single_star`:
	- `caller_plus_single_star.jsonnet` imports `host.libsonnet` via `glob+://*.libsonnet`
	- expected json output (order in the `"imports"` field reflects the import chain; `"checksum"` comes from the `**/host.libsonnet`): 
	  ```json
	  {
	     "checksum": 1,
	     "imports": [
	        "testdata/globPlus/caller_plus_single_star.jsonnet",
	        "testdata/globPlus/host.libsonnet"
	     ]
	  }
	  ```
- `glob_plus_single_star_continuous`:
	- `caller_plus_single_star_continuous.jsonnet` imports `caller_plus_single_star.jsonnet`, which in turn imports `host.libsonnet` via `glob+://*single*.libsonnet` and **not again** `caller_plus_single_star_continuous.jsonnet`
	- expected json output (order in the `"imports"` field reflects the import chain; `"checksum"` comes from the `**/host.libsonnet`): 
	  ```json
	  {
	     "checksum": 1,
	     "imports": [
	        "testdata/globPlus/caller_plus_single_star_continuous.jsonnet",
	        "testdata/globPlus/caller_plus_single_star.jsonnet",
	        "testdata/globPlus/host.libsonnet"
	     ]
	  }
	  ```
- `glob_plus_double_star`:
	- `caller_plus_double_star.jsonnet` imports `host.libsonnet` & `subfolder/subhost.libsonnet` & `subfolder/subsubfolder/subsubhost.libsonnet` via `glob+://**/*.libsonnet`
	- expected json output (order in the `"imports"` field reflects the import chain; `"checksum"` comes from the `**/host.libsonnet`): 
	  ```json
	  { 
	    "checksum": 3,
	    "imports": [
	       "testdata/globPlus/caller_plus_double_star.jsonnet",
	       "testdata/globPlus/host.libsonnet",
	       "testdata/globPlus/subfolder/host.libsonnet",
	       "testdata/globPlus/subfolder/subsubfolder/host.libsonnet"
	    ]
	  }
	  ```
- `glob_plus_double_star_continuous`:
	- `caller_plus_double_star_continuous.jsonnet` imports `caller_plus_double_star.jsonnet`, which in turn imports `host.libsonnet` & `subfolder/subhost.libsonnet` & `subfolder/subsubfolder/subsubhost.libsonnet` via `glob+://**/*double*.libsonnet` and **not again** `caller_plus_double_star_continuous.jsonnet`
	- expected json output (order in the `"imports"` field reflects the import chain; `"checksum"` comes from the `**/host.libsonnet`):
	  
	  ``` json
	  {
	     "checksum": 3,
	     "imports": [
	        "testdata/globPlus/caller_plus_double_star_continuous.jsonnet",
	        "testdata/globPlus/caller_plus_double_star.jsonnet",
	        "testdata/globPlus/host.libsonnet",
	        "testdata/globPlus/subfolder/host.libsonnet",
	        "testdata/globPlus/subfolder/subsubfolder/host.libsonnet"
	     ]
	  }
	  ```
