// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package controller

import (
	"errors"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/bitwise-media-group/dex-client-controller/api/v1alpha1"
)

func dexClient(ns, name string, created time.Time, deleting bool) *v1alpha1.DexClient {
	o := &v1alpha1.DexClient{ObjectMeta: metav1.ObjectMeta{
		Namespace: ns, Name: name, CreationTimestamp: metav1.NewTime(created),
	}}
	if deleting {
		now := metav1.Now()
		o.DeletionTimestamp = &now
	}
	return o
}

func TestClaimant(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		objs []*v1alpha1.DexClient
		want string // "ns/name", "" for none
	}{
		{"oldest wins", []*v1alpha1.DexClient{
			dexClient("b", "x", t0.Add(time.Second), false), dexClient("a", "y", t0, false),
		}, "a/y"},
		{"tie broken by namespace", []*v1alpha1.DexClient{
			dexClient("b", "x", t0, false), dexClient("a", "z", t0, false),
		}, "a/z"},
		{"tie broken by name", []*v1alpha1.DexClient{
			dexClient("a", "z", t0, false), dexClient("a", "x", t0, false),
		}, "a/x"},
		{"deleting objects give up their claim", []*v1alpha1.DexClient{
			dexClient("a", "old", t0, true), dexClient("a", "new", t0.Add(time.Hour), false),
		}, "a/new"},
		{"nobody left", []*v1alpha1.DexClient{dexClient("a", "old", t0, true)}, ""},
		{"empty", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Claimant(tt.objs)
			name := ""
			if ok {
				name = got.Namespace + "/" + got.Name
			}
			if name != tt.want {
				t.Errorf("Claimant = %q, want %q", name, tt.want)
			}
		})
	}
}

func TestSetReady(t *testing.T) {
	var conds []metav1.Condition
	if !SetReady(&conds, 3, nil, "all good") {
		t.Fatal("SetReady(nil) = false")
	}
	c := meta.FindStatusCondition(conds, v1alpha1.ConditionReady)
	if c.Status != metav1.ConditionTrue || c.Reason != v1alpha1.ReasonSynced || c.ObservedGeneration != 3 {
		t.Errorf("Ready = %+v, want True/Synced at generation 3", c)
	}

	if !SetReady(&conds, 4, Conditionf(v1alpha1.ReasonConflict, "taken by %s", "x"), "") {
		t.Fatal("SetReady(ConditionError) = false")
	}
	c = meta.FindStatusCondition(conds, v1alpha1.ConditionReady)
	if c.Status != metav1.ConditionFalse || c.Reason != v1alpha1.ReasonConflict || c.Message != "taken by x" {
		t.Errorf("Ready = %+v, want False/Conflict", c)
	}

	if SetReady(&conds, 5, errors.New("apiserver hiccup"), "") {
		t.Error("SetReady(plain error) = true, want the conditions left alone")
	}
	if c := meta.FindStatusCondition(conds, v1alpha1.ConditionReady); c.ObservedGeneration != 4 {
		t.Errorf("a plain error rewrote the condition: %+v", c)
	}
}
