# Using enghi

enghi is a personal wiki and a GTD system in one, running locally. Everything lives in a SQLite file on your own machine and nothing leaves it.

The GTD vocabulary is explained in [the introduction](/guide/gtd); read that first if the terms are unfamiliar. This page is about **what to do on each screen.**

## Where to start {#start}

There is only one first step. **Open the GTD screen and type in everything that is on your mind.** No sorting, no deciding. One line per item.

The capture box is on every screen: press `c` and it opens wherever you are.

Thirty items, fifty items, it does not matter. What you end up with is your **Inbox**, and the next job is to work through it one item at a time.

## Inbox — the unprocessed pile {#inbox}

Everything you capture lands here. Nothing in it has been decided yet.

Click an item to open the **clarify screen**. Work from the top, one at a time.

Emptying the Inbox is the goal, but not every day. Once a week, during the Weekly Review, it must be emptied.

## The clarify screen — action or reference {#clarify-screen}

Opening an Inbox item gives you two panels side by side: treat this as an **action**, or as **reference material**.

**As an action** — the left panel.

- Rewrite the **action** as one physical activity starting with a verb. Not "expenses" but "Scan the receipts and send them to accounting."
- Pick a **state** (see the table below).
- **Project / context / area** are optional. Leave them empty if nothing fits.
- **Scheduled date** means "I do not want to see this until then." **Deadline** is for dates that genuinely cannot move. They are not the same field. Putting a vague deadline on everything makes deadlines meaningless.

**As reference** — the right panel. Give it a title and body, press create, and it **becomes a wiki article** while the original item is marked as filed. That is a third destination, neither done nor dropped. The new article and the item stay linked.

**If it is neither an action nor reference**, delete it. Throwing things away is a legitimate result of clarifying, not a failure.

### Choosing a state {#states}

These are the values in the state selector. **This distinction is the core of enghi**, so come back to this table whenever you are unsure.

| State | Meaning | Where it appears |
|---|---|---|
| `inbox` | Unprocessed; not yet decided | Inbox |
| `next` | A single action you can do now | Next Actions |
| `later` | A subsequent action in a project; not its turn yet | Project detail only |
| `waiting` | Delegated; you are waiting on someone | Waiting For |
| `scheduled` | Not to be started before a given date | Scheduled; joins Next Actions when the date arrives |
| `someday` | Someday / maybe | Someday / Maybe |
| `filed` | Reference, not an action; turned into an article | (not on any list) |
| `done` | Completed | (not on any list) |
| `dropped` | Discarded | (not on any list) |

**`later` and `someday` are different.** `later` means "committed, but not next"; `someday` means "not committed." The first waits inside a project; the second gets asked "still no?" at every review.

## Next Actions — the list you work from {#next}

**This is the screen you will open most.** It holds only actions you can physically do right now. Scheduled items whose date has arrived appear here automatically — you never have to change their state by hand.

Filter by context at the top. Pick `@phone` and you see only what you can do on the phone.

**If this screen starts filling with items like "the Smith thing," your clarifying has gone slack.** Open the item and rewrite it as the next physical activity.

## Waiting For — what others owe you {#waiting}

Everything you delegated, each row showing **how many days since you handed it over.**

It is out of your hands but not off your mind. During the Weekly Review, read down the list and chase anything that has gone quiet. The day count is there for exactly that.

## Scheduled (Tickler) — hidden until the day {#scheduled}

Items with a scheduled date. **They do not appear in Next Actions until that date arrives.**

Put "think about this next month" here and it stops occupying you until then. Being able to forget it is the benefit.

## Someday / Maybe — not now {#someday}

Things you might do, but are not doing now.

**Review this list every week.** A someday list nobody reads is a bin. Read weekly, it becomes the place you can safely park anything you are unsure about.

## Projects {#projects}

