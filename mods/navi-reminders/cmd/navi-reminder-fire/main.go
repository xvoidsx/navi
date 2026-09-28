// navi-reminder-fire — the systemd oneshot behind every navi reminder.
//
// Invoked as `navi-reminder-fire <id>` from the reminder's .service unit.
// It loads the registry entry, waits up to 60s for a notification daemon
// (the boot-before-login window), notifies via dunst with Done/Snooze
// actions, and — if nobody can hear us — marks the reminder MISSED in the
// registry instead of dropping it silently. The TUI surfaces missed ones.
//
// Exit codes: 0 always on the handled paths (a missed reminder is not a
// crash); 1 on usage errors or registry failures.
package main

import (
	"fmt"
	"os"

	"github.com/rav3ndust/navi-reminders"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: navi-reminder-fire <reminder-id>")
}

func main() {
	if len(os.Args) != 2 || os.Args[1] == "" || os.Args[1] == "--help" || os.Args[1] == "-h" {
		usage()
		os.Exit(1)
	}
	if err := reminders.FireReminder(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "navi-reminder-fire:", err)
		os.Exit(1)
	}
}
