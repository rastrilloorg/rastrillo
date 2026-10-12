# Contributing to rastrillo

Work on rastrillo happens on amadan, at https://amadan.net/rastrillo/rastrillo. The GitHub repository is a mirror. Do not open pull requests or issues there.

## Before a feature or fix lands

A branch can merge when it meets the three conditions below. Put the evidence on the branch page, so a reviewer can check it without asking you.

### 1. Build it with the strongest model you can use

Use the strongest model your provider offers, at its highest reasoning setting if the tool has one. Name the model and the setting in the branch description. If you used a smaller model, say why there.

### 2. Have a model from another provider review it

Before you merge, have the strongest model from a different provider review the branch. If you built it with Claude, use a model from OpenAI or another provider, and the other way round. A second model from the same provider counts as an extra review, not as this one.

Run the review from a checkout of the commit you mean to merge. Give the model the full diff against main and the branch description, and ask what is wrong with it. If you push again after the review, have the new commits reviewed too.

Post every review run in a discussion on the branch: the prompt and the whole reply, not a summary. Under each, list every finding and what you did about it: fixed, with the commit, or left alone, with the reason. Post a review with no findings too.

If you cannot reach another provider, say why in the discussion and stop. A person must approve a same-provider review on the branch before it merges.

### 3. Show a test failing before the change and passing after

Each fix and each new behaviour needs a test that fails before the change and passes after it. Run that test against the code before your change, in a separate worktree. In a discussion on the branch, post the commit, the command and the output of both runs.

The failure must be the test's own: `--- FAIL` with its name, and a message that says what is missing or wrong. A build error does not count. If the test cannot compile against the old code, add stub versions of the new functions there, and post them with the output.

A new test for behaviour that already works says so, and need not fail first. A change with nothing to test, such as a docs edit, says so in the branch description.

Then `make ci` must pass on the final commit. AGENTS.md says how to land the branch, and covers the rest of how work is done here.
