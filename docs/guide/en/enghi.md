# Using enghi

enghi is a local-only app that combines a personal wiki with GTD. All your data lives in a local SQLite database on your machine and never leaves it.

GTD terminology is explained in [the introduction](/guide/gtd)—read that first if the terms are unfamiliar. This guide covers **how to navigate and use enghi's screens.**

## Where to Start {#start}

There is only one first step: **open the GTD view and dump everything on your mind.** Don't organize or evaluate anything yet—just capture one item per line, as many as you can think of.

The capture field is available on every screen. Press `c` on your keyboard to open quick capture without leaving your current view.

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

Enter a pattern in the recurrence field on the clarify screen, and **completing the task automatically generates its next occurrence.** Tasks are never generated ahead of time in bulk, so your list will never fill up with uncompleted recurring tasks.

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

Each new occurrence is created with a **scheduled date**, so it stays out of Next Actions until that day arrives.

To skip a single occurrence, select "Skip this instance" on the clarify screen. To stop the recurrence entirely, select "End recurring series". The Weekly Review lists all recurring series so you can **prune routines that are running only on inertia.**

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
| `j` / `k` | Move down / up a list |
| `Enter` | Open the selected item |
| `e` | Edit the article |

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
