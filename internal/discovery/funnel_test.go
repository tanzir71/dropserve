package discovery

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestFunnelStatePersistsAndAutoExpiresAfterEightHours(t *testing.T) {
	now := time.Date(2026, time.August, 28, 1, 0, 0, 0, time.UTC)
	statePath := filepath.Join(t.TempDir(), "funnel.json")
	var actions []FunnelAction
	options := FunnelOptions{
		StatePath: statePath,
		Clock:     func() time.Time { return now },
		Execute: func(_ context.Context, action FunnelAction) error {
			actions = append(actions, action)
			return nil
		},
	}
	manager, err := NewFunnelManager(options)
	if err != nil {
		t.Fatalf("create Funnel manager: %v", err)
	}
	if err := manager.Enable(context.Background(), "field-notes", "field-notes"); err != nil {
		t.Fatalf("enable Funnel: %v", err)
	}
	entry, active := manager.Active("field-notes")
	if !active || !entry.EnabledAt.Equal(now) || !entry.ExpiresAt.Equal(now.Add(8*time.Hour)) {
		t.Fatalf("active Funnel entry = %#v, %v", entry, active)
	}

	reloaded, err := NewFunnelManager(options)
	if err != nil {
		t.Fatalf("reload Funnel manager: %v", err)
	}
	if _, active := reloaded.Active("field-notes"); !active {
		t.Fatal("persisted Funnel entry was not active after reload")
	}
	now = now.Add(8*time.Hour + time.Second)
	if err := reloaded.Expire(context.Background()); err != nil {
		t.Fatalf("expire Funnel: %v", err)
	}
	if _, active := reloaded.Active("field-notes"); active {
		t.Fatal("Funnel entry remained active after its eight-hour expiry")
	}
	if len(actions) != 2 || !actions[0].Enable || actions[1].Enable {
		t.Fatalf("Funnel actions = %#v, want enable then disable", actions)
	}
}

func TestFunnelCanBeDisabledAndStaysDisabledAfterReload(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "funnel.json")
	var actions []FunnelAction
	options := FunnelOptions{
		StatePath: statePath,
		Execute: func(_ context.Context, action FunnelAction) error {
			actions = append(actions, action)
			return nil
		},
	}
	manager, err := NewFunnelManager(options)
	if err != nil {
		t.Fatalf("create Funnel manager: %v", err)
	}
	if err := manager.Enable(context.Background(), "field-notes", "field-notes"); err != nil {
		t.Fatalf("enable Funnel: %v", err)
	}
	if err := manager.Disable(context.Background(), "field-notes"); err != nil {
		t.Fatalf("disable Funnel: %v", err)
	}
	if _, active := manager.Active("field-notes"); active {
		t.Fatal("disabled Funnel remained active")
	}
	reloaded, err := NewFunnelManager(options)
	if err != nil {
		t.Fatalf("reload Funnel manager: %v", err)
	}
	if _, active := reloaded.Active("field-notes"); active {
		t.Fatal("disabled Funnel returned after reload")
	}
	if len(actions) != 2 || !actions[0].Enable || actions[1].Enable {
		t.Fatalf("Funnel actions = %#v, want enable then disable", actions)
	}
}

func TestFunnelPublishesPublicSharingStateTransitions(t *testing.T) {
	var states []bool
	manager, err := NewFunnelManager(FunnelOptions{
		Execute:  func(context.Context, FunnelAction) error { return nil },
		OnChange: func(active bool) { states = append(states, active) },
	})
	if err != nil {
		t.Fatalf("create Funnel manager: %v", err)
	}
	if err := manager.Enable(context.Background(), "field-notes", "field-notes"); err != nil {
		t.Fatalf("enable Funnel: %v", err)
	}
	if err := manager.Disable(context.Background(), "field-notes"); err != nil {
		t.Fatalf("disable Funnel: %v", err)
	}
	want := []bool{false, true, false}
	if len(states) != len(want) {
		t.Fatalf("public sharing states = %v, want %v", states, want)
	}
	for index := range want {
		if states[index] != want[index] {
			t.Fatalf("public sharing states = %v, want %v", states, want)
		}
	}
}
