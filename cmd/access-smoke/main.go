// Command access-smoke asks the real Biebie Access endpoint everything Biebie
// Kube asks it, using this application's own client.
//
// It is a development tool for checking the integration end to end against a
// running Biebie Access. Nothing in the application imports it.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"biebie-kube/internal/access"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: access-smoke <connection id or name> [cluster host] [cluster port]")
	}
	profileID := os.Args[1]

	client, err := access.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	fmt.Printf("ping: answering=%v\n", client.Installed(ctx))

	profiles, err := client.Profiles(ctx)
	if err != nil {
		log.Fatalf("profiles: %v", err)
	}
	fmt.Printf("profiles: %d\n", len(profiles))
	for _, p := range profiles {
		fmt.Printf("  %s  %q group=%q provider=%q state=%s\n", p.ID, p.Name, p.Group, p.Provider, p.State)
	}

	status, err := client.Status(ctx, profileID)
	if err != nil {
		log.Fatalf("status: %v", err)
	}
	fmt.Printf("status: state=%s connected=%v ip=%s gateway=%s detail=%q\n",
		status.State, status.Connected, status.AssignedIP, status.Gateway, status.Detail)
	for _, f := range status.Forwards {
		fmt.Printf("  forward %s -> %s %s\n", f.Local(), f.Remote(), f.Name)
	}

	// The substitution Biebie Kube would make for a cluster on this connection.
	// Printing it here is the whole reason the field exists: it is what stands
	// between a kubeconfig naming an unreachable address and a working session.
	if len(os.Args) > 2 {
		host, port := os.Args[2], 6443
		if len(os.Args) > 3 {
			if parsed, err := strconv.Atoi(os.Args[3]); err == nil {
				port = parsed
			}
		}
		if forward, ok := status.LocalFor(host, port); ok {
			fmt.Printf("cluster %s:%d is reached at %s, verified as %s\n",
				host, port, forward.Local(), host)
		} else {
			fmt.Printf("cluster %s:%d is reached directly; no forward stands in for it\n", host, port)
		}
	}

	unknown, err := client.Status(ctx, "no-such-profile")
	if err != nil {
		log.Fatalf("status of an unknown profile should not error: %v", err)
	}
	fmt.Printf("unknown: state=%s detail=%q\n", unknown.State, unknown.Detail)

	resolved, err := client.Connect(ctx, profileID)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	fmt.Printf("connect: accepted, resolved to %s\n", resolved)
	if resolved != profileID {
		fmt.Printf("  (%q was a connection name; %s is the identifier to record)\n", profileID, resolved)
	}

	if _, err := client.Connect(ctx, "no-such-profile"); err == nil {
		log.Fatal("connecting to an unknown profile should have failed")
	} else {
		fmt.Printf("connect unknown: refused (%v)\n", err)
	}
}
