// custom importers config
local importers = import 'config://set?onMissingFile="{missing: true}"';
local caller = importers + (import 'missing.file');

{
  caller: caller,
}
