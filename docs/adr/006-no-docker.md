# ADR-006: No Docker requirement

Date: 2026-08-27 · Status: accepted · Supersedes: —

## Context

Dropserve is for the small local-app case where installing and running Docker Desktop is more work than the app itself.

## Options considered

1. Container-backed apps — reproducible and isolated, but heavy and contrary to the zero-configuration experience.
2. Direct static serving and explicitly supervised local commands — lightweight and transparent.

## Decision

Do not require or silently introduce Docker.

## Consequences

App processes run with the user's privileges and are not isolated; documentation must state this plainly.

