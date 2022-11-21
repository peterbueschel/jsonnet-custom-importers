// custom importers config
local importers = import 'config://set?logLevel=debug';
local caller = importers + (import 'caller.jsonnet');

{
  caller: caller,
}
