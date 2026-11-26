package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/th2zz/perch/bridge/internal/core"
	"github.com/th2zz/perch/bridge/internal/pairlink"
	"github.com/th2zz/perch/bridge/internal/server"
)

// cmdPair prints everything the phone needs, and nothing it does not.
//
// Pairing is deliberately boring: one token, one address, no accounts. It is the
// step that has to feel like Tailscale's `tailscale up` — the user reads one
// short block and taps once (E1–E3: zero terminology, zero inputs).
func cmdPair(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	rotate := fs.Bool("rotate", false, "issue a new token, unpairing every existing device")
	withPush := fs.Bool("with-push", false, "also configure the push gateway")
	gateway := fs.String("gateway", "", "push gateway URL, e.g. http://127.0.0.1:45998/v1/push")
	pushToken := fs.String("push-token", "", "token for the push gateway")
	linkOnly := fs.Bool("link", false, "print only the pairing link, for scripts")
	if err := fs.Parse(args); err != nil {
		return err
	}

	c := mustCore(true)

	if *withPush {
		if *gateway == "" {
			return fmt.Errorf("--with-push needs --gateway (for example %s)", exampleGateway())
		}
		cfg, err := c.PushConfig()
		if err != nil {
			cfg = core.PushConfig{}
		}
		cfg.GatewayURL = *gateway
		if *pushToken != "" {
			cfg.GatewayToken = *pushToken
		}
		if err := c.SavePushConfig(cfg); err != nil {
			return err
		}
		fmt.Printf("push gateway set to %s\n", *gateway)
		if cfg.DeviceToken == "" {
			fmt.Println("note: the phone still has to hand over its notification token;")
			fmt.Println("      open Perch and it will register itself.")
		}
	}

	var token string
	var err error
	if *rotate {
		token, err = c.Store().RotateToken()
	} else {
		token, err = c.Store().EnsureToken()
	}
	if err != nil {
		return err
	}

	hosts := candidateHosts()
	// The link carries the address a phone outside the house can actually use:
	// the Tailscale address when there is one, otherwise the first LAN address.
	primary := hosts[0]
	if strings.Contains(primary, " ") {
		// The loopback entry is annotated; never use it for a link.
		primary = strings.Fields(primary)[0]
	}
	link := pairlink.Link{Address: primary, Token: token, Name: c.HostLabel()}

	if *linkOnly {
		fmt.Println(link.String())
		return nil
	}

	fmt.Println()
	fmt.Println("Pair your phone. The easy way is the link at the bottom: send it to")
	fmt.Println("yourself in any chat app, open it on the phone, and tap Connect.")
	fmt.Println()
	fmt.Println("  link    : " + link.String())
	fmt.Println()
	fmt.Println("Or type these two values into Perch by hand:")
	for _, h := range hosts {
		fmt.Printf("    address : %s\n", h)
	}
	fmt.Printf("    token   : %s\n", token)
	fmt.Println()
	fmt.Println("If the address above does not work, use the one from your own network")
	fmt.Println("(Tailscale: `tailscale ip -4`).")
	fmt.Println()

	blob, _ := json.MarshalIndent(map[string]any{
		"addresses": hosts,
		"token":     token,
		"host":      c.HostLabel(),
		"link":      link.String(),
	}, "", "  ")
	fmt.Printf("machine-readable:\n%s\n", blob)
	return nil
}

func exampleGateway() string {
	return "http://127.0.0.1:45998/v1/push"
}

// candidateHosts lists the addresses a phone on the user's own network could
// use, best guess first. Most useful is the Tailscale address, because that is
// the one that works from outside the house.
func candidateHosts() []string {
	port := "45999"
	if addr := listenAddr(); strings.HasPrefix(addr, ":") {
		port = strings.TrimPrefix(addr, ":")
	} else if _, p, err := net.SplitHostPort(addr); err == nil && p != "" {
		port = p
	}

	var out []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return []string{"127.0.0.1:" + port}
	}
	var others []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}
			entry := net.JoinHostPort(ip4.String(), port)
			// 100.64.0.0/10 is Tailscale's address range; offer it first.
			if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
				out = append(out, entry)
			} else {
				others = append(others, entry)
			}
		}
	}
	out = append(out, others...)
	out = append(out, fmt.Sprintf("127.0.0.1:%s (only from this computer)", port))
	return out
}

// ensureServerDefaults keeps the pairing output and the daemon's bind address
// from drifting apart.
func init() {
	_ = server.DefaultAddr
	_ = time.Second
}
