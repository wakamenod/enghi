# Using enghi

enghi is a local-only app that combines a personal wiki with GTD. All your data lives in a local SQLite database on your machine and never leaves it.

GTD terminology is explained in [the introduction](/guide/gtd)—read that first if the terms are unfamiliar. This guide covers **how to navigate and use enghi's screens.**

## Where to Start {#start}

There is only one first step: **open the GTD view and dump everything on your mind.** Don't organize or evaluate anything yet—just capture one item per line, as many as you can think of.

The capture field is available on every screen. Press `c` on your keyboard to open quick capture without leaving your current view.

If a line contains a URL (`http://` or `https://`), enghi moves it out of the title and into the task's URL. `Read this https://example.com/post` becomes a task named "Read this" that links to the page.

Capture 30 items or 50—it doesn't matter. What you end up with is your **Inbox**. Next, clarify it one item at a time.

## Inbox — The Unprocessed Pile {#inbox}

Everything you capture lands here first. Nothing here has been decided yet.

Click an item to open the **clarify screen**. Process items one by one from the top.

**Emptying the Inbox is a daily habit.** Spend a few minutes a day working from the top. You won't clear it out completely every day, and that's fine — but **it must be empty once a week, during the Weekly Review.** That is your backstop.

## The Clarify Screen — Action or Reference {#clarify-screen}

Opening an Inbox item reveals two panels side by side. Use this screen to decide whether the item is an **action** or **reference material**.

**To treat it as an action** — use the left panel:

- Rewrite the **action** as a single physical activity starting with a verb. Not "expenses", but "Scan receipts and send them to accounting."
- Choose a **state** (see the table below).
- **Project / Context / Area** are optional. Leave them empty if none apply.
- **URL** links the task to a web page, such as an issue or an article. Only `http://` and `https://` work. In lists, a task with a URL shows 🔗. Click it, or select the task and press `o`, to open the page in a new tab.
- Use **Scheduled Date** for items you don't want to see until that date arrives. Use **Deadline** only for hard, immovable due dates. They are fundamentally different—assigning arbitrary deadlines strips them of their urgency.

**To treat it as reference** — use the right panel:

Enter a title and body, then click "Create Article". The item **becomes a wiki article**, and the original item is preserved as "filed". This is a third destination—neither completed nor discarded. The new article and the original item remain linked.

**If it is neither an action nor reference**, click "Delete" at the bottom to discard it. Trashing items is not a failure; it is a valid outcome of clarifying.

### Choosing a State {#states}

These are the available states on the clarify screen. **This distinction is the core of enghi**, so return to this table whenever you are unsure.

<div class="dg-wrap"><svg class="dg" viewBox="0 0 830 370" role="img" aria-label="Where items go when they leave the Inbox"><defs><marker id="a3en" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path class="head" d="M 0 0 L 8 4 L 0 8 z"/></marker></defs><rect class="box" x="20" y="158" width="150" height="44" rx="6"/><text class="mono" x="95.0" y="176.0" text-anchor="middle">inbox</text><text class="small" x="95.0" y="193.0" text-anchor="middle">Unprocessed item</text><rect class="box" x="250" y="20" width="250" height="44" rx="6"/><text class="mono" x="375.0" y="38.0" text-anchor="middle">next</text><text class="small" x="375.0" y="55.0" text-anchor="middle">can be done right now</text><path d="M 170 180 L 210 180 L 210 42 L 248 42" marker-end="url(#a3en)"/><rect class="box" x="250" y="76" width="250" height="44" rx="6"/><text class="mono" x="375.0" y="94.0" text-anchor="middle">later</text><text class="small" x="375.0" y="111.0" text-anchor="middle">a follow-on; make it next when its turn comes</text><path d="M 170 180 L 210 180 L 210 98 L 248 98" marker-end="url(#a3en)"/><rect class="box" x="250" y="132" width="250" height="44" rx="6"/><text class="mono" x="375.0" y="150.0" text-anchor="middle">waiting</text><text class="small" x="375.0" y="167.0" text-anchor="middle">waiting on someone else</text><path d="M 170 180 L 210 180 L 210 154 L 248 154" marker-end="url(#a3en)"/><rect class="box" x="250" y="188" width="250" height="44" rx="6"/><text class="mono" x="375.0" y="206.0" text-anchor="middle">scheduled</text><text class="small" x="375.0" y="223.0" text-anchor="middle">joins next when the date arrives</text><path d="M 170 180 L 210 180 L 210 210 L 248 210" marker-end="url(#a3en)"/><rect class="box" x="250" y="244" width="250" height="44" rx="6"/><text class="mono" x="375.0" y="262.0" text-anchor="middle">someday</text><text class="small" x="375.0" y="279.0" text-anchor="middle">not committed to</text><path d="M 170 180 L 210 180 L 210 266 L 248 266" marker-end="url(#a3en)"/><rect class="box" x="250" y="300" width="250" height="44" rx="6"/><text class="mono" x="375.0" y="318.0" text-anchor="middle">filed</text><text class="small" x="375.0" y="335.0" text-anchor="middle">reference, not an action</text><path d="M 170 180 L 210 180 L 210 322 L 248 322" marker-end="url(#a3en)"/><path d="M 500 42 L 545 42"/><path d="M 500 98 L 545 98"/><path d="M 500 154 L 545 154"/><path d="M 500 210 L 545 210"/><path d="M 500 266 L 545 266"/><path d="M 545 42 L 545 266"/><path d="M 545 154 L 608 154" marker-end="url(#a3en)"/><rect class="end" x="610" y="132" width="200" height="44" rx="6"/><text class="mono end-t" x="710.0" y="158.5" text-anchor="middle">done / dropped</text><path d="M 500 322 L 608 322" marker-end="url(#a3en)"/><rect class="box" x="610" y="300" width="200" height="44" rx="6"/><text class="small" x="710.0" y="326.5" text-anchor="middle">becomes a wiki article</text></svg></div>

