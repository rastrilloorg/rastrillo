package main

// budgetsTxtTemplate is .rastrillo/budgets.txt. Routes mounts the static
// files and the API, so the perf walk sees both, and neither is a page.
const budgetsTxtTemplate = `# Exceptions to this app's budgets, each
# with its reason. Empty is the goal. docs/site/testing.md has the grammar.
#
#   size  <dir>             <source>|-  <test>|-  <reason>
#   time  <dir>             <duration>            <reason>
#   perf  <METHOD> <path>   <duration>|skip       <reason>
#   boot                    <duration>            <reason>
perf  GET /static/*  skip  the static files, not a page
perf  GET /api/v1/*  skip  the JSON API, not a page
`
