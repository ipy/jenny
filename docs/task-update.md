---
title: TaskUpdate Tool
slug: task-update
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
# TaskUpdate Tool (Todo v2)

## Overview

Updates task fields, status, and dependency graph.

## Parameters

| Param | Description |
|-------|-------------|
| `task_id` | Required task identifier |
| `subject` | New subject |
| `description` | New description |
| `active_form` | New active form |
| `status` | New status (`pending`, `in_progress`, `completed`, `deleted`) |
| `metadata` | Metadata object (merge on update, null value deletes key) |
| `add_blocks` | Array of task IDs this task blocks |
| `add_blocked_by` | Array of task IDs this task is blocked by |

## Output

Returns updated task with `blocks` and `blocked_by` arrays.

## Acceptance Criteria

- **AC1:** deleted status removes or marks task deleted.
- **AC2:** addBlocks/addBlockedBy update graph.
- **AC3:** null metadata key deletes field.
