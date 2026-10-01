package domain

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/renanporto/payment-engine/internal/kernel"
)

func TestNewDocument(t *testing.T) {
	d, err := NewDocument("cnpj", "11.222.333/0001-81")
	if err != nil || d.Number != "11222333000181" {
		t.Fatalf("got %+v, %v", d, err)
	}
	for _, c := range [][2]string{{"cpf", "123"}, {"rg", "12345678901"}, {"cnpj", "12345678901"}} {
		if _, err := NewDocument(c[0], c[1]); !errors.Is(err, kernel.ErrInvalid) {
			t.Errorf("%v: want ErrInvalid, got %v", c, err)
		}
	}
}

func TestWorkspaceOnboarding(t *testing.T) {
	doc, _ := NewDocument("cpf", "12345678901")
	w, err := NewWorkspace(uuid.New(), "Shop", "s@x.com", doc, "5411")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Activate(); !errors.Is(err, kernel.ErrInvalidTransition) {
		t.Fatalf("activate before accredit: want ErrInvalidTransition, got %v", err)
	}
	if len(w.Pull()) != 0 {
		t.Fatal("a rejected transition must not record events")
	}
	if err := w.Accredit(); err != nil {
		t.Fatal(err)
	}
	if err := w.Activate(); err != nil || w.Status != WorkspaceActive {
		t.Fatalf("activate: %s %v", w.Status, err)
	}
	if evs := w.Pull(); len(evs) != 1 || evs[0].EventName() != "workspace_activation_completed" {
		t.Fatalf("events = %v", evs)
	}
	if err := w.Reject(); !errors.Is(err, kernel.ErrInvalidTransition) {
		t.Fatalf("reject an active workspace: want ErrInvalidTransition, got %v", err)
	}
	if _, err := NewWorkspace(uuid.New(), "Shop", "s@x.com", doc, "54a1"); !errors.Is(err, kernel.ErrInvalid) {
		t.Fatalf("bad mcc: want ErrInvalid, got %v", err)
	}
}
