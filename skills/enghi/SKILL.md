---
name: enghi
description: Use the user's local enghi wiki and GTD system. Use when the user wants to capture something for later ("add to my inbox", "remind me to", "後でやる", "Inbox に入れて"), save notes or a design decision as a wiki page ("write this up in the wiki", "wiki にまとめて", "メモしておいて"), look up something they wrote before ("my notes on X", "前に書いた○○のメモ"), keep a work log on a task ("log what I did on X", "作業ログに残して"), write a daily report from what was done ("write today's report", "日報を書いて", "今日やったこと"), or get help with GTD: planning the day ("what should I do today", "今日やること", "朝の確認"), clarifying the inbox, the weekly review ("週次レビュー"), stalled projects, next actions, waiting-for items.
---

# enghi

enghi is the user's local-only wiki + GTD server. Talk to it with `curl` on its JSON API
at:

    http://127.0.0.1:7777

Everything is in the user's own database, so treat it as theirs: **read freely, but
write only as described below.**

## Calling the API

- A `POST` must carry `-H 'Content-Type: application/json'`, or the server answers 415.
  Send it on `PUT` and `PATCH` too.
- **Never splice text into a JSON string by hand.** Titles and Markdown bodies contain
  quotes, backslashes and newlines. Build the body with `jq -n` and pipe it in:

  ```sh
  jq -n --arg title "$TITLE" --arg note "$NOTE" '{title: $title, note: $note}' |
    curl -s -X POST http://127.0.0.1:7777/api/tasks \
      -H 'Content-Type: application/json' --data-binary @-
  ```

  For a long body, write it to a temporary file first and use `--rawfile body FILE`.
  If `jq` is missing, build the JSON with `python3 -c 'import json,sys; ...'` instead.
- Errors come back as `{"error": "<code>", "message": "..."}`. `409` means a conflict
  (see below); do not retry the same write blindly.
- If curl cannot connect, tell the user the enghi server is not running
  (`enghi serve`, or `enghi install-agent -load` to keep it resident; for a Homebrew
  install, `brew services start enghi`) and stop.
- Slugs are often Japanese. URL-encode one before putting it in a path:
  `SLUG=$(jq -rn --arg s "$SLUG_RAW" '$s|@uri')`.
- Use `jq` to trim responses to the fields you need rather than reading whole payloads.

## Capture to the Inbox

For "do this later", "add to my inbox" and the like. Capturing commits the user to
nothing, so **do it right away without asking for confirmation.**

```sh
jq -n --arg title "..." --arg note "..." '{title: $title, note: $note}' |
  curl -s -X POST http://127.0.0.1:7777/api/tasks \
    -H 'Content-Type: application/json' --data-binary @-
```

- `title`: short and specific, in the language the user is speaking.
- `note`: what the user will need to pick it up again later — the repository, branch,
  file and line, the error message, the command that failed. A capture made mid-task is
  useless if it only makes sense in the moment.
- Report back in one line with the task id. Do not interrupt the work in progress
  further than that.

## Write a wiki page

For "write this up", "save this as a note" and the like.

1. Draft the page in Markdown: a title, the body, and a few tags. Write for the user
   reading it months later, without this conversation.
2. Link related pages with `[[Page title]]`. Get candidates from
   `GET /api/titles?q=<prefix>` or a search. A link to a page that does not exist yet is fine; it
   resolves once that page is created.
3. **Show the draft and wait for the user's go-ahead before saving.**
4. Create it:

   ```sh
   jq -n --arg title "..." --rawfile body draft.md --argjson tags '["tag1","tag2"]' \
     '{title: $title, body: $body, tags: $tags}' |
     curl -s -X POST http://127.0.0.1:7777/api/pages \
       -H 'Content-Type: application/json' --data-binary @-
   ```

5. Report the title and `http://127.0.0.1:7777/wiki/<slug>`. Any open view of enghi
   picks the change up by itself.

A `409` with `"error": "title_conflict"` means that title is taken; the response carries
`conflicting_page`. Ask whether to add to that page or use another title.

### Update an existing page

1. `GET /api/pages/<slug>` and keep its `title`, `body`, `tags` and `version`.
2. Make the change, show the user what changes, and wait for the go-ahead.
3. `PUT /api/pages/<slug>` with **all of** `title`, `body`, `tags` and the `version` you
   read. A different `title` renames the page; the old title stays as an alias.
