export const meta = {
  name: 'jenny-devloop',
  description: 'Autonomous loop: PM→Coder→Review→Test→Secretary for completing all docs/adhoc tasks',
  phases: [
    { title: 'PM', detail: 'Read adhoc docs, pick next task, write spec' },
    { title: 'Coder', detail: 'TDD implementation from spec' },
    { title: 'Review', detail: 'Parallel code-logic and code-quality review' },
    { title: 'Senior Review', detail: 'Synthesize reviews, produce verdict' },
    { title: 'Test', detail: 'Black-box e2e testing against executable' },
    { title: 'Secretary', detail: 'Update docs, commit changes' },
  ],
}

// --- Constants ---
const ADHOC_DIR = 'docs/adhoc'
const TASKS_DIR = '.jenny/tasks'
const DEBT_FILE = 'docs/adhoc/debt.md'
const MAX_ROUNDS = 1024

// --- Helper: list remaining adhoc files ---
async function listAdhocFiles() {
  const result = await agent(
    `List all .md files in ${ADHOC_DIR}/. Return only the filenames (one per line). Do NOT include debt.md.`,
    { label: 'list-adhoc', model: 'sonnet' }
  )
  if (!result) return []
  return result.split('\n').map(s => s.trim()).filter(s => s.endsWith('.md') && s !== 'debt.md')
}

// --- Helper: check if debt.md exists and has open items ---
async function debtHasOpenItems() {
  const result = await agent(
    `Read ${DEBT_FILE}. If the file does not exist, return "NO_DEBT_FILE". If it exists, count how many debt items are NOT marked with a strikethrough (~~). Return "DEBT_OPEN: <count>" with the number of open items, or "DEBT_CLEAR" if all items are struck through.`,
    { label: 'check-debt', model: 'sonnet' }
  )
  if (!result) return false
  if (result.includes('DEBT_OPEN:')) {
    const match = result.match(/DEBT_OPEN:\s*(\d+)/)
    const count = match ? parseInt(match[1]) : 0
    log(`Debt file has ${count} open items`)
    return count > 0
  }
  return false
}

// --- Helper: check if a task dir already has a completed review ---
async function taskIsDone(taskName) {
  const reviewPath = `${TASKS_DIR}/${taskName}/review.md`
  const result = await agent(
    `Check if the file ${reviewPath} exists. If it does, read it and return "DONE: <verdict>" where verdict is APPROVED, APPROVED_WITH_DEBT, or REJECTED. If the file does not exist, return "NOT_DONE".`,
    { label: `check-done-${taskName}`, model: 'sonnet' }
  )
  return (result || '').startsWith('DONE: APPROVED')
}

