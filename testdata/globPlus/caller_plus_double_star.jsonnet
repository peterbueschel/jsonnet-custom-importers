(import 'glob+://**/*.libsonnet') +
{
  checksum: super.checksum,
  imports: [std.thisFile] + super.imports,
}
