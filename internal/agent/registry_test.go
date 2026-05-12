package agent_test

import (
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/agent"
)

type stubAgent struct{ name string }

func (s stubAgent) Name() string                        { return s.name }
func (s stubAgent) Binary() string                      { return s.name }
func (s stubAgent) DefaultSessionName() string          { return s.name }
func (s stubAgent) ProcessName() string                 { return s.name }
func (s stubAgent) BuildCommand(agent.SpawnSpec) string { return "" }
func (s stubAgent) YOLOFlag() []string                  { return nil }

func TestRegister_ThenFor(t *testing.T) {
	agent.Reset()
	a := stubAgent{name: "stubA"}
	agent.Register(a)
	got, ok := agent.For("stubA")
	if !ok {
		t.Fatal("For(stubA) not found after Register")
	}
	if got.Name() != "stubA" {
		t.Fatalf("name = %q, want stubA", got.Name())
	}
}

func TestRegister_NilPanics(t *testing.T) {
	agent.Reset()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on nil Register")
		}
	}()
	agent.Register(nil)
}

func TestRegister_EmptyNamePanics(t *testing.T) {
	agent.Reset()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on empty Name()")
		}
	}()
	agent.Register(stubAgent{name: ""})
}

func TestRegister_DuplicatePanics(t *testing.T) {
	agent.Reset()
	agent.Register(stubAgent{name: "dup"})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate register")
		}
	}()
	agent.Register(stubAgent{name: "dup"})
}

func TestRegistered_ReturnsSortedNames(t *testing.T) {
	agent.Reset()
	agent.Register(stubAgent{name: "z"})
	agent.Register(stubAgent{name: "a"})
	got := agent.Registered()
	if len(got) != 2 || got[0] != "a" || got[1] != "z" {
		t.Fatalf("Registered = %v, want [a z]", got)
	}
}

func TestFor_UnknownReturnsFalse(t *testing.T) {
	agent.Reset()
	if _, ok := agent.For("nope"); ok {
		t.Fatal("expected For(nope)=false on empty registry")
	}
}

func TestMustFor_UnknownPanics(t *testing.T) {
	agent.Reset()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on MustFor miss")
		}
	}()
	_ = agent.MustFor("nope")
}

func TestMustFor_HitReturnsAgent(t *testing.T) {
	agent.Reset()
	agent.Register(stubAgent{name: "ok"})
	got := agent.MustFor("ok")
	if got.Name() != "ok" {
		t.Fatalf("MustFor = %q, want ok", got.Name())
	}
}
