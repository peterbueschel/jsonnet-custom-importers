local plus_continuous = import 'glob+!**/diamond*.jsonnet://testdata/globPlus/**/*.jsonnet';
local plus = import 'glob+://testdata/globPlus/**/*.libsonnet';

local dot = import 'glob.stem+://testdata/globDot/**/*.libsonnet';
// alias stem -> glob.stem
local dot_continuous = import 'stem://testdata/globDot/**/*.jsonnet';


{
  dot: dot,
  plus: plus,

  dot_continuous: dot_continuous,
  plus_continuous: plus_continuous,
}
