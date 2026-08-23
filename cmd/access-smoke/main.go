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
	"time"

	"biebie-kube/internal/access"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: access-smoke <connection id or name>")
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
	fmt.Printf("status: state=%s connected=%v ip=%s detail=%q\n",
		status.State, status.Connected, status.AssignedIP, status.Detail)

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
