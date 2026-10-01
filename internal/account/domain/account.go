// Package domain holds the account context's entities: Account (the tenant)
// and Workspace (the seller, a connected account at the PSP). No I/O.
package domain

import (
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/renanporto/payment-engine/internal/kernel"
)

// Document is a Brazilian tax id.
type Document struct {
	Type   string // cpf | cnpj
	Number string // digits only
}

func NewDocument(typ, number string) (Document, error) {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, number)
	want := map[string]int{"cpf": 11, "cnpj": 14}[typ]
	if want == 0 {
		return Document{}, kernel.Invalid("document_type must be cpf or cnpj")
	}
	if len(digits) != want {
		return Document{}, kernel.Invalid("document_number must have %d digits", want)
	}
	return Document{Type: typ, Number: digits}, nil
}

func validateContact(name, email string) error {
	if strings.TrimSpace(name) == "" {
		return kernel.Invalid("name is required")
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return kernel.Invalid("invalid email")
	}
	return nil
}

type AccountStatus string

const (
	AccountPending  AccountStatus = "pending"
	AccountActive   AccountStatus = "active"
	AccountCanceled AccountStatus = "canceled"
)

// ponytail: no accrediting step (no KYC); add it when a PSP needs account-level onboarding.
var accountFSM = kernel.FSM[AccountStatus]{
	"accredit": {AccountPending: AccountActive},
	"cancel":   {AccountActive: AccountCanceled},
}

type Account struct {
	ID        uuid.UUID
	Name      string
	Email     string
	Document  Document
	Sources   []string // consumers allowed to act on this account
	Status    AccountStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

func NewAccount(name, email string, doc Document, source string) (Account, error) {
	if err := validateContact(name, email); err != nil {
		return Account{}, err
	}
	return Account{Name: name, Email: email, Document: doc, Sources: []string{source}, Status: AccountPending}, nil
}

func (a *Account) Accredit() (err error) {
	a.Status, err = accountFSM.Next(a.Status, "accredit")
	return err
}

func (a Account) Active() bool { return a.Status == AccountActive }
