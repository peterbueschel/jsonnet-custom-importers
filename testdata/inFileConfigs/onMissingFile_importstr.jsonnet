// custom importers config
local useDefaultContent = import 'config://set?onMissingFile="[]"';
local caller_str = std.get(useDefaultContent, '', (importstr 'missing.file'));

{
  caller_str: caller_str,
}
