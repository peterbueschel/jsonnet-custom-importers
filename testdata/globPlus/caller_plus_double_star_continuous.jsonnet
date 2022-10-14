(import 'glob+://**/*double*.jsonnet') +
{
  checksum: super.checksum,
  imports: [std.thisFile] + super.imports,
}