// --- Phase 1: PM ---
async function pmPhase() {
  phase('PM')

  // First check if there are adhoc tasks
  const adhocFiles = await listAdhocFiles()
  log(`Found ${adhocFiles.length} adhoc files: ${adhocFiles.join(', ')}`)

  // If no adhoc files, check debt.md
  if (adhocFiles.length === 0) {
    const debtOpen = await debtHasOpenItems()
    if (!debtOpen) {
      log('No adhoc files and no open debt items. Loop complete.')
      return null
    }
    log('No adhoc tasks remain. Switching to debt clearance mode.')

    // PM reads debt.md and picks the next debt item
    const spec = await agent(
      `You are a PM agent for the Jenny project. The adhoc tasks are all done, but there are open debt items in ${DEBT_FILE}.

Read ${DEBT_FILE} fully. Your job is to pick the NEXT debt item to fix.

Picking rules:
- CRITICAL items before MAJOR, MAJOR before MINOR
- Within the same severity, prefer items that unblock others
- Group very small related items (like same-file fixes) into a single task

Write a spec to ${TASKS_DIR}/debt-<id>/spec.md (e.g., ${TASKS_DIR}/debt-CD-1/spec.md) with these sections:
- Task name: debt-<id>
- Description: what debt to clear, referencing the debt item ID
- Scope: exact files to touch
- Background: why this debt exists (from the review cycle)
- Fix steps: concrete, ordered steps to clear the debt
- Test requirements: what tests must pass before this is done

The spec must be SELF-CONTAINED — the Coder will ONLY read this spec file.

IMPORTANT: Only pick ONE debt item (or a tight group of <4 related items in the same file). Write the spec and return "SPEC_WRITTEN: debt-<id>".`,
      { label: 'pm-pick-debt', model: 'sonnet' }
    )

    if (!spec) {
      log('PM failed to produce a spec for debt.')
      return null
    }

    const match = (spec || '').match(/SPEC_WRITTEN:\s*(debt-\S+)/)
    if (!match) {
      log('PM did not return SPEC_WRITTEN marker. Output was: ' + (spec || '').substring(0, 200))
      return null
    }

    const taskName = match[1]
    log(`PM selected debt task: ${taskName}`)
    return taskName
  }
  
  // Check which are already done
  const remaining = []
  for (const f of adhocFiles) {
    const taskName = f.replace('.md', '')
    const done = await taskIsDone(taskName)
    if (done) {
      log(`${taskName}: already APPROVED, skipping`)
    } else {
      remaining.push(f)
    }
  }
  
  if (remaining.length === 0) {
    log('All adhoc tasks are APPROVED. Loop complete.')
    return null
  }
  
  log(`Remaining tasks: ${remaining.join(', ')}`)
  
  // PM reads all remaining adhoc docs and picks the next one
  const spec = await agent(
    `You are a PM agent for the Jenny project. Your job is to read all remaining adhoc documents and pick the NEXT task to implement.

REMAINING ADHOC FILES: ${remaining.join(', ')}

Steps:
1. Read each file from ${ADHOC_DIR}/ (the full content of every remaining file)
2. Read docs/README.md to understand the project conventions
3. Analyze dependencies between the tasks. Each adhoc doc has a "depends_on" field in its frontmatter. A task must not be started until its dependencies are already implemented.
4. Pick the SINGLE next task that:
   - Has all its dependencies satisfied (check if those tasks already have APPROVED reviews in ${TASKS_DIR}/<dep-name>/review.md)
   - Is the most foundational / unblocks the most other tasks
   - Can be implemented in ~300 lines of Go code or less
5. Write a detailed spec to ${TASKS_DIR}/<task_name>/spec.md with these sections:
   - Task name (slug from the adhoc filename)
   - Description: what to build, in clear terms
   - Scope: exact files/packages to touch, what NOT to touch
   - Background: why this task exists, what problem it solves (summarize from the adhoc doc)
   - Success criteria: concrete, verifiable conditions
   - Test requirements: what tests must pass
   - Estimated size: rough line count

The spec must be SELF-CONTAINED — the Coder agent will ONLY read this spec file, not the original adhoc docs.

IMPORTANT: Only pick ONE task. Write the spec and return "SPEC_WRITTEN: <task_name>".`,
    { label: 'pm-pick-task', model: 'sonnet' }
  )
  
  if (!spec) {
    log('PM failed to produce a spec.')
    return null
  }
  
  // Extract task name
  const match = (spec || '').match(/SPEC_WRITTEN:\s*(\S+)/)
  if (!match) {
    log('PM did not return SPEC_WRITTEN marker. Output was: ' + (spec || '').substring(0, 200))
    return null
  }
  
  const taskName = match[1]
  log(`PM selected task: ${taskName}`)
  return taskName
}

// --- Phase 2: Coder ---
async function coderPhase(taskName) {
  phase('Coder')
  
  const specPath = `${TASKS_DIR}/${taskName}/spec.md`
  
  const result = await agent(
    `You are a Coder agent for the Jenny project (a Go codebase). Your task is to implement the feature described in ${specPath}.

CRITICAL RULES:
- Read ${specPath} FIRST — it is your only source of truth
- Test-Driven Development: write tests FIRST, then implementation
- Ship working code — no stubs, no TODOs, no monkey-patches
- Fix root causes — never mask symptoms with workarounds, swallowed errors, disabled checks, or superficial patches
- No drive-by refactors outside scope
- Minimize tech debt; choose readable solutions over clever ones
- Before handoff, run: go fmt ./..., go vet ./..., go test ./... (for affected packages), go build ./...
- All tests must pass before you report done

Workflow:
1. Read the spec at ${specPath}
2. Read any existing code in the target packages to understand patterns
3. Write unit tests first (files matching *_test.go in the target package)
4. Write the implementation
5. Run go fmt, go vet, go test, go build
6. Fix any failures and re-run until green
7. Report "CODER_DONE: ${taskName}" when everything passes

If you encounter a problem you cannot fix in 3 attempts, report "CODER_BLOCKED: <reason>".`,
    { label: `coder-${taskName}`, model: 'sonnet' }
  )
  
  if (!result) return 'BLOCKED'
  if (result.includes('CODER_BLOCKED')) {
    log(`Coder blocked: ${result}`)
    return 'BLOCKED'
  }
  if (result.includes('CODER_DONE')) {
    log(`Coder completed: ${taskName}`)
    return 'DONE'
  }
  log(`Coder unexpected output: ${(result || '').substring(0, 200)}`)
  return 'BLOCKED'
}

