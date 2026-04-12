package cmd

import (
	"testing"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/session"
)

func TestSortByMostRecent(t *testing.T) {
	now := time.Date(2026, 4, 12, 12, 0, 0, 0, time.UTC)

	t.Run("orders by LastAttachedAt desc", func(t *testing.T) {
		sessions := []*session.Session{
			{Name: "old", LastAttachedAt: now.Add(-2 * time.Hour)},
			{Name: "newest", LastAttachedAt: now.Add(-1 * time.Minute)},
			{Name: "middle", LastAttachedAt: now.Add(-30 * time.Minute)},
		}
		sortByMostRecent(sessions)
		want := []string{"newest", "middle", "old"}
		for i, s := range sessions {
			if s.Name != want[i] {
				t.Errorf("[%d]=%q, want %q", i, s.Name, want[i])
			}
		}
	})

	t.Run("never-attached fallback is deterministic by name", func(t *testing.T) {
		sessions := []*session.Session{
			{Name: "zebra"},
			{Name: "apple"},
			{Name: "mango"},
		}
		sortByMostRecent(sessions)
		want := []string{"apple", "mango", "zebra"}
		for i, s := range sessions {
			if s.Name != want[i] {
				t.Errorf("[%d]=%q, want %q", i, s.Name, want[i])
			}
		}
	})

	t.Run("attached sessions sort before never-attached", func(t *testing.T) {
		sessions := []*session.Session{
			{Name: "never"},
			{Name: "attached", LastAttachedAt: now.Add(-1 * time.Hour)},
		}
		sortByMostRecent(sessions)
		if sessions[0].Name != "attached" {
			t.Errorf("expected 'attached' first, got %q", sessions[0].Name)
		}
	})

	t.Run("equal timestamps tie-break by name", func(t *testing.T) {
		ts := now.Add(-1 * time.Hour)
		sessions := []*session.Session{
			{Name: "zebra", LastAttachedAt: ts},
			{Name: "apple", LastAttachedAt: ts},
		}
		sortByMostRecent(sessions)
		if sessions[0].Name != "apple" {
			t.Errorf("tie-break failed: got %q, want %q", sessions[0].Name, "apple")
		}
	})
}
