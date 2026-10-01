package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/renanporto/payment-engine/internal/kernel"
)

type WorkspaceStatus string

const (
	WorkspacePending     WorkspaceStatus = "pending"
	WorkspaceAccrediting WorkspaceStatus = "accrediting"
	WorkspaceActive      WorkspaceStatus = "active"
	WorkspaceRejected    WorkspaceStatus = "rejected"
	WorkspaceSuspended   WorkspaceStatus = "suspended"
	WorkspaceCanceled    WorkspaceStatus = "canceled"
)

var workspaceFSM = kernel.FSM[WorkspaceStatus]{
	"accredit":   {WorkspacePending: WorkspaceAccrediting},
	"activate":   {WorkspaceAccrediting: WorkspaceActive},
	"reject":     {WorkspaceAccrediting: WorkspaceRejected},
	"suspend":    {WorkspaceActive: WorkspaceSuspended},
	"cancel":     {WorkspaceActive: WorkspaceCanceled},
	"reactivate": {WorkspaceCanceled: WorkspaceActive},
}

type Workspace struct {
	kernel.Events
	ID        uuid.UUID
	AccountID uuid.UUID
	Name      string
	Email     string
	Document  Document
	MCC       string
	Status    WorkspaceStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

func NewWorkspace(accountID uuid.UUID, name, email string, doc Document, mcc string) (Workspace, error) {
	if err := validateContact(name, email); err != nil {
		return Workspace{}, err
	}
	if len(mcc) != 4 || strings.Trim(mcc, "0123456789") != "" {
		return Workspace{}, kernel.Invalid("mcc must have 4 digits")
	}
	return Workspace{AccountID: accountID, Name: name, Email: email, Document: doc, MCC: mcc, Status: WorkspacePending}, nil
}

func (w *Workspace) Accredit() error { return w.apply("accredit") }

func (w *Workspace) Activate() error {
	if err := w.apply("activate"); err != nil {
		return err
	}
	w.Record(WorkspaceActivationCompleted{WorkspaceID: w.ID})
	return nil
}

func (w *Workspace) Reject() error {
	if err := w.apply("reject"); err != nil {
		return err
	}
	w.Record(WorkspaceRejectionCompleted{WorkspaceID: w.ID})
	return nil
}

func (w *Workspace) apply(event string) (err error) {
	w.Status, err = workspaceFSM.Next(w.Status, event)
	return err
}

// WorkspaceActivationCompleted: the PSP approved the seller.
type WorkspaceActivationCompleted struct {
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

func (WorkspaceActivationCompleted) EventName() string { return "workspace_activation_completed" }

// WorkspaceRejectionCompleted: the PSP refused the seller.
type WorkspaceRejectionCompleted struct {
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

func (WorkspaceRejectionCompleted) EventName() string { return "workspace_rejection_completed" }
