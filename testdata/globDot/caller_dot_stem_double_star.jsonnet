local files = import 'glob.stem://**/*.libsonnet';
{
  names: std.objectFields(files),
  checksum+: std.foldl(
    function(sum, f) sum + files[f].checksum, std.objectFields(files), 0
  ),
  imports+: std.foldl(
    function(list, f) list + files[f].imports, std.objectFields(files), [std.thisFile]
  ),
}
