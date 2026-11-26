package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/th2zz/perch/bridge/internal/fingerprint"
)

// cmdFingerprint records, or compares, what Perch is not allowed to change.
//
// Two uses:
//
//	perch-bridge fingerprint --out before.json     # before installing
//	perch-bridge fingerprint --baseline before.json # after, and it tells you
//
// It exits non-zero only for differences that break the promise. The plugin
// registration that installing causes is reported and tolerated, because that is
// what `herdr plugin link` is for and `herdr plugin unlink` takes it back out.
func cmdFingerprint(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("fingerprint", flag.ContinueOnError)
	out := fs.String("out", "", "write the fingerprint to this file")
	baseline := fs.String("baseline", "", "compare against a fingerprint taken earlier")
	if err := fs.Parse(args); err != nil {
		return err
	}

	c := mustCore(true)
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	current, err := fingerprint.Take(home, c.Store().Dir())
	if err != nil {
		return err
	}

	if *out != "" {
		if err := fingerprint.Write(*out, current); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote %s\n", *out)
	}

	if *baseline == "" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(current)
	}

	before, err := fingerprint.Read(*baseline)
	if err != nil {
		return err
	}
	changes := fingerprint.Compare(before, current)

	fmt.Printf("config.toml   : %s\n", describe(current.HerdrConfig))
	fmt.Printf("plugin entry  : %s\n", yesNo(current.OurPluginRegistered))
	fmt.Printf("state dir     : %s (%s)\n", current.StateDir, existsText(current.StateDirExists))
	fmt.Println()

	if len(changes) == 0 {
		fmt.Println("Nothing changed. Perch touched none of it.")
		return nil
	}

	for _, change := range changes {
		switch change.Kind {
		case fingerprint.ChangeUnexpected:
			fmt.Printf("  CHANGED   %s: %s\n", change.Field, change.Detail)
		case fingerprint.ChangeExpected:
			fmt.Printf("  expected  %s: %s\n", change.Field, change.Detail)
		default:
			fmt.Printf("  note      %s: %s\n", change.Field, change.Detail)
		}
	}

	if unexpected := fingerprint.Unexpected(changes); len(unexpected) > 0 {
		return fmt.Errorf(
			"%d unexpected change(s): Perch is supposed to leave your herdr setup untouched",
			len(unexpected),
		)
	}
	fmt.Println()
	fmt.Println("Only Perch's own plugin registration differs, which is what installing does")
	fmt.Println("and what `herdr plugin unlink` reverses.")
	return nil
}

func describe(stamp fingerprint.FileStamp) string {
	if !stamp.Exists {
		return "not present"
	}
	return fmt.Sprintf("%d bytes, %s", stamp.Size, shortHash(stamp.SHA256))
}

func shortHash(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}

func yesNo(value bool) string {
	if value {
		return "present"
	}
	return "absent"
}

func existsText(value bool) string {
	if value {
		return "exists"
	}
	return "not created yet"
}
