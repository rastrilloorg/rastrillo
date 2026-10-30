// Package codelimits measures Rastrillo's Go code against four
// readability limits: a line is at most 80 columns, a file at most 400
// lines, a function at most 40 lines (an action at most 15), and no
// name is a single letter.
//
// It only measures. Measure returns every offence it finds and judges
// none of them, so the code that decides which offences are allowed
// stays apart from the code that counts. An offence has a Key that
// names it within its file and survives edits elsewhere in the file,
// which is what a record of known offences needs to point at.
//
// Three kinds of file are never measured against some or all limits.
// A generated file (a "Code generated ... DO NOT EDIT." line before the
// package clause) is skipped whole, because the fix is in the generator.
// A data file, one whose header carries "//rastrillo:data <reason>", is
// exempt from the columns and file limits, because a table of strings
// cannot be wrapped without being made worse. Directories named
// testdata, vendor or node_modules, any starting with "." or "_", and
// nested modules are not walked.
package codelimits
