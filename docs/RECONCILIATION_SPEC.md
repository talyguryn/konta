# Konta Reconciliation Specification

This document defines the intended runtime behavior of Konta as a normative ruleset.

The goal is simple: on every cycle, Konta must make the actually running Docker state match the expected state derived from Git and state.json.

This specification intentionally avoids introducing separate mental models such as deploy mode, healing mode, drift mode, or recovery procedures. Konta has one job: reconcile actual state to desired state.

## 1. Core Principle

Konta SHALL execute one reconciliation cycle.

Each cycle SHALL do three things:

1. Compute desired state.
2. Observe actual state.
3. Apply the minimum safe actions required so that actual state converges to desired state.

## 2. Sources of Truth

Konta SHALL use only these sources of truth:

1. Git repository content for application definitions.
2. The current fetched commit for detecting newly changed applications.
3. state.json for the expected deployed commit of unchanged applications.
4. Docker runtime state for the actual running containers and stacks.

Konta SHALL NOT invent a new target version for an unchanged application.

## 3. Desired State

For every application in apps/, Konta SHALL compute one expected deployment target.

For a changed application:

1. Desired commit SHALL be the newly fetched commit.
2. Desired compose definition SHALL come from the newly fetched release directory.

For an unchanged application:

1. Desired commit SHALL be the commit stored in state.json for that application.
2. Desired compose definition SHALL come from the release directory for that stored commit.
3. If no per-application state exists yet, Konta MAY fall back to the current fetched release.

Desired state SHALL include:

1. Expected compose project name.
2. Expected set of managed services.
3. Expected container state: present and running.
4. Expected health state when healthchecks exist.
5. Absence of stale managed stacks that do not belong to the expected target.

## 4. Actual State

Konta SHALL observe actual Docker state using managed container labels and compose project names.

Actual state SHALL include:

1. Which managed stacks exist for each application.
2. Which managed services exist inside each stack.
3. Whether containers are running, stopped, missing, or unhealthy.

## 5. Cycle Semantics

Konta SHALL run the same reconciliation model on every cycle.

The only difference between cycles is how desired commit is computed per application.

1. Changed applications target the newly fetched commit.
2. Unchanged applications target the commit stored in state.json.

There SHALL be no separate concept of "healing target version" for unchanged applications.

## 6. Minimal Action Rule

Konta SHALL apply the minimum safe action needed to converge an application.

Examples:

1. If the expected stack exists and all expected containers are running and healthy, action SHALL be none.
2. If the expected stack exists and containers are stopped, action SHOULD be start.
3. If the expected stack is missing, action SHALL be reconcile that application to the expected stack.
4. If service membership does not match the compose definition, action SHALL be reconcile that application.
5. If unexpected stale managed stacks exist, action SHALL remove those stale stacks only when doing so is safe.

## 7. Stable Applications

For non-rolling applications, the expected compose project name SHALL be the stable application name.

For a stable application:

1. Missing stack SHALL be restored using the expected commit.
2. Stopped containers SHOULD be started if the stack already matches the expected target.
3. Unhealthy containers MAY trigger full reconcile of that application.
4. A new version SHALL be introduced only when the application is marked as changed in the current Git cycle.

## 8. Rolling Applications

For rolling applications, the expected compose project name SHALL be app-shortCommit.

Rolling reconciliation SHALL obey the following rules:

1. If the expected stack exists and is healthy, Konta SHALL NOT create another stack.
2. If the expected stack is missing, Konta SHALL restore the expected stack for the expected commit.
3. If both expected and stale rolling stacks exist, stale stacks SHALL be removed only after the expected stack is confirmed healthy.
4. If a new expected rolling stack is created during reconciliation, cleanup of stale rolling stacks SHOULD complete in the same reconciliation flow.
5. Konta SHALL NOT leave two rolling versions running longer than required for safe cutover.

## 9. State Updates

state.json SHALL represent the deployed target chosen by normal reconciliation.

Rules:

1. Deploying a changed application SHALL update that application's commit in state.json.
2. Reconciling an unchanged application back to its stored state SHALL NOT change its commit in state.json.
3. Runtime repair alone SHALL NOT promote an unchanged application to a newer commit.

## 10. Hooks

Hooks SHALL follow the reconciliation purpose of the cycle.

1. Git-driven application updates MAY run deploy hooks.
2. Runtime repair of unchanged applications SHOULD NOT run deploy hooks by default.
3. Hook failures SHALL NOT silently redefine the reconciliation target.

## 11. Post-Deploy Convergence

After a cycle that deploys changed applications, Konta SHOULD verify that the resulting actual state matches desired state for the affected applications before considering the cycle complete.

This post-deploy verification is part of reconciliation, not a separate healing subsystem.

## 12. Forbidden Behaviors

Konta SHALL NOT do any of the following:

1. Upgrade an unchanged application to the current fetched commit only because a health check ran.
2. Rewrite state.json for an unchanged application merely because runtime repair occurred.
3. Treat a healthy stack on the stored commit as outdated solely because a newer Git commit exists.
4. Leave stale rolling stacks indefinitely after successful convergence to the expected stack.
5. Broaden the scope of a changed-application deploy by unexpectedly promoting unrelated unchanged applications.

## 13. Recommended Operational Model

The simplest stable operational model is:

1. self_heal.enable controls whether unchanged applications are reconciled back to their stored state.
2. state.json remains the single source of truth for unchanged application versions.
3. New Git commits affect only changed applications.
4. Rolling and stable applications differ only in cutover strategy, not in source of truth.

## 14. Summary Law

Konta SHALL, on every cycle, make the set of running managed containers match the expected per-application state derived from Git changes plus state.json, using the smallest safe action, without silently upgrading unchanged applications.
