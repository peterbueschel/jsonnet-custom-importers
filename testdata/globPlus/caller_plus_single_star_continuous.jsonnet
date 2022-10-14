(import 'glob+://*single*.jsonnet') +
{
  checksum: super.checksum,
  imports: [std.thisFile] + super.imports,
}
