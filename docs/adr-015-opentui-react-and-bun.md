# ADR-015 — OpenTUI React Renderer and Bun Runtime

**Status:** Accepted

## Context

The product requires a terminal-native interface with reusable components, strict TypeScript types, keyboard-driven interaction, focus management, streaming views, and future diff/review screens. The implementation plan allows either `@opentui/core` or `@opentui/react`.

## Decision

Use `@opentui/react` as the primary application renderer on top of `@opentui/core`.

Use Bun as the package manager, test runner, development runtime, and supported native runtime for the TUI application.

Dependencies are pinned to exact versions in the initial foundation so native renderer and React reconciler versions remain aligned.

## Consequences

- Application screens can be composed with React components and strict JSX typing.
- Low-level OpenTUI primitives remain available through `@opentui/core` when needed.
- TUI development and CI require Bun.
- React renderer and OpenTUI core versions must be upgraded together and verified with type checks and smoke tests.
- Domain and workflow logic must remain outside React components and communicate through the versioned protocol.
