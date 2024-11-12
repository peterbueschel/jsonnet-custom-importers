// custom importers config
local importers = import 'config://set?onMissingFile=../simple/default.jsonnet';
local caller = importers + (import 'missing.file');

{
  caller: caller,
}
