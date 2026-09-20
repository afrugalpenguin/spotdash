# Contributing

House style for anything written in this repo. Keep it plain and short.

## Commits

Use the conventional format, `type(scope): description`, with a scope and a subject of 72 characters or fewer. The body is optional and about 80 words at most. Say what changed, and why only when the diff does not show it. Testing detail goes in the pull request.

## Pull requests

Write impersonally, in the present tense: what changed, why, how it was tested (the commands and their results), and what is still untested. No first person singular. Call hardware "the Echo Spot" or "real hardware". A small pull request is a paragraph with no headings. Do not reuse the same set of headings on every pull request.

## Issues

A small issue is a short paragraph plus how to tell it is done. Add headings only when the issue is large.

## Code comments

Say what the code cannot. Three lines at most by default. A doc comment on an exported name is one or two sentences. Longer design reasoning lives in docs/architecture.md, with a one-line pointer in the code if needed. Do not restate the code.

Write short, direct sentences and avoid leaning on one connective. Watch in particular for "rather than", "X, not Y", "on purpose" and "deliberately". When a comment needs them, the sentence can usually be split or cut.

## Test failure messages

Use the short got and want form, for example `status = %d, want %d`.

## Docs

Prose first, few bullets, little bold. Keep commands and facts exact. A design doc for something not yet built is shorter than the docs for what exists.

## Punctuation

ASCII only: hyphens, never long dashes, and three dots, never the single ellipsis character.