// --- Phase 3: Parallel Review ---
async function reviewPhase(taskName) {
  phase('Review')
  
  const specPath = `${TASKS_DIR}/${taskName}/spec.md`
  
  // Run both reviewers in parallel
  const [logicReview, qualityReview] = await parallel([
    // Reviewer 1: Code Logic
    () => agent(
      `You are a Code Logic Reviewer for the Jenny project. Review the changes for task "${taskName}".

Read the spec at ${specPath}, then examine ALL changed files (use git diff to see what changed).

FOCUS ON:
1. Correctness: Does the implementation match the spec exactly?
2. Behavior: Does the code behave as expected in all edge cases?
3. Logic flow: Are there any control flow errors, off-by-one bugs, nil pointer risks?
4. Error handling: Are errors properly propagated and categorized?
5. Test coverage: Do the tests actually verify the spec's success criteria?

Write your review to ${TASKS_DIR}/${taskName}/review-1.md with this structure:
- Summary (1-2 sentences)
- Findings: each with severity (CRITICAL, MAJOR, MINOR) and file:line reference
- Overall assessment: PASS or FAIL

DO NOT run any tests. DO NOT modify any code. Only read and analyze.`,
      { label: `review-logic-${taskName}`, model: 'sonnet' }
    ),
    // Reviewer 2: Code Quality
    () => agent(
      `You are a Code Quality Reviewer for the Jenny project. Review the changes for task "${taskName}".

Read the spec at ${specPath}, then examine ALL changed files (use git diff to see what changed).

FOCUS ON:
1. Best practices: Go idioms, naming conventions, package organization
2. Architecture: Is the code in the right package? Are interfaces used appropriately?
3. Performance: Any obvious inefficiencies, unnecessary allocations, or blocking operations?
4. Security: Any input validation gaps, path traversal risks, injection vectors?
5. Maintainability: Is the code readable? Are there magic numbers? Is error handling consistent?

Write your review to ${TASKS_DIR}/${taskName}/review-2.md with this structure:
- Summary (1-2 sentences)
- Findings: each with severity (CRITICAL, MAJOR, MINOR) and file:line reference
- Overall assessment: PASS or FAIL

DO NOT run any tests. DO NOT modify any code. Only read and analyze.`,
      { label: `review-quality-${taskName}`, model: 'sonnet' }
    ),
  ])
  
  log(`Logic review: ${logicReview ? 'completed' : 'failed'}`)
  log(`Quality review: ${qualityReview ? 'completed' : 'failed'}`)
  
  return (logicReview && qualityReview) ? 'DONE' : 'BLOCKED'
}

