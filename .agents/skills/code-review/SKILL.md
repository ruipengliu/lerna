---
name: code-review
description: Review code changes against repo standards and the task spec. Use for PRs, branches, work in progress, or changes since a ref.
---

Two-axis review of the requested working-tree changes or commit diff:

- **Standards**: does the code conform to this repo's documented coding standards?
- **Spec**: does the code faithfully implement the originating issue / spec?

Both axes run as **parallel sub-agents** so they don't pollute each other's context, then this skill aggregates their findings.

Use `docs/agents/issue-tracker.md` when available to resolve ticket references.

## Process

### 1. Pin the review scope

Reuse the scope from the request or task context, including any supplied diff, refs, or paths. Ask only if materially different scopes remain plausible. When called by `/implement`, review the current task's uncommitted changes.

- **Working tree:** inspect staged changes with `git diff --cached -- <paths>`, unstaged changes with `git diff -- <paths>`, and new files from `git ls-files --others --exclude-standard -z -- <paths>`. Read each in-scope new file. Keep paths and hunks within the requested scope, including when other work shares a file.
- **Commits:** resolve the requested refs first. For changes since a fixed point on the current branch, use `git diff <fixed-point>...HEAD` and `git log <fixed-point>..HEAD --oneline`. Honor different endpoints when explicitly requested.

Capture the scoped diffs, new-file contents, and commit list (where applicable) once for both reviewers. Report invalid refs and leave that comparison unassessed. If a valid scope has neither diffs nor new files, finish with "no changes to review".

### 2. Identify the spec source

Reuse requirements already available, in this order:

1. A spec, ticket, or agreed acceptance criteria supplied in the request or conversation.
2. Issue references in the commit messages (`#123`, `Closes #45`, GitLab `!67`, etc.), fetched via the workflow in `docs/agents/issue-tracker.md` when available.
3. A spec file under `docs/`, `specs/`, or `.scratch/` matching the branch name or feature.

When requirements are missing or incomplete, continue Standards and any Spec checks supported by the available requirements. Ask a focused question only where the missing detail affects the assessment, and report the unassessed scope as a coverage gap.

### 3. Identify the standards sources

Anything in the repo that documents how code should be written, such as `CODING_STANDARDS.md` or `CONTRIBUTING.md`.

On top of whatever the repo documents, the Standards axis always carries the **smell baseline** below: a fixed set of Fowler code smells (_Refactoring_, ch.3) that applies even when a repo documents nothing. Two rules bind it:

- **The repo overrides.** A documented repo standard always wins; where it endorses something the baseline would flag, suppress the smell.
- **Always a judgement call.** Each smell is a labelled heuristic ("possible Feature Envy"), never a hard violation. Like any standard here, skip anything tooling already enforces.

Each smell reads *what it is* → *how to fix*; match it against the diff:

- **Mysterious Name**: a function, variable, or type whose name doesn't reveal what it does or holds. → rename it; if no honest name comes, the design's murky.
- **Duplicated Code**: the same logic shape appears in more than one hunk or file in the change. → extract the shared shape, call it from both.
- **Feature Envy**: a method that reaches into another object's data more than its own. → move the method onto the data it envies.
- **Data Clumps**: the same few fields or params keep travelling together (a type wanting to be born). → bundle them into one type, pass that.
- **Primitive Obsession**: a primitive or string standing in for a domain concept that deserves its own type. → give the concept its own small type.
- **Repeated Switches**: the same `switch`/`if`-cascade on the same type recurs across the change. → replace with polymorphism, or one map both sites share.
- **Shotgun Surgery**: one logical change forces scattered edits across many files in the diff. → gather what changes together into one module.
- **Divergent Change**: one file or module is edited for several unrelated reasons. → split so each module changes for one reason.
- **Speculative Generality**: abstraction, parameters, or hooks added for needs the spec doesn't have. → delete it; inline back until a real need shows.
- **Message Chains**: long `a.b().c().d()` navigation the caller shouldn't depend on. → hide the walk behind one method on the first object.
- **Middle Man**: a class or function that mostly just delegates onward. → cut it, call the real target direct.
- **Refused Bequest**: a subclass or implementer that ignores or overrides most of what it inherits. → drop the inheritance, use composition.

### 4. Spawn both sub-agents in parallel

**Standards sub-agent prompt** should include:

- The scoped diffs and new-file contents from step 1, plus the commit list where applicable.
- The list of standards-source files you found in step 3, **plus the smell baseline from step 3** pasted in full (the sub-agent has no other access to it).
- The brief: "Report, per file/hunk where relevant, (a) every place the diff violates a documented standard: cite the standard (file + the rule); and (b) any baseline smell you spot: name it and quote the hunk. Distinguish hard violations from judgement calls: documented-standard breaches can be hard, but baseline smells are always judgement calls, and a documented repo standard overrides the baseline. Skip anything tooling enforces. Under 400 words."

**Spec sub-agent prompt** should include:

- The same scoped diffs and new-file contents from step 1, plus the commit list where applicable.
- The available requirements and their sources (task context, spec, or ticket), including known gaps.
- The brief: "Report: (a) requirements that are missing or partial; (b) behaviour that wasn't asked for (scope creep); (c) requirements that look implemented but where the implementation looks wrong. Cite the requirement and its source for each finding. Report checks unsupported by the available requirements as coverage gaps. Under 400 words."

If no requirements are available, run Standards and report Spec as "not assessed: requirements unavailable".

### 5. Aggregate

Present the two reports under `## Standards` and `## Spec` headings, verbatim or lightly cleaned. Use "not assessed" for an unavailable Spec axis and state any partial coverage. Do **not** merge or rerank findings, because the two axes are deliberately separate (see _Why two axes_).

End with a one-line summary of findings and coverage per axis, and the worst issue _within each assessed axis_ (if any). Don't pick a single winner across axes: that's the reranking the separation exists to prevent.

## Why two axes

A change can pass one axis and fail the other:

- Code that follows every standard but implements the wrong thing → **Standards pass, Spec fail.**
- Code that does exactly what the issue asked but breaks the project's conventions → **Spec pass, Standards fail.**

Reporting them separately stops one axis from masking the other.