| State | Meaning | Where It Appears |
|---|---|---|
| `inbox` | Unprocessed; not yet clarified | Inbox |
| `next` | A single action you can do right now | Next Actions |
| `later` | A subsequent action in a project; not its turn yet | Project details only |
| `waiting` | Delegated; waiting on someone else | Waiting For |
| `scheduled` | Deferred until a specific date | Scheduled; joins Next Actions when the date arrives |
| `someday` | Someday/Maybe | Someday/Maybe |
| `filed` | Reference material; converted into an article | (Hidden from task lists) |
| `done` | Completed | (Hidden from task lists) |
| `dropped` | Discarded | (Hidden from task lists) |

**`later` and `someday` are different.** `later` means "committed, but not yet next," waiting in line inside a project. `someday` means "not committed." The former waits inside its project; the latter asks you "still not doing this?" at every review.

## Dashboard — Today at a Glance {#dashboard}

The dashboard (`g` `d`) opens with the state of your GTD system: the Inbox count, what is due today, stalled projects, and Waiting For items delegated more than a week ago.

- **Overdue** comes first: tasks whose **deadline** has passed, with how many days late. A scheduled date that has passed is not overdue; the task simply shows up under Today.
- **Today** lists tasks with a deadline or scheduled date of today or earlier.
- **Deadlines in the next 7 days** gives warning before a deadline arrives: tomorrow through a week ahead, soonest first, with the days left. Nothing already under Today appears here, and Someday items are left out. Change the window with `deadline_warning_days` in `config.toml`.

## Next Actions — The Active List {#next}

**This is the screen you will use most.** It holds only actions you can physically perform right now. `scheduled` tasks automatically appear here once their scheduled date arrives (no need to change their state manually).

Filter by context at the top. Select `@phone`, for example, to see only tasks you can do over the phone.

**If this screen fills with vague items like "Smith issue", your clarifying is slipping.** Open the item and rewrite it as the next concrete, physical action.

## Waiting For — Waiting on Others {#waiting}

This list tracks everything you have delegated. Each row shows **how many days have passed since delegation.**

The work is out of your hands, but following up remains your responsibility. Scan this list during your Weekly Review and follow up on anything that has stalled. The day counter helps you spot overdue items.

## Scheduled (Tickler) — Hidden Until Needed {#scheduled}

This screen lists tasks with a scheduled date. **They stay out of Next Actions until that date arrives.**

Putting "think about this next month" here keeps it out of sight until then. The benefit is being able to safely forget about it in the meantime.