4. A `409` with `"error": "version_conflict"` means someone saved in between; `current`
   holds the newer page. Redo the change on top of it and confirm again. **Never
   overwrite the newer version.**

## Look something up

For "my notes on X", "what did I write about Y":

```sh
curl -s -G http://127.0.0.1:7777/api/search --data-urlencode "q=..." \
  --data-urlencode "kind=page" | jq '.results[] | {slug, title, snippet}'
curl -s http://127.0.0.1:7777/api/pages/<slug> | jq '{title, tags, body, links, backlinks}'
```

- Search matches titles, aliases, tags and bodies, Japanese included. Try a couple of
  wordings before concluding there is nothing.
- Leave out `kind` to search projects, tasks, task work logs and areas as well. A `log`
  result is one entry of a task's work log: `title` is the task's, `task_id` says which
  task, and `id` is the entry.
- `links` and `backlinks` lead to related pages; follow them when the first page is not
  the whole story.
- If several pages could be the one meant, list them and ask.
- Say which pages you drew on, so the user can check them.

## GTD

The model follows the GTD method; `/guide/gtd` on the server explains it. The parts that
matter here:

- Task `state`: `inbox`, `next`, `waiting` (set `waiting_for` to who and what),
  `scheduled` (set `scheduled_on`, `YYYY-MM-DD`), `someday`, `filed`, `done`, `dropped`.
- **A next action is a physical, visible activity**: "Email Tanaka asking for the
  contract draft", not "Contract". Propose them in that form.
- A project is an outcome needing more than one action. Its `outcome` says in one
  sentence what done looks like. **An active project with no next action is stalled.**

Reading:

| | |
|---|---|
| `GET /api/dashboard` | Today at a glance: `gtd.today`, `gtd.upcoming` (deadlines in the next `gtd.deadline_warning_days` days), `gtd.inbox_count`, `gtd.waiting_overdue`, `gtd.stalled_projects`. A task with a deadline carries `deadline_days`: days left, negative when overdue |
| `GET /api/review` | Everything the weekly review needs, in one call (see below) |
| `GET /api/tasks?state=inbox` | Tasks by state, as `{"tasks": [...]}`; `state=next_actions` gives the Next Actions view |
| `GET /api/tasks/<id>` | One task and the pages it links to. `working: true` means it has been started and not paused |
| `GET /api/tasks/<id>/logs` | The task's work log, oldest first, as `{"working": ..., "logs": [...]}` |
| `GET /api/day?date=YYYY-MM-DD` | One day's work record (today by default); see "The daily report" |
| `GET /api/days?month=YYYY-MM` | Per-day counts for a month, `{"days": {"2026-09-26": {"done", "dropped", "logs"}}}`; days with nothing are absent |
| `GET /api/projects?status=active` | Projects (`active`, `someday`, `done`, `dropped`) |
| `GET /api/projects/stalled` | Active projects with no next action |
| `GET /api/projects/<id>` | A project, its tasks and linked pages |
| `GET /api/contexts`, `GET /api/areas` | Contexts and areas of responsibility |

Writing — **propose first, write only after the user agrees** (capture is the one
exception):

| | |
|---|---|
| `PATCH /api/tasks/<id>` | Change fields or move a task: `{"state": "next", "version": N}`. Send only the fields that change, plus the `version` you read |
| `POST /api/tasks/<id>/complete` | Complete a task, body `{}` |
| `POST /api/tasks/<id>/file` | File an inbox item as a wiki page: `{title, body, tags}` |
| `POST /api/tasks` | New task: `{title, note, state}`. To put it in a project, `PATCH` it with `project_id` afterwards |
| `POST /api/projects` / `PATCH /api/projects/<id>` | `{title, outcome, status}` |
| `POST /api/tasks/<id>/logs` | Append to the work log: `{body}` for a note, `{kind: "start"}` / `{kind: "pause"}` with an optional `body` comment |
| `PATCH /api/task-logs/<id>` | Rewrite an entry: `{body, version}` |
| `DELETE /api/task-logs/<id>` | Remove an entry |

A task or project `409` works like a page one: re-read, redo, confirm.

