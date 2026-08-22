// Package use contains the model asking for a tool.
//
// A use is three things: which tool, with what arguments, and the id the
// answer has to carry back so the call and its result can be paired.
//
// [Arguments] is raw JSON and stays that way. The core does not know any
// particular tool's parameters, so it neither parses them nor reshapes them;
// the tool that receives them unmarshals its own. That is also why [New]
// validates nothing — whether the arguments parse is the tool's business, and
// arguments that do not are a failure the model is shown rather than one this
// package is entitled to catch.
//
// The counterpart is
// [github.com/ksahli/compadre/internal/core/tools/results.Type], which
// carries the same id back.
package use
