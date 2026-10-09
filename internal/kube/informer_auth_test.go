package kube

import (
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestUnauthorizedWatchErrorInvokesAuthFailureOnce(t *testing.T) {
	var calls int
	hub := NewWatchHub(nil, nil, func(_ schema.GroupVersionResource, _ error) {
		calls++
	})
	key := watchKey{gvr: schema.GroupVersionResource{Resource: "pods"}}
	gvr := key.gvr

	hub.handleWatchError(key, gvr, apierrors.NewUnauthorized("expired"))
	hub.handleWatchError(key, gvr, apierrors.NewUnauthorized("expired"))

	if calls != 1 {
		t.Fatalf("auth failure callback calls = %d, want 1", calls)
	}
}

func TestForbiddenWatchErrorRequiresTwoAttempts(t *testing.T) {
	var calls int
	hub := NewWatchHub(nil, nil, func(_ schema.GroupVersionResource, _ error) {
		calls++
	})
	key := watchKey{gvr: schema.GroupVersionResource{Resource: "pods"}}
	gvr := key.gvr
	err := apierrors.NewForbidden(gvr.GroupResource(), "x", nil)

	hub.handleWatchError(key, gvr, err)
	if calls != 0 {
		t.Fatalf("first forbidden should not call back, got %d", calls)
	}
	hub.handleWatchError(key, gvr, err)
	if calls != 1 {
		t.Fatalf("second forbidden should call back once, got %d", calls)
	}
}