### Log work on a task

Each task keeps a **work log**: timestamped Markdown entries recording what was tried,
what was found and what was decided. It is where the running record of the work goes;
the task's `note` stays a one-line reminder. For "log what I did on X", "作業ログに残して"
and the like — including at the end of a work session on something that has a task:

1. Find the task: `GET /api/tasks?state=next_actions`, or search with `kind=task`. If
   more than one could be meant, ask.
2. Draft the entry for the user reading it weeks later: what was done, what was found
   (error messages, numbers, commands), what was decided and why, what is left. Link
   pages with `[[Page title]]`. Do not repeat what earlier entries already say
   (`GET /api/tasks/<id>/logs`).
3. **Show the text and wait for the go-ahead**, as for other GTD writes.
4. Append it:

   ```sh
   jq -n --rawfile body entry.md '{body: $body}' |
     curl -s -X POST http://127.0.0.1:7777/api/tasks/<id>/logs \
       -H 'Content-Type: application/json' --data-binary @-
   ```

Start and pause mark when the user was actually working on a task. Write them only when
the user says they are starting or stopping; that request is the go-ahead. A start on a
task already started, or a pause on one that is not, changes nothing and answers
`created: false`. Completing or dropping a task ends the work by itself — no pause is
needed. Moving a working task to any other open state (inbox, later, waiting, scheduled,
someday) writes a pause by itself, with `moved_to` set to the new state; do not add one.
Moving it to next keeps it working. Editing an entry takes the `version` you read; a `409` works like a page one.

### Clarify the inbox

Take items **from the top, one at a time**; never skip the hard ones. For each, propose
where it goes and why:

- Not actionable → drop it (`state: dropped`), file it as reference
  (`/file`), or `someday`.
- More than one step → a project, with an outcome and a first next action.
- Under two minutes → say so; the user may just do it now.
- Someone else's to do → `waiting`, with `waiting_for`.
- Tied to a date → `scheduled`, with `scheduled_on`.
- Otherwise → `next`, rewritten as a concrete action if it is vague.

Let the user accept, change or skip each proposal before moving on.

### The daily check

For "what should I do today", a morning check-in and the like. It takes a few minutes,
not an hour; keep it short and do not turn it into a weekly review.

```sh
curl -s http://127.0.0.1:7777/api/dashboard | jq '.gtd | {inbox_count, today, upcoming, waiting_overdue}'
curl -s 'http://127.0.0.1:7777/api/tasks?state=next_actions' |
  jq '[.tasks[] | {id, title, context_name, project_title, deadline_on, energy, time_estimate, working}]'
```

1. **Today** — `today` holds what is due or scheduled for today or earlier. Lead with
   these, overdue deadlines (negative `deadline_days`) first, then briefly the deadlines
   of the coming week in `upcoming`. Mention any next action with `working: true`: it
   was started and never paused, so it is either still in progress or a pause was
   missed. Scheduled tasks show up in Next Actions by themselves
   once their day comes; they need no state change.
2. **Inbox** — if `inbox_count` is not zero, offer to clarify it (as below). If the user
   has no time now, just say how many are waiting and move on.
3. **Waiting for** — mention items in `waiting_overdue` (waiting a week or more) and offer
   to capture a follow-up.
4. **Pick for today** — ask how much time they have and where they are, then suggest a
   few next actions that fit: deadlines in the next few days first, then by context,
   `energy` and `time_estimate`. Group the list by `context_name`.

enghi has no "today" list, so the picks stay in the conversation; do not change tasks to
record them. Completing, moving or rewriting tasks follows the usual rule: propose, then
write after the user agrees. Leave stalled projects for the weekly review unless the user
asks.

### The daily report

For "write today's report", "日報を書いて", "what did I do yesterday" and the like: a short
report of one day's work, drawn from the work record. The user sees the same day at
`http://127.0.0.1:7777/gtd/day/YYYY-MM-DD`.

```sh
curl -s 'http://127.0.0.1:7777/api/day?date=2026-09-26' | jq '{date, weekday,
  done:    [.done[]    | {title: .task.title, project: .task.project_title, completed_at, logs, earlier_logs}],
  working: [.working[] | {title: .task.title, project: .task.project_title, since, logs}],
  worked:  [.worked[]  | {title: .task.title, project: .task.project_title, logs}],
  dropped: [.dropped[] | {title: .task.title, completed_at}]}'
```

