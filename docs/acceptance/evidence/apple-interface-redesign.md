# Apple interface redesign

**Topic:** Apple interface redesign.  
**Record scope:** The complete source, environment, observed checks, commands, revisions, results, and limitations formerly retained in the Component verification index.

This file is an evidence record, not an acceptance statement; category definitions are in [../evidence.md](../evidence.md#evidence-categories-and-acceptance-boundary).

## Source/environment and observed checks

On the earlier Linux validation host, `PATH=/usr/local/go/bin:$PATH go test ./...` and `PATH=/usr/local/go/bin:$PATH go test -race ./internal/control` passed on the integrated API source; history tests cover ACL/scope denial, cursor/limit validation, stable timestamp ordering, and pagination. At the Linux checkpoint, nine new Swift contract/state regression tests were added but had not run. The [earlier installation record](https://github.com/ronigooja/Zoe/blob/95ee55a5bbd8dccfb7f14af9f9703bb83727da6c/docs/master-plan.md#historical-apple-redesign-installation-refresh) records successful execution of the full 67-test Swift suite and fresh macOS/iOS target builds. A tree-sitter 0.26.0 / Swift grammar 0.7.4 syntax pass covered all 25 Swift files; paired locale keys, literal-source coverage, format placeholders, plural plists, `python3 tools/check_docs.py`, and `git diff --check` passed. These checks do not type-check Swift or render the UI.

## Acceptance limits

The installation refresh establishes fresh Swift tests and macOS/iOS app-target builds, macOS local installation/startup, and Launchpad registration only. Screenshots across the native interface requirements, core UI flow tests, VoiceOver/Dynamic Type/keyboard checks, real iPhone/iPad devices, and real services remain open. No complete redesign UI or product acceptance is claimed.