// --- Phase 4: Senior Review ---
async function seniorReviewPhase(taskName) {
  phase('Senior Review')
  
  const review1Path = `${TASKS_DIR}/${taskName}/review-1.md`
  const review2Path = `${TASKS_DIR}/${taskName}/review-2.md`
  const reviewPath = `${TASKS_DIR}/${taskName}/review.md`
  
  const verdict = await agent(
    `You are a Senior Reviewer for the Jenny project. Synthesize the two code reviews for task "${taskName}" and produce a final verdict.

Read:
- ${review1Path} (code logic review)
- ${review2Path} (code quality review)

Then independently verify the most important findings by reading the actual code.

Write your final review to ${reviewPath} with this structure:

# Senior Review: ${taskName}

## Summary
(2-3 sentences synthesizing both reviews)

## Verified Findings
(Findings you confirmed by reading the code, with severity)

## Disputed Findings
(Findings from one reviewer you disagree with, with reasoning)

## VERDICT
One of:
- APPROVED — all criteria met, no significant issues
- APPROVED_WITH_DEBT — acceptable but with noted debt that should be tracked
- REJECTED — must be reworked (specify exactly what the Coder must fix)

If APPROVED_WITH_DEBT, include a "## Debt" section listing each debt item (one line each) to add to ${DEBT_FILE}.

If REJECTED, include a "## Required Changes" section with specific, actionable items for the Coder.

Return "VERDICT: <APPROVED|APPROVED_WITH_DEBT|REJECTED>" as the last line.`,
    { label: `senior-review-${taskName}`, model: 'opus' }
  )
  
  if (!verdict) return 'REJECTED'
  
  if (verdict.includes('VERDICT: APPROVED_WITH_DEBT')) {
    log('Senior review: APPROVED_WITH_DEBT — adding debt items')
    // Debt items should already be in the review.md file written by the agent
    return 'APPROVED_WITH_DEBT'
  }
  if (verdict.includes('VERDICT: APPROVED')) {
    log('Senior review: APPROVED')
    return 'APPROVED'
  }
  log('Senior review: REJECTED')
  return 'REJECTED'
}

// --- Phase 5: Black-box Test ---
async function testPhase(taskName) {
  phase('Test')
  
  const specPath = `${TASKS_DIR}/${taskName}/spec.md`
  const testReportPath = `${TASKS_DIR}/${taskName}/test.md`
  
  const result = await agent(
    `You are a Black-box Tester for the Jenny project. Test the implementation for task "${taskName}".

CRITICAL: You must NOT read any implementation code. You are testing the BUILT EXECUTABLE only.

Steps:
1. Read the spec at ${specPath} to understand what the feature should do
2. Read docs/arch/e2e-test-harness.md to understand the e2e test infrastructure
3. Build the executable: go build -o jenny .
4. Write e2e tests in the e2e/ directory that exercise the feature as described in the spec
5. Run the e2e tests against the built executable
6. Write your test report to ${testReportPath} with:

# Test Report: ${taskName}

## Test Cases
(Each test case with: description, command/flags used, expected result, actual result, PASS/FAIL)

## Issues Found
(Any behavior that deviates from the spec)

## VERDICT
- APPROVED — all spec behavior verified
- REJECTED — issues found (list each with spec reference)

Return "TEST_VERDICT: <APPROVED|REJECTED>" as the last line.

DO NOT read any files under internal/ except test infrastructure files.`,
    { label: `test-${taskName}`, model: 'sonnet' }
  )
  
  if (!result) return 'REJECTED'
  if (result.includes('TEST_VERDICT: APPROVED')) {
    log('Test: APPROVED')
    return 'APPROVED'
  }
  log('Test: REJECTED')
  return 'REJECTED'
}

// --- Phase 6: Secretary ---
async function secretaryPhase(taskName) {
  phase('Secretary')

  const isDebtTask = taskName.startsWith('debt-')
  const adhocFile = isDebtTask ? DEBT_FILE : `${ADHOC_DIR}/${taskName}.md`

  const prompt = isDebtTask
    ? `You are a Secretary agent for the Jenny project. Finalize debt task "${taskName}".

Steps:
1. Read ${DEBT_FILE}
2. Find the debt item matching ${taskName.replace('debt-', '')} (e.g., "debt-CD-1" matches item "CD-1")
3. Mark that item as done by wrapping its line with ~~strikethrough~~ (e.g., "- ~~CD-1: description~~")
4. If the fix also required changes to permanent docs under docs/arch/ or docs/tools/, update those docs
5. Stage all changes and commit with a message like "fix: clear debt <id> — <summary>"
6. Verify the commit succeeded

Return "SECRETARY_DONE" when complete.`
    : `You are a Secretary agent for the Jenny project. Finalize task "${taskName}".

Steps:
1. Read docs/README.md to understand the documentation conventions
2. Read the adhoc doc at ${adhocFile} — it should be consolidated into permanent docs and then deleted
3. Read the spec at ${TASKS_DIR}/${taskName}/spec.md to understand what was built
4. Update any relevant permanent docs under docs/arch/, docs/tools/, or docs/patterns/ to reflect the new implementation state
5. Delete the adhoc file ${adhocFile} (the task is done)
6. Stage all changes and commit with a conventional commit message like "feat(<scope>): <description>"
   - Use the format: type(scope): description
   - Types: feat, fix, refactor, test, docs, chore
7. Verify the commit succeeded

Return "SECRETARY_DONE" when complete.`

  const result = await agent(prompt, { label: `secretary-${taskName}`, model: 'haiku' })

  if (result && result.includes('SECRETARY_DONE')) {
    log(`Secretary completed: ${taskName}`)
    return 'DONE'
  }
  log(`Secretary unexpected output: ${(result || '').substring(0, 200)}`)
  return 'BLOCKED'
}

