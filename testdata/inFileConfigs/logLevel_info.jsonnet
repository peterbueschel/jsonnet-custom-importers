// custom importers config
local importers = import 'config://set?logLevel=info';
local caller = importers + (import 'caller.jsonnet');

{
  caller: caller,
}
