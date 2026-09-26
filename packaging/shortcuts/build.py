#!/usr/bin/env python3
"""Build and sign the enghi-events shortcut (shortcuts/enghi-events.shortcut).

The shortcut finds the calendar events around today and prints one JSON object
per line. The enghi server runs it with `shortcuts run` (internal/calendar).

    python3 packaging/shortcuts/build.py            # build and sign
    python3 packaging/shortcuts/build.py --unsigned # plist only, for inspection

Signing contacts Apple and needs a Mac signed into iCloud. The signature embeds
an anonymous Apple certificate that expires about a year later, so **re-sign at
every release** (see CLAUDE.md).

**The window below must match calendar.WindowPastDays / WindowFutureDays** in
internal/calendar/calendar.go: the server replaces exactly that window.
"""
import os
import plistlib
import subprocess
import sys
import tempfile
import uuid

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
OUT = os.path.join(ROOT, "shortcuts", "enghi-events.shortcut")

WINDOW_PAST_DAYS = 1
WINDOW_FUTURE_DAYS = 14

# Content filter operators for dates, as Shortcuts serializes them. By
# calendar day, "in the last N days" means today-(N-1) through today, and "in
# the next N days" tomorrow through today+N.
OP_IN_THE_LAST = 1001
OP_IN_THE_NEXT = 1000
UNIT_DAYS = 16  # NSCalendarUnitDay

EVENTS = "events"  # the variable the Finds append to
OBJ = "￼"  # the placeholder a variable occupies in a text token


def uid():
    return str(uuid.uuid4()).upper()


def repeat_item(prop=None, iso=False):
    """The loop's Repeat Item, optionally one property of it as ISO 8601."""
    v = {"Type": "Variable", "VariableName": "Repeat Item"}
    aggs = []
    if prop:
        aggs.append({"Type": "WFPropertyVariableAggrandizement", "PropertyName": prop})
    if iso:
        aggs.append({"Type": "WFDateFormatVariableAggrandizement",
                     "WFDateFormatStyle": "ISO 8601", "WFISO8601IncludeTime": True})
    if aggs:
        v["Aggrandizements"] = aggs
    return v


def token(attachment):
    """A text field holding just one variable."""
    return {"Value": {"string": OBJ, "attachmentsByRange": {"{0, 1}": attachment}},
            "WFSerializationType": "WFTextTokenString"}


def text(s):
    return {"Value": {"string": s}, "WFSerializationType": "WFTextTokenString"}


def output_of(action_uuid, name):
    return {"Type": "ActionOutput", "OutputUUID": action_uuid, "OutputName": name}


def date_filter(op, number=None):
    return {"Bounded": True, "Operator": op, "Property": "Start Date", "Removable": False,
            "Values": {"Number": str(number or 1), "Unit": UNIT_DAYS}}


def find_events(action_uuid, template):
    """Find Calendar Events matching one condition, oldest first."""
    return {
        "WFWorkflowActionIdentifier": "is.workflow.actions.filter.calendarevents",
        "WFWorkflowActionParameters": {
            "UUID": action_uuid,
            "WFContentItemFilter": {
                "Value": {
                    "WFActionParameterFilterPrefix": 1,
                    "WFContentPredicateBoundedDate": False,
                    "WFActionParameterFilterTemplates": [template],
                },
                "WFSerializationType": "WFContentPredicateTableTemplate",
            },
            "WFContentItemLimitEnabled": False,
            "WFContentItemSortProperty": "Start Date",
            "WFContentItemSortOrder": "Oldest First",
        },
    }


