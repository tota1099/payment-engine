package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/renanporto/payment-engine/internal/account/domain"
	"github.com/renanporto/payment-engine/internal/kernel"
)

type AccountInteractor struct {
	Repo AccountRepository
	Tx   kernel.Tx
}

var _ Accounts = (*AccountInteractor)(nil)

func (uc *AccountInteractor) Create(ctx context.Context, in CreateAccountInput) (domain.Account, error) {
	doc, err := domain.NewDocument(in.DocumentType, in.DocumentNumber)
	if err != nil {
		return domain.Account{}, err
	}
	a, err := domain.NewAccount(in.Name, in.Email, doc, in.Source)
	if err != nil {
		return a, err
	}
	return uc.Repo.Create(ctx, a)
}

func (uc *AccountInteractor) Get(ctx context.Context, id uuid.UUID) (domain.Account, error) {
	return uc.Repo.Get(ctx, id)
}

func (uc *AccountInteractor) Accredit(ctx context.Context, id uuid.UUID) (a domain.Account, err error) {
	err = uc.Tx(ctx, func(ctx context.Context) error {
		if a, err = uc.Repo.GetForUpdate(ctx, id); err != nil {
			return err
		}
		if err := a.Accredit(); err != nil {
			return err
		}
		return uc.Repo.Save(ctx, &a)
	})
	return a, err
}
