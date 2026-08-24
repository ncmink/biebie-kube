// Command mock-access stands in for Biebie Access during development.
//
// The two applications ship separately, so Biebie Kube needs a way to exercise
// the whole handoff path — status, ticket, deep link, retry — without a VPN
// client, a customer network, or the other application installed. This binary
// speaks the real Biebie Context Protocol on the real endpoint, so what it
// proves here is what will happen in production.
//
// It is a development tool. It performs no VPN work of any kind: it reports a
// state the operator chooses on the command line.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	bctx "github.com/ncmink/biebie-protocol/context"
	"github.com/ncmink/biebie-protocol/deeplink"
	"github.com/ncmink/biebie-protocol/handoff"
	"github.com/ncmink/biebie-protocol/ipc"
)

func main() {
	var (
		profileID    = flag.String("profile", "smoi-vpn", "access profile identifier to report on")
		connected    = flag.Bool("connected", true, "report the profile as connected")
		assignedIP   = flag.String("ip", "10.168.6.160", "tunnel address to report")
		customerID   = flag.String("customer", "smoi", "customer identifier")
		customerName = flag.String("customer-name", "SMOI", "customer display name")
		clusterID    = flag.String("cluster", "rke2-prod", "cluster identifier to hand off")
		clusterName  = flag.String("cluster-name", "RKE2 Production", "cluster display name")
		server       = flag.String("server", "https://172.16.20.65:6443", "Kubernetes API endpoint")
		openKube     = flag.Bool("open", false, "create a handoff and open Biebie Kube with it")
	)
	flag.Parse()

	endpoint, err := ipc.AccessEndpoint()
	if err != nil {
		log.Fatalf("resolve endpoint: %v", err)
	}

	tickets := handoff.NewStore()

	srv := ipc.NewServer(endpoint)
	srv.OnError = func(err error) { log.Printf("serve: %v", err) }

	srv.Handle(ipc.MethodAccessStatus, func(_ context.Context, params json.RawMessage) (any, error) {
		var req struct {
			ProfileID string `json:"profileId"`
		}
		_ = json.Unmarshal(params, &req)

		state := bctx.AccessDisconnected
		detail := "This customer is not connected in Biebie Access."
		ip := ""
		var since *time.Time
		if *connected {
			state = bctx.AccessConnected
			detail = ""
			ip = *assignedIP
			now := time.Now()
			since = &now
		}
		log.Printf("status %s -> %s", req.ProfileID, state)
		return bctx.AccessStatus{
			ProfileID:   req.ProfileID,
			State:       state,
			Connected:   *connected,
			AssignedIP:  ip,
			ConnectedAt: since,
			Detail:      detail,
		}, nil
	})

	srv.Handle(ipc.MethodAccessConnect, func(_ context.Context, params json.RawMessage) (any, error) {
		var req struct {
			ProfileID string `json:"profileId"`
		}
		_ = json.Unmarshal(params, &req)
		log.Printf("connect requested for %s (a real Biebie Access would raise its window here)", req.ProfileID)
		return map[string]bool{"accepted": true}, nil
	})

	srv.Handle(ipc.MethodConsumeHandoff, func(_ context.Context, params json.RawMessage) (any, error) {
		var req struct {
			HandoffID string `json:"handoffId"`
			TargetApp string `json:"targetApp"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, errors.New("malformed request")
		}
		received, err := tickets.ConsumeFor(req.HandoffID, bctx.App(req.TargetApp))
		if err != nil {
			log.Printf("handoff %s refused: %v", req.HandoffID, err)
			return nil, err
		}
		log.Printf("handoff %s consumed by %s", req.HandoffID, req.TargetApp)
		return received, nil
	})

	if err := srv.Start(); err != nil {
		log.Fatalf("start: %v", err)
	}
	defer func() { _ = srv.Close() }()

	log.Printf("mock Biebie Access listening on %s", endpoint.Address)
	log.Printf("profile %s reported as connected=%v", *profileID, *connected)

	if *openKube {
		id, err := tickets.CreateHandoff(context.Background(), handoff.ContextHandoff{
			SourceApp: bctx.AppAccess,
			TargetApp: bctx.AppKube,
			Context: bctx.BiebieContext{
				ContextID:       "ctx_" + time.Now().UTC().Format("20060102150405"),
				CustomerID:      *customerID,
				CustomerName:    *customerName,
				EnvironmentID:   "prod",
				EnvironmentName: "Production",
				EnvironmentKind: bctx.EnvironmentProduction,
				AccessProfileID: *profileID,
				ClusterID:       *clusterID,
				ClusterName:     *clusterName,
				Server:          *server,
			},
		})
		if err != nil {
			log.Fatalf("create handoff: %v", err)
		}
		link, err := deeplink.OpenKube(id)
		if err != nil {
			log.Fatalf("build link: %v", err)
		}
		log.Printf("opening %s", link)
		if err := open(link); err != nil {
			log.Printf("could not open Biebie Kube automatically: %v", err)
			fmt.Println(link)
		}
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Println("stopping")
}

func open(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("/usr/bin/open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Run()
}
