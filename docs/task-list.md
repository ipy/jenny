---
title: TaskList Tool
slug: task-list
priority: P4
status: done
spec: complete
code: done
package: internal/tool
gaps:
  []
depends_on:
  - task-create
---
# TaskList Tool (Todo v2)

## Overview

Lists tasks with optional filters.

## Behavior

- Filter `_internal` metadata from output
- Output includes `blocks` and `blocked_by` arrays for each task
- Strip resolved blockers from `blocked_by` arrays

## Acceptance Criteria

- **AC1:** Internal metadata not exposed.
- **AC2:** Resolved blockers stripped from blockedBy.
