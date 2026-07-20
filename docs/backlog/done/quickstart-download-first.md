Should Quickstart lead with the download instead of build-from-source?
Status: resolved — yes; D-0125 (session `quickstart-download-first`)
Priority: next

Split out of `docs/backlog/release-pipeline.md` at that item's closure
(session `release-pipeline-03`, D-0123), per that item's own instruction that
this question need not block closure.

`-02` (D-0122) deliberately left Quickstart on its build-from-source path and
only corrected what had become false. Worth deciding post-tag: **should the
guided first run lead with downloading a released binary instead of
`make build`?**

The case for: the download path needs neither a Go toolchain nor Node, which
are currently three of Quickstart's prerequisites, and it reaches a running
daemon in fewer steps — a better on-rails first experience for a reader who
wants one answer, not a build.

The case against, and what to weigh: Quickstart is the on-rails path for a
reader evaluating Joe, who may well want the source tree anyway; and the
guide currently assumes a repository checkout in later steps. Flipping it is
a restructure, not an edit.

This is recorded as a question to decide, not a decided change. Resolve it
deliberately; do not treat it as implied by the tag cut.