When you move a task here (click its title, or press `s` on its row), the **Repeat** picker under the date turns it into a recurring task: every N days/weeks/months/years, on chosen weekdays, on a day of the month, or once a year. The weekday and day default to the date you picked. Each occurrence waits here until its day, then shows up in Next Actions on its own. See [Recurring Tasks](#recurrence).

## Someday/Maybe — Not Doing Now {#someday}

A parking lot for things you might do, but aren't doing now.

**Always review this list during your Weekly Review.** An unreviewed Someday list is just a trash bin. Knowing you review it weekly gives you the confidence to park uncertain ideas here.

## Projects {#projects}

A project is any desired outcome requiring two or more actions. For details, see [the project section in the GTD introduction](/guide/gtd#project).

- **Title**: A short identifier ("Office move").
- **Outcome**: A single sentence describing what done looks like ("Moved into new office and operations have resumed"). This is optional, but **defining it makes your next actions obvious.** Projects without an outcome are flagged during your Weekly Review.
- **Project Support Material**: Links a single wiki article to the project for notes and research. Link any additional articles by typing `[[Article name]]` in the text.

### Stalled Project Detection {#stalled}

**This is the most valuable automated feature enghi provides.**

Any active project without a `next`, `waiting`, or `scheduled` task is flagged as stalled on both the dashboard and Weekly Review.

Inactive projects stop silently; that is what makes them dangerous. When a project appears here, define one next action you can take. If you can't, either demote it to Someday or redefine its desired outcome.

### Review Date {#review-on}

You can assign a **review date** to projects moved to Someday. When that date arrives, the project surfaces on the dashboard and in the Weekly Review as a Someday item due for reconsideration.

This is what makes Someday truly usable: knowing when an idea will resurface lets you safely shelve it.

<!--feature:areas-->
## Areas — Areas of Responsibility {#areas}

Areas are ongoing standards you maintain rather than finish (e.g., "Finances", "Health", "Hiring").

Linking projects and single actions to an Area shows you **everything currently in motion within that sphere of your life.** Single actions too small for a project can link directly to an Area, bypassing projects entirely.

You can also attach a single wiki article to an Area for reference notes.

<!--feature:contexts-->
## Contexts {#contexts}

Add contexts on the right side of the GTD screen. Starting with a few basics like `@phone`, `@home`, `@errands`, and `@email` is plenty.

**Don't create too many upfront.** It works much better to add contexts only when you find yourself wanting to filter by them.

## Weekly Review — One Hour a Week {#review-screen}

**This screen is the reason enghi exists.** Once a week, set aside about an hour and work through the checklist from top to bottom.

It is intentionally not a wizard. **All the data you need to make decisions sits on a single screen:** stalled projects, Someday items due for review, your Inbox, completed items from last week, upcoming dates and deadlines for the next two weeks, Waiting For elapsed days, and recurring task series. You can process everything in place without navigating away.

Clicking a checklist item jumps straight to its relevant data. When you finish, note any observations and click "Complete Review" to log the session.

**Even in a rushed week, always do these two: review your project list, and review Someday/Maybe.** Drop everything else if you must, but keep these two, and the system survives.

## Recurring Tasks {#recurrence}

Set a rule with the Repeat picker—in the move dialog's Scheduled step or on the clarify screen—and **completing the task automatically generates its next occurrence.** Tasks are never generated ahead of time in bulk, so your list will never fill up with uncompleted recurring tasks.

| Rule | Meaning | Example / Best for |
|---|---|---|
| `+1w` | One week after the previous **scheduled date** | Fixed calendar cadence |
| `.+2w` | Two weeks after the **completion date** | Washing sheets (cadence starts from when done) |
| `++1w` | Advances the scheduled date until it is in the future | Catching up on long-neglected tasks |
| `weekly:tue,fri` | Every Tuesday and Friday | Taking out the trash |
| `monthly:25` | The 25th of each month | Expense reports |
| `monthly:last` | Last day of the month | |
| `yearly:04-01` | Every April 1st | |

**The distinction between `+1w` and `.+2w` matters most in practice.** Use the former for fixed calendar days and the latter when the countdown should start from whenever you actually finished the task.

The picker writes every rule above except `++` (without JavaScript, the clarify screen shows a text field for the rule instead). A task that already has such a rule shows it as "Custom" and keeps it; choosing "Does not repeat" removes the rule.

Each new occurrence is created with a **scheduled date**, so it stays out of Next Actions until that day arrives.

To skip a single occurrence, select "Skip this instance" on the clarify screen. To stop the recurrence entirely, select "End recurring series". The Weekly Review lists all recurring series so you can **prune routines that are running only on inertia.**

## Work Log — What You Did on a Task {#work-log}

Every task has a **work log** at the bottom of its clarify screen: timestamped entries recording what you tried, what you found and what you decided. Entries are Markdown, as in articles—`[[links]]`, code blocks, and images pasted or dropped into the text box all work. `Ctrl`/`⌘`+`Enter` adds the entry.

**Start** and **Pause** mark when you were actually working on the task. While it is started, it shows a "Working" badge in the task lists, and `p` on a task in a list starts or pauses it. Working is not a state of its own: the task stays in Next Actions (or wherever it is), and completing or dropping it simply ends the work.

Moving a working task anywhere else that is still open—Inbox, Later, Waiting, Scheduled or Someday—pauses it for you. The log records it as "⏸ paused (moved to Someday)." A forgotten Pause no longer leaves the task working forever. Undoing the move takes that pause back, and the task is working again. Moving it to Next leaves the work running.

The log is included in search, labeled "Log"; when several entries of one task match, only its best one is shown. Entries can be edited or deleted, and deleting a Start or Pause takes back a mistaken press. There is no revision history for entries—an edit overwrites.

## Work Record — What You Did Each Day {#day}

The **work record** shows one day's work. Open it with `g` `l`, or with "Today's work →" on the dashboard. It lists the tasks you finished and the tasks you worked on, each with the work-log entries you wrote that day. The rows are task rows: `j` / `k` move between them, and the task keys and the move dialog work as in any list, returning to the same day.

It opens on today. To see another day, pick a date in the month calendar on the right, or press `[` and `]` for the previous and next day. Press `t` to come back to today, or type a date in the date field. Days with anything recorded have a dot in the calendar.

A day has four groups:

- **Done** — tasks completed that day, with the time. If you reopen a task later, it no longer appears here. A task you worked on over several days also shows its last three entries from before that day, with their dates, under "Earlier entries." They give the context of what you finished. Long ones start folded.
- **In progress** — tasks you had started and not paused by the end of the day. For today, these are the tasks you are working on now. For a past day, enghi works them out from the Start and Pause entries.
- **Worked on** — any other task with a log entry written that day.
- **Dropped** — tasks dropped that day, including skipped recurring occurrences. This group starts folded.

"Copy as Markdown" copies the day as plain Markdown, ready to paste into a report or a chat. Where the browser can't copy, such as inside Emacs, the text appears selected so you can copy it by hand.

The same data is available at `/api/day?date=YYYY-MM-DD`. It returns JSON, or Markdown with `&format=markdown`. When you ask the Claude Code skill for a daily report, it reads this to write a draft.

The dashboard also lists the tasks you are working on now. Each one shows when you started it and how long ago, such as "since 10:42 (2h 5m)." If you started it on an earlier day, the date comes first. This makes a forgotten Pause easy to spot. The Weekly Review links to the work record of each of the seven days before the review—the same days its "Completed last week" list covers.

## Calendar Events (macOS) {#calendar}

enghi can show your calendar events. It only reads them. Today's schedule appears at the top of the dashboard, and each day's events appear at the top of that day's work record. Any calendar in the macOS Calendar app works, including Google calendars you add under System Settings → Internet Accounts.

A Shortcuts shortcut named `enghi-events` reads the events. The permission to read your calendars belongs to Shortcuts, so updating enghi never asks for it again. enghi runs the shortcut every 30 minutes and stores the events from yesterday through the next 14 days. It keeps the events of earlier days, so a past day's work record still shows what was on the calendar.

To set it up:

1. If you use Google Calendar, add the account in System Settings → Internet Accounts and turn on Calendars. Check that its events appear in the Calendar app.
2. In Settings → Calendar, click **Add shortcut**. Shortcuts opens and asks whether to add `enghi-events`. Click **Add Shortcut**.
3. Open the shortcut in Shortcuts and run it once. If it asks for access to your calendars, allow it.
4. Back in Settings, turn on **Sync the calendar**, and save. The first sync starts right away.

All-day events come first, then timed events in order. Each shows the name of its calendar. On today's schedule, a "now" line follows the events that have started, and events that have ended turn pale. Both keep up with the clock without a reload. To hide a calendar from the screens, clear its checkbox under **Calendars to show**. enghi still syncs it, so when you select it again, its events come straight back.

**Make a task** turns an event into a task, named with the event's time and scheduled for the event's day. For example, "15:00–16:00 Planning meeting." It shows up in Next Actions on that day. A task from an all-day event has no time in its name. After that, the event links to its task.

Other tools can send events too, on any system. `PUT /api/calendar/events?source=NAME&from=YYYY-MM-DD&to=YYYY-MM-DD` takes a JSON array of events in the same shape the shortcut prints:

```json
[{"title": "Planning", "start": "2026-09-28T15:00:00+09:00", "end": "2026-09-28T16:00:00+09:00", "calendar": "Work", "location": "Room A"}]
```

The array replaces that source's events that start within those days. For an all-day event, `start` and `end` can be plain dates such as `2026-09-28`, with `end` as the last day. `GET /api/calendar/events?from=&to=` returns the stored events, leaving out hidden calendars.

**Troubleshooting**

- *"The shortcut is not installed."* Add it again from Settings. If you named it something else, set `calendar_shortcut` in the config file to that name.
- *The sync fails with a message from Shortcuts.* You probably haven't allowed calendar access yet. Run the shortcut once in the Shortcuts app, allow access, and click **Sync now**.
- *No events and no error.* The shortcut found nothing, which isn't an error. Check that the events appear in the Calendar app, and that you selected their calendar under **Calendars to show**.
- *Adding the shortcut fails.* Build it by hand, as the next section describes.

## Building the Calendar Shortcut by Hand {#calendar-manual}

If **Add shortcut** doesn't work, you can build the same shortcut in the Shortcuts app. Labels may differ slightly between macOS versions.

1. Create a new shortcut and name it exactly `enghi-events`.
2. Add **Find Calendar Events**. Click **Add Filter** and set it to **Start Date** · **is in the last** · **2** · **days**. Keep the sort at **Start Date**, **Oldest First**.
3. Add the **Add to Variable** action. Its input is the calendar events, and the variable name is `events`.
4. Add a second **Find Calendar Events** with the filter **Start Date** · **is in the next** · **14** · **days**, then another **Add to Variable** into `events`. Keep the two conditions in separate actions. One action with "Any" of both finds nothing.
5. Add **Repeat with Each** over the `events` variable.
6. Inside the loop, add **Dictionary** with five Text items:
   - `title`: the **Repeat Item** variable.
   - `start`: **Repeat Item**. Click it, choose **Start Date**, set **Date Format** to **ISO 8601**, and turn on **Include Time**.
   - `end`: the same, with **End Date**.
   - `calendar`: **Repeat Item**, set to **Calendar**.
   - `location`: **Repeat Item**, set to **Location**.
7. Still inside the loop, add **Text** that holds only the **Dictionary** variable. A dictionary turned into text is one line of JSON.
8. After **End Repeat**, add **Combine Text** on **Repeat Results**, with **New Lines**.
9. Add **Stop and Output** with **Combined Text**.
10. Run it once and allow calendar access. In Terminal, `shortcuts run enghi-events < /dev/null` should print one JSON object per event. When its standard input isn't a terminal, `shortcuts run` waits for that input to end. If it seems to hang when run from a script or a pipe, add `< /dev/null`.

## Wiki Integration {#wiki}

GTD and the wiki are independent. **The wiki works fully without touching GTD**, and vice versa.

They connect in only three places:

- Filing an Inbox item **as reference** turns it into a wiki article
- Projects and Areas can each hold **one dedicated article**
- Writing `[[Article name]]` in any article creates a link (even if the target doesn't exist yet)

## Keyboard Shortcuts {#keys}

| Key | Action |
|---|---|
| `c` | Quick capture into the Inbox, from any screen |
| `/` | Focus the search box |
| `g` `d` | Dashboard |
| `g` `i` | Inbox |
| `g` `n` | Next Actions |
| `g` `p` | Projects |
| `g` `w` | Articles |
| `g` `l` | Today's work record |
| `j` / `k` | Move down / up a list |
| `Enter` | Open the selected item; on a task, choose where to move it (so does clicking its title) |
| `u` | Undo the last move, while its notice is shown at the bottom of the screen |
| `p` | Start or pause work on the selected task |
| `o` | Open the selected task's URL in a new tab |
| `e` | Edit the article |
| `[` / `]` | Work record: previous / next day |
| `t` | Work record: back to today. With a task selected, rename it |

## Terminology {#glossary}

How terms in GTD literature map to screens and concepts in enghi.

| GTD Term | enghi Interface |
|---|---|
| Inbox / In-basket | Inbox |
| Next Actions | Next Actions |
| Waiting For | Waiting For |
| Calendar / Tickler | Scheduled |
| Someday/Maybe | Someday/Maybe |
| Projects | Projects |
| Project Outcome | Outcome |
| Project Support Material | Project Support Material (wiki article) |
| Contexts | Contexts |
| Areas of Responsibility | Areas |
| Reference Material | Filed as reference → wiki article |
| Weekly Review | Weekly Review |
