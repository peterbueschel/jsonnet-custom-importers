// custom importers config
local importers = import 'config://set?importGraph=./testdata/inFileConfigs/graph.gv&logLevel=info';
local caller = importers + (import 'caller.jsonnet');

{
  caller: caller,
}
