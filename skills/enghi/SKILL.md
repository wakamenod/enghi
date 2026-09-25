---
name: enghi
description: Use the user's local enghi wiki and GTD system. Use when the user wants to capture something for later ("add to my inbox", "remind me to", "後でやる", "Inbox に入れて"), save notes or a design decision as a wiki page ("write this up in the wiki", "wiki にまとめて", "メモしておいて"), look up something they wrote before ("my notes on X", "前に書いた○○のメモ"), or get help with GTD: clarifying the inbox, the weekly review ("週次レビュー"), stalled projects, next actions, waiting-for items.
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
- Leave out `kind` to search projects, tasks and areas as well.
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
| `GET /api/review` | Everything the weekly review needs, in one call (see below) |
| `GET /api/tasks?state=inbox` | Tasks by state; `state=next_actions` gives the Next Actions view |
| `GET /api/tasks/<id>` | One task and the pages it links to |
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

A task or project `409` works like a page one: re-read, redo, confirm.

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