def workflow():
    dic, txt, loop_end, combine, out = (uid() for _ in range(5))
    group = uid()
    fields = [
        ("title", repeat_item()),  # an event as text is its title
        ("start", repeat_item("Start Date", iso=True)),
        ("end", repeat_item("End Date", iso=True)),
        ("calendar", repeat_item("Calendar")),
        ("location", repeat_item("Location")),
    ]
    # One Find Calendar Events per half of the window, each appended to one
    # variable. **A single Find with "Any" of these conditions returns
    # nothing** (macOS 26.6), though each condition works on its own.
    actions = []
    for op, n in ((OP_IN_THE_LAST, WINDOW_PAST_DAYS + 1),
                  (OP_IN_THE_NEXT, WINDOW_FUTURE_DAYS)):
        u = uid()
        actions += [find_events(u, date_filter(op, n)),
                    {"WFWorkflowActionIdentifier": "is.workflow.actions.appendvariable",
                     "WFWorkflowActionParameters": {
                         "WFVariableName": EVENTS,
                         "WFInput": {"Value": output_of(u, "Calendar Events"),
                                     "WFSerializationType": "WFTextTokenAttachment"}}}]
    actions += [
        {
            "WFWorkflowActionIdentifier": "is.workflow.actions.repeat.each",
            "WFWorkflowActionParameters": {
                "GroupingIdentifier": group,
                "WFControlFlowMode": 0,
                "WFInput": {"Value": {"Type": "Variable", "VariableName": EVENTS},
                            "WFSerializationType": "WFTextTokenAttachment"},
            },
        },
        {   # Dictionary of text fields
            "WFWorkflowActionIdentifier": "is.workflow.actions.dictionary",
            "WFWorkflowActionParameters": {
                "UUID": dic,
                "WFItems": {
                    "Value": {"WFDictionaryFieldValueItems": [
                        {"WFItemType": 0, "WFKey": text(k), "WFValue": token(v)}
                        for k, v in fields
                    ]},
                    "WFSerializationType": "WFDictionaryFieldValue",
                },
            },
        },
        {   # Text: the dictionary, which as text is its JSON
            "WFWorkflowActionIdentifier": "is.workflow.actions.gettext",
            "WFWorkflowActionParameters": {
                "UUID": txt,
                "WFTextActionText": token(output_of(dic, "Dictionary")),
            },
        },
        {
            "WFWorkflowActionIdentifier": "is.workflow.actions.repeat.each",
            "WFWorkflowActionParameters": {
                "GroupingIdentifier": group,
                "UUID": loop_end,
                "WFControlFlowMode": 2,
            },
        },
        {   # Combine Text with new lines (the default separator)
            "WFWorkflowActionIdentifier": "is.workflow.actions.text.combine",
            "WFWorkflowActionParameters": {
                "UUID": combine,
                "text": {"Value": output_of(loop_end, "Repeat Results"),
                         "WFSerializationType": "WFTextTokenAttachment"},
            },
        },
        {   # Stop and Output
            "WFWorkflowActionIdentifier": "is.workflow.actions.output",
            "WFWorkflowActionParameters": {
                "UUID": out,
                "WFOutput": token(output_of(combine, "Combined Text")),
            },
        },
    ]
    return {
        "WFQuickActionSurfaces": [],
        "WFWorkflowActions": actions,
        "WFWorkflowClientVersion": "4711",
        "WFWorkflowHasOutputFallback": False,
        "WFWorkflowHasShortcutInputVariables": False,
        "WFWorkflowIcon": {"WFWorkflowIconGlyphNumber": 61440,
                           "WFWorkflowIconStartColor": -12365313},
        "WFWorkflowImportQuestions": [],
        "WFWorkflowInputContentItemClasses": [],
        "WFWorkflowMinimumClientVersion": 900,
        "WFWorkflowMinimumClientVersionString": "900",
        "WFWorkflowOutputContentItemClasses": ["WFStringContentItem"],
        "WFWorkflowTypes": [],
    }


def main():
    data = plistlib.dumps(workflow(), fmt=plistlib.FMT_BINARY)
    if "--unsigned" in sys.argv:
        sys.stdout.buffer.write(data)
        return
    with tempfile.TemporaryDirectory() as d:
        src = os.path.join(d, "enghi-events.shortcut")
        with open(src, "wb") as f:
            f.write(data)
        os.makedirs(os.path.dirname(OUT), exist_ok=True)
        subprocess.run(["shortcuts", "sign", "--mode", "anyone", "-i", src, "-o", OUT], check=True)
    print(OUT)


if __name__ == "__main__":
    main()