// ============================================================
// MAIN LOOP
// ============================================================

let round = 0
let taskName = null

while (round < MAX_ROUNDS) {
  round++
  log(`=== Round ${round}/${MAX_ROUNDS} ===`)
  
  // Phase 1: PM picks next task
  if (!taskName) {
    taskName = await pmPhase()
    if (!taskName) {
      log('No more tasks. DevLoop complete!')
      break
    }
  }
  
  // Phase 2: Coder implements
  let coderResult = await coderPhase(taskName)
  if (coderResult === 'BLOCKED') {
    log(`Coder blocked on ${taskName}, stopping loop.`)
    break
  }
  
  // Phase 3: Parallel review
  let reviewOk = await reviewPhase(taskName)
  if (reviewOk === 'BLOCKED') {
    log(`Review phase blocked on ${taskName}`)
    break
  }
  
  // Phase 4: Senior review
  let verdict = await seniorReviewPhase(taskName)
  
  if (verdict === 'REJECTED') {
    log(`Task ${taskName} REJECTED by senior review. Returning to Coder.`)
    // Coder re-reads review.md and fixes
    coderResult = await agent(
      `You are the Coder agent. Your previous implementation for task "${taskName}" was REJECTED.

Read the review at ${TASKS_DIR}/${taskName}/review.md — specifically the "Required Changes" section.
Fix ALL issues listed there.
Run go fmt ./..., go vet ./..., go test ./..., go build ./... before reporting.
Report "CODER_DONE: ${taskName}" when all tests pass.`,
      { label: `coder-fix-${taskName}`, model: 'sonnet' }
    )

    if (!coderResult || coderResult.includes('CODER_BLOCKED')) {
      log(`Coder could not fix ${taskName}, stopping loop.`)
      break
    }

    // Re-review after fix
    await reviewPhase(taskName)
    verdict = await seniorReviewPhase(taskName)

    if (verdict === 'REJECTED') {
      log(`Task ${taskName} REJECTED again after fix. Stopping loop.`)
      break
    }
  }

  // Phase 5: Black-box test
  let testVerdict = await testPhase(taskName)

  if (testVerdict === 'REJECTED') {
    log(`Task ${taskName} REJECTED by tests. Returning to Coder.`)
    coderResult = await agent(
      `You are the Coder agent. Your implementation for task "${taskName}" failed black-box testing.

Read the test report at ${TASKS_DIR}/${taskName}/test.md — specifically the "Issues Found" section.
Fix ALL issues listed there.
Run go fmt ./..., go vet ./..., go test ./..., go build ./... before reporting.
Report "CODER_DONE: ${taskName}" when all tests pass.`,
      { label: `coder-fix-test-${taskName}`, model: 'sonnet' }
    )
    
    if (!coderResult || coderResult.includes('CODER_BLOCKED')) {
      log(`Coder could not fix test issues for ${taskName}, stopping loop.`)
      break
    }
    
    // Re-test after fix
    testVerdict = await testPhase(taskName)
    
    if (testVerdict === 'REJECTED') {
      log(`Task ${taskName} REJECTED again after test fix. Stopping loop.`)
      break
    }
  }
  
  // Phase 6: Secretary
  const secResult = await secretaryPhase(taskName)
  if (secResult !== 'DONE') {
    log(`Secretary blocked on ${taskName}`)
    break
  }
  
  log(`=== Task ${taskName} COMPLETE ===`)
  taskName = null  // Reset for next PM phase
}

log(`DevLoop finished after ${round} rounds.`)
return { rounds: round, lastTask: taskName }
