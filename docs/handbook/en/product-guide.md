# Lum Harbor Product Guide

[Project Handbook](../README.md) · [中文产品指南](../zh-CN/product-guide.md) · [Plugin Usage](plugin-usage.md) · [Troubleshooting](runtime-and-troubleshooting.md)

Lum Harbor is a local-first work cockpit. It brings long-running work, daily Todos, external evidence, AI-assisted understanding, reminders, notifications, and an explainable history into one desktop application.

The central rule is simple:

> AI understands and proposes. You review. Lum Harbor applies authoritative changes.

This guide describes Lum Harbor V1 as it works today. The screenshots come from the native application and a synthetic acceptance workspace; they are not mockups.

## Contents

1. [What Lum Harbor is](#1-what-lum-harbor-is)
2. [Getting started](#2-getting-started)
3. [Main areas of the app](#3-main-areas-of-the-app)
4. [Configure AI](#4-configure-ai)
5. [Install and configure Plugins](#5-install-and-configure-plugins)
6. [Use the Assistant and review Proposals](#6-use-the-assistant-and-review-proposals)
7. [WorkItems](#7-workitems)
8. [Workflow](#8-workflow)
9. [Tracked Items and dependencies](#9-tracked-items-and-dependencies)
10. [Planner and Todos](#10-planner-and-todos)
11. [Reminders](#11-reminders)
12. [MonitorPoints](#12-monitorpoints)
13. [Attention and Notifications](#13-attention-and-notifications)
14. [Apple Reminders integration](#14-apple-reminders-integration)
15. [Synthetic Project Plugin](#15-synthetic-project-plugin)
16. [Source and provenance](#16-source-and-provenance)
17. [History](#17-history)
18. [Restart and durability](#18-restart-and-durability)
19. [Common workflows](#19-common-workflows)
20. [Troubleshooting](#20-troubleshooting)
21. [Current V1 limitations](#21-current-v1-limitations)

## 1. What Lum Harbor is

Lum Harbor helps you answer three practical questions:

- What work am I responsible for?
- What needs attention now?
- Why did this item, reminder, or notification appear?

The app keeps its authoritative state locally. External systems and Plugins provide evidence, but they do not write directly into Lum Harbor's WorkItems, Workflow, or Planner truth. Structured integrations can synchronize through controlled rules; semantic AI changes become Proposals that require your review.

The product concepts fit together like this:

- A **WorkItem** is a long-running outcome such as a release, migration, or project.
- A **Workflow** gives one WorkItem an ordered sequence of stages.
- **Tracked Items** are the units moving through that Workflow.
- **Planner Todos** are concrete actions you can schedule and complete.
- **Reminders** and **MonitorPoints** express expectations about time and progress.
- **Attention** says something currently needs consideration.
- A **Notification** communicates that Attention to you.
- **History** records what happened and how the facts are connected.

![Dashboard overview with navigation and pending Proposal awareness](../assets/dashboard-overview.png)

The Dashboard is an orientation surface. It shows today's work, active WorkItems, and whether Suggested Changes are waiting for review. It does not apply Proposals itself.

## 2. Getting started

### Launch the app

Open the Lum Harbor application bundle supplied by your trusted distributor or maintainer. When running from source, follow [Build and Run](build-and-run.md).

On first launch, Lum Harbor creates its local configuration and SQLite data store in the operating system's user configuration area. Your WorkItems, Planner items, Proposals, Source evidence, and History remain on this machine unless an integration performs an explicit external side effect such as delivering a macOS notification.

### First-run checklist

1. Open **Settings**.
2. Configure an AI provider under **AI**.
3. Add any `.lhplugin` packages under **Extensions**.
4. Create and configure the Plugin instances you want to use.
5. Restart Lum Harbor after Plugin or provider-selection changes.
6. Open **AI Assistant** and ask a read-only question before creating your first change Proposal.

### Navigation

The left sidebar contains the primary pages: **Dashboard**, **Planner**, **Work Items**, **Notifications**, **History**, and **Settings**. **AI Assistant** opens from the upper-right corner and keeps the current page or selected WorkItem as a context hint.

Settings that change runtime composition are loaded once at startup. When Lum Harbor says a restart is required, save your work and quit/reopen the app; there is no Plugin hot reload or in-app **Restart Now** action in V1.

## 3. Main areas of the app

| Area              | What it is for                                                                                                      |
| ----------------- | ------------------------------------------------------------------------------------------------------------------- |
| **Dashboard**     | A quick view of today's work, current WorkItems, active workflows with target dates, and pending Suggested Changes. |
| **Planner**       | Calendar and open-task views for dated, timed, date-only, and undated Todos.                                        |
| **Work Items**    | Long-running outcomes, Workflow structure, Tracked Items, dependencies, MonitorPoints, and scoped History.          |
| **Notifications** | Durable alerts with unread, action, snooze, and resolved views.                                                     |
| **History**       | Timeline, linked Traces, and Audit views explaining what happened.                                                  |
| **AI Assistant**  | Questions, Source interpretation review, and Proposal-first changes.                                                |
| **Settings**      | Appearance, effective AI provider, installed Plugins, instances, and runtime status.                                |

Only Plugin inventory and AI runtime settings are fully live in the current Settings experience. Cards marked preview or “coming later” are not active configuration surfaces.

## 4. Configure AI

Open **Settings → AI**. The **Runtime** card names the one effective provider currently handling AI requests.

![Settings showing the effective Plugin AI provider and inactive built-in provider](../assets/settings-ai.png)

Lum Harbor supports two provider routes:

- **Built-in OpenAI**: an OpenAI-compatible endpoint, model, request timeout, and securely stored credential.
- **Plugin AI Runtime**: an enabled Plugin instance providing `ai.provider`. When selected, it takes over all AI requests and the built-in provider is shown as inactive.

Only one effective provider is active. Use **Verify AI Runtime** to test the configured route. Lum Harbor never displays the saved API key back through the UI.

### Local Ollama

Lum Harbor can use an Ollama model through an OpenAI-compatible route. Start Ollama, make sure the requested model is installed, and configure the provider with the local OpenAI-compatible endpoint (commonly `http://127.0.0.1:11434/v1`) and the exact model name. The accepted V1 product world used `qwen3:14b` through the official AI Runtime Plugin.

Local models can be substantially slower than cloud models. Responses taking one or several minutes are possible, especially while background Source interpretation is using the same model. Do not quit simply because **Thinking…** remains visible; check History for the final AI Run outcome.

Relative dates such as “next Tuesday” can be interpreted imperfectly by local models. Prefer explicit dates, times, and time zones, and always verify them in Proposal Review before Apply.

## 5. Install and configure Plugins

Lum Harbor Plugins are distributed as `.lhplugin` files. Open **Settings → Extensions** and choose **Add Plugin**, then select the package in the native file picker.

![Settings showing the four installed official Plugins](../assets/settings-plugins.png)

After installation:

1. Restart Lum Harbor so the package can be discovered.
2. Open the Plugin in **Extensions**.
3. Choose **Add Instance**.
4. Select one of the Plugin's capabilities and complete its non-secret configuration.
5. Restart again to make the selected instance part of the runtime.

An installed Plugin and a configured instance are different things. One installed Plugin can have multiple instances, and one Plugin process can serve those instances.

Use **Stop Using** to disable an instance and **Use Again** to re-enable it. These actions change the next-start configuration; they do not stop or start the running process immediately. To remove a Plugin, stop using every instance, restart, and then use **Remove Plugin**. V1 preserves the Plugin's data and cache directories during removal.

The official V1 Plugins are:

| Plugin                          | User purpose                                                                                                  |
| ------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| `lh-plugin-ai-runtime`          | Provides the effective AI runtime for Assistant and Source interpretation.                                    |
| `lh-plugin-apple-reminders`     | Reads Apple Reminders and synchronizes them into Planner as external items.                                   |
| `lh-plugin-macos-notifications` | Delivers Lum Harbor Notifications through macOS Notification Center.                                          |
| `lh-plugin-synthetic-project`   | Supplies deterministic project-like Source facts and bounded read queries for demonstration and verification. |

There is no Plugin marketplace, remote registry, automatic update, replace-in-place, or hot reload in V1. For the complete lifecycle, see [Plugin Usage](plugin-usage.md). For authoring and package details, see [Plugin Development](plugin-development.md).

## 6. Use the Assistant and review Proposals

Open **AI Assistant** from the upper-right corner. The context chip shows the current page or selected WorkItem. Context helps the Assistant, but it is not a permission boundary: the Assistant can answer application-level questions and can ask for additional bounded reads when needed.

The Assistant has two outcomes:

- **Answer**: a read-only response based on available Lum Harbor or Plugin facts.
- **Suggested Change**: a typed Proposal that does not change authoritative state until you choose **Apply**.

Here is a real read-only answer. The Workflow itself is at Testing, while the Assistant correctly reports the independently derived Tracked Item progress.

![Assistant answering which Tracked Items are in progress](../assets/assistant-tracked-items-answer.png)

### Create through the Assistant

A typical first prompt is:

> Create a work item called Product Release.

The expected flow is:

1. Lum Harbor records the user intent and runs the AI provider.
2. The Assistant creates a pending Suggested Change.
3. Open the item under **Needs Review**.
4. Inspect the reason, source, and exact **Proposed Changes**.
5. Choose **Apply** or **Reject**.

![Proposal Review showing source, proposed title, impact, Reject, and Apply](../assets/proposal-review.png)

The purple **Previewing Proposed Change** banner is a temporary preview. It is not saved as domain truth. **Reject** closes the Proposal without changing the WorkItem, Planner, or Tracking state. **Apply** rechecks the current authoritative state and performs one validated transaction. If the target changed since the Proposal was created, the Proposal can become **STALE** instead of overwriting newer truth.

Assistant intent is retained as provenance. In user terms, Lum Harbor remembers what you asked, what the AI proposed, what you approved, and what finally changed.

If an AI or validation step fails, Lum Harbor fails closed: no Proposal Apply and no authoritative mutation occurs.

## 7. WorkItems

A WorkItem represents a meaningful outcome that lasts longer than a single Todo: a release, a feature, a migration, or a project objective.

![Work Items overview showing one active project and derived Tracked Item summary](../assets/work-items-overview.png)

A WorkItem contains a title, optional description, lifecycle, optional Workflow, Tracked Items, MonitorPoints, and related History.

The lifecycle states are **ACTIVE**, **PAUSED**, **COMPLETED**, and **CANCELLED**. The visible action buttons provide valid transitions such as **Pause**, **Complete**, **Cancel**, **Resume**, or **Reopen**. Lifecycle actions are direct, deterministic user commands.

Title and description changes are different: they remain Proposal-first. For example:

> Update only the description of Product Release to: Final customer validation and release preparation.

Description-only and title-only updates are supported. Omitted fields preserve their current authoritative value.

The **Create Work Item** button supports a direct manual creation path. For richer setup—description, Workflow, Tracked Items, dependencies, and deadlines—the Assistant is the primary control surface.

## 8. Workflow

A WorkItem can have at most one Workflow. The Workflow binds the WorkItem to one ordered, versioned set of stages and stores its own current step and actual start time.

For example, with the WorkItem open:

> Create a workflow with Design, Development, Testing, and Release.

Review and Apply the resulting Proposal. The new Workflow begins at its first stage unless the Proposal specifies another valid current step.

![WorkItem Workflow with ordered stages, independent item positions, and dependencies](../assets/workflow-workspace.png)

The Workflow current step can move forward or backward through an Assistant Proposal:

> Move this workflow into Testing.

Important: the Workflow current step is **not** the current stage of every Tracked Item. In the screenshot, the Workflow was moved to Testing, while Backend API is independently in Development and the other items remain at Design. Moving the Workflow does not rewrite Tracked Item step states, complete the WorkItem, or complete any Todo.

Workflow definitions are fixed after creation. V1 has no Workflow edit, migration, deletion, lifecycle, or completion state.

## 9. Tracked Items and dependencies

A Tracked Item is one unit of progress inside the WorkItem's Workflow. Create them through Assistant Proposals, one reviewed item per turn in V1, for example:

> Add Backend API as a tracked item.

Repeat for Frontend UI and Deployment.

Each Tracked Item has authoritative per-step states. Lum Harbor derives the item's current position and overall state from those rows; it does not store a separate “current status” field that could drift out of sync.

Common derived states include:

- **NOT_STARTED**: the current stage has not started.
- **IN_PROGRESS**: work is active at the current stage.
- **BLOCKED**: an impediment prevents progress.
- **WAITING**: the item is waiting on something.
- **FAILED**: the current stage failed.
- **UNKNOWN**: the current state is not known.
- **COMPLETED**: every Workflow stage is passed.

`SKIPPED` is a valid step state and counts as passed when progression is calculated.

![Tracked Item detail showing Development, In progress, and the items it blocks](../assets/tracked-item-detail.png)

Dependencies are bounded `DEPENDS_ON` relationships between active Tracked Items in the same Workflow. For example:

> Make Frontend UI depend on Backend API.

Lum Harbor rejects self-dependencies, duplicates, cross-Workflow links, and cycles. The Workflow view draws dependency arrows; the detail view also shows the inverse **BLOCKS** list.

Useful read-only question:

> Which tracked items are currently in progress? Tell me their current stage and state.

Changing a Tracked Item's stage does not move the Workflow's current step. Moving the Workflow does not change a Tracked Item.

## 10. Planner and Todos

Planner combines calendar membership with an **Open** task view. A Todo may have an exact time, a date only, or no date. Dated items appear in the calendar; undated open items remain available in **Open**.

Create a Todo through the Assistant:

> Create a Todo to verify the Backend API on October 2, 2026 at 3 PM America/Chicago.

From a WorkItem, **Schedule Action** creates a local Todo associated with that WorkItem and, when available, its Workflow/current step. The Planner inspector shows this origin so the action is not detached from its longer-running outcome.

![Planner inspector showing a completed WorkItem-linked Todo and satisfied reminder](../assets/planner-todo.png)

You can complete a local Todo with its checkbox or detail action. A Todo managed by an external source is read-only in Lum Harbor and must be completed in its source application.

V1 does not provide general-purpose Planner edit, archive/delete, or reopen controls. If a newly proposed Todo has the wrong title or date, Reject it and ask the Assistant for a corrected Proposal. There is no recurring Todo engine.

## 11. Reminders

A Todo Reminder is long-lived intent to watch one Todo until it is completed or the reminder is cancelled. It is not the Notification itself.

For a dated Todo, the inspector can configure one or more thresholds such as several days before, one day before, or **At due time**. When thresholds are reached, Lum Harbor uses this chain:

```text
Todo → Reminder → Attention → Notification
```

If Lum Harbor was not running when several thresholds passed, V1 suppresses a burst of old alerts and emits at most the most recent passed threshold.

Completing the Todo:

- marks its Reminder **SATISFIED**;
- prevents future thresholds from firing;
- resolves related open Attention;
- leaves existing Notifications as durable communication records;
- does **not** advance the Workflow.

Undated Todos can exist, but a time-based Reminder is not meaningful until the Todo has a date. Because V1 has no general Todo date-edit UI, review a proposed date carefully before Apply.

## 12. MonitorPoints

A MonitorPoint is an explicit expectation that a WorkItem or Tracked Item should reach a target by a particular time.

Examples:

- “Product Release must be COMPLETED by October 2, 2026 at 5 PM America/Chicago.”
- “Deployment should have the Release step completed by that deadline.”

Create one through the Assistant and Proposal Review:

> Create a deadline requiring this WorkItem to be completed by October 2, 2026 at 5 PM America/Chicago.

MonitorPoint states are **ACTIVE**, **SATISFIED**, and **CANCELLED**. State changes are explicit; an overdue MonitorPoint normally stays ACTIVE until it is satisfied or cancelled. **Overdue** is a derived condition based on the deadline, current time, and unmet expectation—it is not a stored MonitorPoint state.

When an active expectation is due and unmet, the Scheduler opens Attention and creates a Notification. If the expectation later becomes met or the MonitorPoint is cancelled, related Attention resolves. Use the Assistant to propose **SATISFIED** or **CANCELLED**; terminal MonitorPoints cannot be reactivated in V1.

## 13. Attention and Notifications

These are related but distinct:

- **Attention** is current system state: something needs consideration. It can be open, resolved, or temporarily snoozed.
- **Notification** is the communication record that tells you about that Attention. It can remain visible and unread even after the underlying Attention resolves.

![Notifications page with a resolved Todo reminder and resolved deadline alert](../assets/notifications.png)

Lum Harbor can present Notifications in three ways:

- an in-app toast;
- the durable **Notifications** page and header unread badge;
- macOS Notification Center through `lh-plugin-macos-notifications`.

Use **Open Todo** or **Open Item** to navigate to the affected object. **Mark Read** only changes read state; it does not resolve Attention or complete work. Snoozing suppresses current presentation until the snooze expires; it does not change underlying truth.

On macOS, grant Notification permission to the Lum Harbor Notification Bridge. Normal alerts use standard presentation. Critical/Severe alerts use heightened V1 presentation, including a distinct subtitle and sound. Lum Harbor does **not** claim Apple's special Critical Alert entitlement.

## 14. Apple Reminders integration

The Apple Reminders Plugin is read-only. After installation, create a `source.fetch` instance and grant macOS Reminders access when prompted.

Its path is deterministic:

```text
Apple Reminder → Source evidence → EXTERNAL Planner item
```

This path does not call AI and does not create a Proposal. A later title, notes, due-date, or completion change updates the same Planner item by stable external identity. An EXTERNAL item cannot be completed locally; update it in Apple Reminders.

Absence from a later fetch does not mean deletion. V1 preserves the last known Planner state and does not automatically archive an item that was not returned.

If macOS permission is denied, enable Reminders access in System Settings and restart Lum Harbor. If the first permission dialog stays open longer than the Plugin request timeout, restart after making your choice; V1 does not automatically restart a timed-out Plugin process.

## 15. Synthetic Project Plugin

`lh-plugin-synthetic-project` is a demo and reference integration. It produces deterministic project-like facts—workflows, stories, priorities, statuses, and updates—without an external account.

With its Source instance enabled, the path is:

```text
Synthetic project fact → Source → AI interpretation → Proposal → user review → Planner result
```

The Plugin can also answer bounded read queries about its own simulated data. It preserves simulation state across restart.

It does **not** represent an automatic Rally/Jira-to-Tracking mapping. Current generic Source interpretation can propose Planner Todos; it does not automatically turn arbitrary project records into WorkItems, Workflow stages, Tracked Items, or dependencies. Future project integrations may choose richer deterministic or reviewable mappings, but none should be assumed in V1.

## 16. Source and provenance

“Source” is Lum Harbor's user-facing idea of where meaningful information came from. It may be:

- a prompt you submitted to Assistant;
- an observation supplied by a Source producer;
- an Apple Reminder or Synthetic Project observation;
- an explicit manual create action.

Lum Harbor keeps this chain understandable:

```text
what arrived → what AI understood → what was proposed → what you approved → what changed
```

Structured Sources such as Apple Reminders can use deterministic synchronization. Semantic Sources are interpreted into pending Proposals. Neither a Source Plugin nor AI writes domain truth directly.

Derived actions—such as a Reminder firing, Attention opening, or a Notification being delivered—do not invent new Sources. They remain connected through History to the fact or intent that caused them.

## 17. History

History is an explanation layer, not a replay engine. Current WorkItems, Todos, Attention, and Notifications remain the authority; History records the action and causal trail.

The global History page offers:

- **Timeline**: newest-first events with search, date, and scope filters.
- **Traces**: linked events showing why Lum Harbor acted and what happened next.
- **Audit**: a dense event-oriented view for deeper inspection.

WorkItem **History** shows only events connected to that WorkItem.

![History Trace from Reminder configuration through Attention, Notification, delivery, and resolution](../assets/history-reminder-trace.png)

In a Trace, **Direct cause** means the immediately preceding causal action. Other related events can share the same approved change or Source without being direct parent/child events. For most users, the practical reading order is top to bottom: user action, evaluation, notification, and resolution.

Useful examples to look for:

- Assistant intent → AI Run → Proposal Created → Proposal Applied → WorkItem change.
- Todo Reminder Set → Attention Opened → Notification Created/Sent/Presented/Delivered → Attention Resolved.
- Apple Source observed → external Planner item created or updated.

## 18. Restart and durability

Lum Harbor persists important product state in SQLite. A normal cold restart preserves WorkItems, Workflow, Tracked Item states and dependencies, MonitorPoints, Todos, Reminders, Attention, Notifications, Sources, Proposals, and History.

Pending Proposals survive restart and remain reviewable. A stopped Plugin instance remains stopped after restart; an enabled instance is selected again. Plugin installation, instance configuration, **Stop Using**, **Use Again**, and removal all require restart because runtime discovery is fixed for one process lifetime.

If an AI operation is in flight during shutdown, it is marked failed rather than left running forever. V1 does not automatically retry it. Submit the request again or explicitly retry Source interpretation after reviewing the failure.

Lum Harbor V1 has no Backup/Restore feature. Protecting the local application data directory is currently an operating-system or administrator responsibility.

## 19. Common workflows

### A. Start a new project through Assistant

1. Open **AI Assistant**.
2. Ask: “Create a work item called Q4 Release.”
3. Open the Suggested Change.
4. Verify the title and reason.
5. Choose **Apply**.
6. Open the new WorkItem from **Work Items**.

### B. Add Workflow, Tracked Items, and dependencies

With the WorkItem open, submit and Apply these one at a time:

1. “Create a workflow with Design, Development, Testing, and Release.”
2. “Add Backend API as a tracked item.”
3. “Add Frontend UI as a tracked item.”
4. “Add Deployment as a tracked item.”
5. “Make Frontend UI depend on Backend API.”
6. “Make Deployment depend on Frontend UI.”

One Proposal represents one reviewed mutation. Splitting the prompts makes each change easier to verify.

### C. Ask what is in progress

Ask:

> Which tracked items are currently in progress? Tell me their current stage and state.

This is read-only. A correct response can report a Tracked Item in Development while the Workflow current step is Testing.

### D. Create an actionable Todo and Reminder

1. Ask: “Create a Todo to verify the Backend API on October 2, 2026 at 3 PM America/Chicago.”
2. Review the exact time and Apply.
3. Open the Todo in Planner.
4. Configure a reminder threshold such as **At due time**.
5. Complete the Todo when done; confirm the Reminder becomes **Satisfied**.

Alternatively, open a WorkItem and use **Schedule Action** to keep the Todo linked to that outcome.

### E. Set a deadline MonitorPoint

1. Ask: “Create a deadline requiring this WorkItem to be completed by October 2, 2026 at 5 PM America/Chicago.”
2. Verify subject, expectation, date, time, and timezone.
3. Apply.
4. If it becomes overdue, use **Notifications** or **History → Traces** to understand the resulting Attention.

### F. Sync an Apple Reminder

1. Install and configure `lh-plugin-apple-reminders`.
2. Restart and grant Reminders permission.
3. Create or update a Reminder in Apple's app.
4. Wait for the scheduled fetch/reconciliation cycle.
5. Find the EXTERNAL item in Planner.
6. Make later changes in Apple Reminders, not Lum Harbor.

### G. Understand why a notification appeared

1. Open **Notifications** and read the reason.
2. Use **Open Todo** or **Open Item** for the affected object.
3. Open **History → Traces**.
4. Select the reminder or monitor trace and follow it from cause to resolution.

## 20. Troubleshooting

### For normal users

**The Assistant is slow or shows Thinking for a long time.**

Local models can take minutes, and background Source interpretation may share the same provider. Wait, then check History for **AI Run Succeeded** or **AI Run Failed**. A failed operation changes nothing.

**The Assistant used the wrong relative date.**

Reject the Proposal. Repeat the request with an absolute date, exact time, and IANA timezone such as `America/Chicago`.

**A Plugin change did not take effect.**

Restart Lum Harbor. Install, configure, Stop Using, Use Again, and Remove all describe next-start state.

**A macOS notification did not appear.**

Confirm the macOS Notifications Plugin has an enabled `notification.deliver` instance, restart Lum Harbor, and allow the Lum Harbor Notification Bridge in macOS System Settings. The in-app Notification can still exist even if external delivery fails.

**Apple Reminders do not appear.**

Confirm the Plugin is installed, its Source instance is enabled, and macOS Reminders permission is granted. Restart after a denied, delayed, or newly granted permission. Updates are scheduled, not instantaneous.

**A Source is visible but no Proposal appeared.**

Open History and look for a failed AI Run. Failed semantic Sources are not retried automatically; use the explicit **Interpret** action again after correcting the provider problem. An existing pending Proposal may intentionally prevent a newer interpretation from being created until you Apply or Reject it.

**Apply produced STALE instead of changing the item.**

The authoritative target changed after the Proposal was created. Review the current object and ask the Assistant for a fresh Proposal.

### For developers running from source

Before the first public release, the project uses one evolving `00001_foundation.sql`. A database created against an older version can report migration version 1 while still having an obsolete schema. Use a fresh isolated `HOME`/data directory or deliberately recreate the disposable development database; never point acceptance work at a personal database.

Rebuilding the macOS app changes its ad-hoc signature and can reset Reminders permission. A new permission prompt after a rebuild is expected.

## 21. Current V1 limitations

- Workflow has no lifecycle or completion state.
- There is no recurring Todo or recurring Workflow-action engine.
- Plugins do not hot reload; there is no **Restart Now** action.
- There is no Plugin marketplace, remote registry, update/replace flow, or automatic update.
- There is no Backup/Restore feature.
- Planner has no general edit, archive/delete, or reopen UI.
- There is no automatic generic Source-to-Tracking mapping.
- MonitorPoints cannot be edited, rescheduled, reactivated, or automatically marked satisfied.
- There is no general calendar-event creation path.
- Dashboard is partial: it does not yet provide full Attention, recent-changes, notification-summary, or system-health cards.
- The System runtime page and **Run Now** background-job controls are not implemented.
- Native Windows runtime has not been fully product-verified.

These are current boundaries, not promises about a roadmap.
