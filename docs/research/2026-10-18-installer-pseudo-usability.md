# Installer — pseudo-user pass (2026-10-18)

Not a usability test with people. A scripted, deliberately careless session against the real stack
to find states a curious or impatient person would reach: double clicks, a reload mid-run, scope
switching, clicking disabled rows, the optional step, a mobile viewport.

## What was tried

| Action | Result |
|---|---|
| Double-click "Install everything" | Second request refused; the first run continued |
| Primary button clicked four times during a run | Ignored while the button showed `Running…` |
| Scope → entry screen during a run | Entry screen reached; "Install everything" disabled |
| Reload during a run | Returned to the wizard, not the entry screen; status still `Running: …` |
| Click a completed rail row after completion | Opened that step; `Back to summary` returned |
| Click a locked rail row | No effect, as intended |
| Copy a command | Clipboard denied in automation; the button said `Copy failed — select the text` |
| Optional step opened after completion | Shown as waiting, never as failure |
| 390×844 mid-flow | No horizontal overflow; rail collapsed behind `Steps` |

## Defects found and fixed in the same pass

1. **Second start was noisy.** Double-clicking produced `Cannot start: that step is already running`
   in the status line. The entry button is now disabled while a run is in flight and a duplicate
   start reads `A run is already in progress`. `Step through` stays enabled so a person can watch.
2. **"Next step 0."** The optional step (index −1 among required steps) rendered as
   `0. Your session`. Optional steps now appear as `Optional next`, without a number.
3. **Reload lost the wizard.** A reload during a run returned to the entry screen while the run
   carried on. The wizard is remembered in `localStorage` and restored; `Scope` clears it.
4. **Scope during a run was a dead end.** Both entry buttons were disabled, leaving no way back to the
   progress. Only "Install everything" is disabled now.
5. **Batch start kept the old step in view.** `Install everything` now jumps to the first step of the
   batch immediately instead of showing the previous one until the first event.
6. **Optional step read as a failure.** `Your session` carries state `fail` when no session has
   arrived; it rendered a red block and `Blocked`. Optional steps now show `Waiting`, no error block,
   and a `When it appears` line.
7. **Server grouping reordered the steps.** `Your session` was grouped under `Data`, so it appeared
   before `Claude Code settings` in the rail regardless of source order. Moved to `Engineers`.
8. **A late `done` event reset the view.** After a batch, the trailing event set the current step to
   `null`, so clicking a rail row fell back to the summary. The event now only moves the view when a
   batch is running; `autoAll` is cleared when the batch ends.
9. **A resumed wizard could render an empty pane.** Reloading with the wizard remembered restored the
   screen with no current step, so the pane was an empty bordered box. `render()` now falls back to the
   next unfinished step instead of leaving the work area blank.
10. **Mobile entry rows** stacked the time and the button under the wrong column and hid the header
   row awkwardly; the 390 px layout was rewritten (number, name, time, action in one column, no
   header).

## Still open

- Five real people, one of them on Windows. This pass proves the interface survives careless
  clicking; it says nothing about whether the words make sense to a first-time user.
- Clipboard success was never observed because the automation context denies it; on a normal browser
  it uses the standard async clipboard API.
