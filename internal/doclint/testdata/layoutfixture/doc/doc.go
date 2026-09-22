// Package doc links across the layout of a module whose root holds no package.
//
// [example.com/lp/winonly] and [example.com/lp/winonly.W] name a package that
// exists only under a build constraint, and [example.com/lp/winonly.Ignored]
// a name only an ignored file declares. [example.com/lp] names the module
// root, and [example.com/lp/gone] and [example.com/lp/v1/gone] directories
// that do not exist; v1 is no major-version element.
// [example.com/lp/v2/api.Absent] is in a nested module,
// [example.com/lp/v3/api.Absent] and [example.com/lp/v5/api.Absent] in major
// versions this module does not hold, v5 being a file, and [example.com/lp/v4/pkg.Absent] in a directory of this module
// that only looks like one.
package doc