Outcomes that take more than one action. See [the projects section of the introduction](/guide/gtd#project).

- **Title** is a short label ("Office move").
- **Outcome** is one sentence describing done ("We have moved into the new office and work has resumed"). It is optional, but **writing it makes the next action obvious.** Projects without one are flagged during the Weekly Review.
- **Project Support Material** links one wiki article to the project, for notes and research. Any other related article can be linked simply by writing `[[Article name]]` in the text.

### Stalled project detection {#stalled}

**This is the most valuable thing enghi does for you without being asked.**

Any active project with no `next`, `waiting` or `scheduled` task is reported as stalled, on the dashboard and in the Weekly Review.

A stopped project stops silently; that is what makes it dangerous. When a name shows up here, decide one action you can take. If you cannot, that is the signal to move it to someday or to rewrite the outcome.

### Review date {#review-on}

A project moved to someday can be given a **review date**. When that day arrives, it surfaces on the dashboard and in the Weekly Review as a someday item due for reconsideration.

This is what makes someday usable. Because you have decided when it comes back, you can let it sink.

## Areas {#areas}

Standards you maintain and never finish: Finances, Health, Hiring.

Attaching projects and single actions to an area shows you **what is currently moving in that part of your life.** An action too small to deserve a project can be attached directly to an area, skipping projects entirely.

An area can also hold one wiki article as its notes.

## Contexts {#contexts}

Add them on the right side of the GTD screen. `@phone`, `@home`, `@errands`, `@email` are enough to begin with.

**Do not create many up front.** Adding one when you actually want to filter works far better.

## Weekly Review — one hour a week {#review-screen}

**This screen is the reason enghi exists.** Once a week, set aside about an hour and work down the checklist.

It is deliberately not a wizard. **Everything you need in order to decide is on the same page:** stalled projects, someday items due for review, the Inbox, what you finished last week, the next two weeks of dates and deadlines, waiting-for items with days elapsed, and your recurring series. You can process all of it without navigating away.

Clicking a checklist item jumps to the matching data. When you are finished, write down what you noticed and complete the review; it is recorded.

**Even in a week with no time, do these two: review the project list, and review someday/maybe.** Cut everything else and the system still survives.

## Recurring tasks {#recurrence}

Put a rule in the "recurrence" field and **the next single occurrence is created when you complete the task.** Occurrences are never generated ahead of time, so the list cannot fill up with recurring items you never did.

| Rule | Meaning | Good for |
|---|---|---|
| `+1w` | One week after the previous **scheduled date** | Fixed cadence |
| `.+2w` | Two weeks after the **completion date** | Washing the sheets — counted from when you did it |
| `++1w` | Add the interval until the date is in the future | Catching up something long neglected |
| `weekly:tue,fri` | Every Tuesday and Friday | Taking the bins out |
| `monthly:25` | The 25th of each month | Expenses |
| `monthly:last` | Last day of the month | |
| `yearly:04-01` | Every April 1st | |

**The difference between `+1w` and `.+2w` is the one that matters in practice.** Fixed days use the former; "two weeks after I last did it" uses the latter.

The generated occurrence always carries a scheduled date, so it stays out of Next Actions until that day.

"Skip this one" is on the clarify screen, as is "end the series." The Weekly Review lists every recurring series, so you can **retire the ones that are only running out of inertia.**

## How this relates to the wiki {#wiki}

GTD and the wiki are independent. **The wiki works fully without using GTD at all**, and the reverse is equally true.

They touch in exactly three places:

- Filing an Inbox item **as reference** creates a wiki article
- Projects and areas can each hold **one article**
- Writing `[[Article name]]` in any article creates a link (the target need not exist yet)

## Keyboard {#keys}

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

How the words in the GTD books map onto enghi's screens.

| GTD | enghi |
|---|---|
| Inbox / In-basket | Inbox |
| Next Actions | Next Actions |
| Waiting For | Waiting For |
| Calendar / Tickler | Scheduled |
| Someday/Maybe | Someday / Maybe |
| Projects | Projects |
| Project Outcome | Outcome |
| Project Support Material | Project Support Material (a wiki article) |
| Contexts | Contexts |
| Areas of Responsibility | Areas |
| Reference Material | Filed as reference → a wiki article |
| Weekly Review | Weekly Review |
