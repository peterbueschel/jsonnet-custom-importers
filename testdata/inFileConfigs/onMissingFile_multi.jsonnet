// custom importers config
local useDefaultContent = import 'config://set?onMissingFile="[]"';
local caller_str = std.get(useDefaultContent, '', (importstr 'missing.file'));
local caller = std.get(useDefaultContent, '', (import 'missing.file'));

local useDefaultFile = import 'config://set?onMissingFile=../simple/default.jsonnet';
local caller_str_file = std.get(useDefaultFile, '', (importstr 'missing.file'));
local caller_file = std.get(useDefaultFile, '', (import 'missing.file'));

{
  caller: caller,
  caller_str: caller_str,

  caller_file: caller_file,
  caller_str_file: caller_str_file,
}