Leave out `date` for today. A day is the user's **local** calendar day.

- `done` — completed that day. `working` — started and not paused at the end of the day
  (for today: still in progress now). `worked` — anything else with a log entry that day.
  `dropped` — dropped or skipped that day.
- `logs` are only that day's entries of the task's work log, oldest first: `kind` is
  `note`, `start` or `pause`, and `body` is raw Markdown (may be empty for start/pause).
  A pause with `moved_to` was written by moving the task to that state, not by the user.
- `earlier_logs`, on `done` items only, are the task's last three entries with a body
  from **before** that day, oldest first — the context of a task worked on over several
  days. They are not that day's work.
- **The day's own times — `completed_at`, `since`, each log's `created_at` — are ISO 8601
  in local time with the offset** (`2026-09-26T14:05:00+09:00`); `timezone` names the zone.
  The embedded `task` object keeps the API's usual UTC `YYYY-MM-DD HH:MM:SS` fields; do
  not mix the two when quoting times.
- `format=markdown` gives the same day as plain Markdown (what the page's copy button
  copies), if the user wants the raw record rather than a report.

1. Fetch the day. If it is empty, say so in one line; do not pad the report.
2. Write a short report **in the language the user is speaking**:
   - **Done** — what was finished, and what it amounted to: use `earlier_logs` for
     context when the day's own `logs` say little. Do not report earlier entries as
     that day's work.
   - **In progress** — what is under way and where it stands, from the latest log entries.
   - **Findings and decisions** — anything notable in the log bodies: causes found,
     numbers, choices made and why. Quote sparingly; summarise.
   - **Next** — what comes next, from the last entries of in-progress tasks and the
     next actions of the projects involved. Do not invent plans the log does not support.
   Keep it to what a colleague would read in a minute. Link pages the logs mention with
   `[[Page title]]`.
3. **Show it in the conversation first.** That is the report; stop here unless asked.
4. Only if the user asks to save it: a wiki page titled `日報 YYYY-MM-DD` with tag `日報`
   (in English, `Daily report YYYY-MM-DD` with tag `daily-report`), following "Write a
   wiki page" above — show the final text and wait for the go-ahead. If the create answers
   `409 title_conflict`, the day already has a report: read it from `conflicting_page`,
   show what would change, and on the go-ahead `PUT` it with the `version` you read, as in
   "Update an existing page". **Never overwrite it blindly.**

### The weekly review

`GET /api/review` returns:

| Field | |
|---|---|
| `checklist` | The review steps in order, each with `label` and `checked` |
| `inbox` | Unprocessed items |
| `next_actions` | The Next Actions list (group it by `context_name`) |
| `completed_last_week`, `upcoming` | The past week, and the next two weeks |
| `waiting` | Waiting-for items; `waiting_days` is how long each has waited |
| `stalled_projects` | Active projects with no next action |
| `someday_due_review` | Someday projects whose review date has come |
| `series` | Recurring series |

Walk the user through it in the order of `checklist`, one step at a time:

1. **Loose ends** — ask whether anything outside enghi (notes, mail, chat) still needs
   capturing, and capture it.
2. **Inbox** — clarify it to zero as above.
3. **Mind sweep** — ask what else is on their mind and capture each answer.
4. **Next actions** — point out items that are done, obsolete, or too vague to act on.
5. **Calendar** — look back over `completed_last_week` and ahead over `upcoming`.
6. **Waiting for** — flag long waits and offer to capture a follow-up.
7. **Projects** — **the step that matters most.** For each stalled project, read
   `GET /api/projects/<id>` (its outcome, finished tasks, linked notes) and propose one
   concrete next action. Also ask whether the project is still wanted at all; `someday`
   or `dropped` are fine answers.
8. **Someday/maybe** — for each project in `someday_due_review`, ask: activate, keep
   (with a new `review_on`), or drop.
9. **Recurring** — go over `series` and ask whether each still earns its place.

Keep a running tally of what was changed. At the end, summarise it and remind the user
to tick off the checklist and complete the review at
`http://127.0.0.1:7777/gtd/review` — that part is done in the UI, not the API.
