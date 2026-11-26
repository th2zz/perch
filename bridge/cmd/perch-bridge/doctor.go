package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// errDoctorFailed signals a failed check-up without printing a scary stack.
var errDoctorFailed = errors.New("check-up found a problem")

func cmdDoctor(ctx context.Context, args []string) error {
	c := mustCore(true)
	ctx, cancel := withTimeout(ctx, 30*time.Second)
	defer cancel()

	h := c.CheckHealth(ctx)
	fmt.Printf("Perch check-up — herdr %s\n\n", orDash(h.HerdrVersion))
	for i, item := range h.Items {
		fmt.Printf("%d. %-9s %s\n", i+1, symbol(item.Status), item.Title)
		fmt.Printf("   %s\n", item.Detail)
		if item.Fix != "" {
			fmt.Printf("   fix: %s\n", item.Fix)
		}
	}
	fmt.Println()
	if h.Overall == "fail" {
		return fmt.Errorf("%w (see the fix lines above)", errDoctorFailed)
	}
	fmt.Println("Everything Perch needs is working.")
	return nil
}

func symbol(s interface{ String() string }) string {
	switch s.String() {
	case "ok":
		return "[ ok ]"
	case "warn":
		return "[warn]"
	case "fail":
		return "[FAIL]"
	default:
		return "[ ?? ]"
	}
}

func orDash(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
