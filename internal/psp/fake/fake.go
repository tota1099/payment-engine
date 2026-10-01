// Package fake is an in-memory PSP with deterministic outcomes, for dev and tests.
//
// Tokens drive charge results: "tok_decline" is declined, anything else is
// authorized (credit_card) or pending (pix, boleto). Webhooks are signed like
// Stripe's: header Fake-Signature: t=<unix>,v1=<hex hmac-sha256(secret, "t.body")>.
package fake

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/renanporto/payment-engine/internal/psp"
)

const (
	SignatureHeader = "Fake-Signature"
	ReplayWindow    = 10 * time.Minute
)

type Provider struct {
	Secret []byte
	Now    func() time.Time
}

func New(secret string) *Provider { return &Provider{Secret: []byte(secret), Now: time.Now} }

func (p *Provider) Name() string { return "fake" }

// Ids derive from the idempotency key, so retries return the same object.
func (p *Provider) CreateConnectedAccount(_ context.Context, key string, _ psp.Workspace) (string, error) {
	return "acct_fake_" + key, nil
}

func (p *Provider) CreateCheckout(_ context.Context, key, _ string, _ psp.Checkout) (string, string, error) {
	id := "plink_fake_" + key
	return id, "https://pay.fake.local/" + id, nil
}

func (p *Provider) Charge(_ context.Context, r psp.ChargeRequest) (psp.ChargeResult, error) {
	res := psp.ChargeResult{ExternalID: "pi_fake_" + r.IdempotencyKey, CreatedAt: p.Now(), Code: "00"}
	switch {
	case r.TokenID == "tok_decline":
		res.Status, res.Code = psp.ChargeDeclined, "card_declined"
	case r.PaymentMethod == "credit_card":
		res.Status = psp.ChargeAuthorized
	default:
		res.Status = psp.ChargePending
	}
	return res, nil
}

func (p *Provider) ParseWebhook(h http.Header, body []byte) (psp.Event, error) {
	var ts, sig string
	for part := range strings.SplitSeq(h.Get(SignatureHeader), ",") {
		k, v, _ := strings.Cut(part, "=")
		switch k {
		case "t":
			ts = v
		case "v1":
			sig = v
		}
	}
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return psp.Event{}, fmt.Errorf("%w: missing timestamp", psp.ErrInvalidWebhook)
	}
	if !hmac.Equal([]byte(sig), []byte(p.sign(ts, body))) {
		return psp.Event{}, fmt.Errorf("%w: bad signature", psp.ErrInvalidWebhook)
	}
	if d := p.Now().Sub(time.Unix(unix, 0)); d > ReplayWindow || d < -ReplayWindow {
		return psp.Event{}, fmt.Errorf("%w: outside replay window", psp.ErrInvalidWebhook)
	}
	var ev psp.Event
	if err := json.Unmarshal(body, &ev); err != nil || ev.ID == "" || ev.Type == "" {
		return psp.Event{}, fmt.Errorf("%w: malformed body", psp.ErrInvalidWebhook)
	}
	ev.Raw = body
	return ev, nil
}

// SignatureFor returns the header value a real sender would attach to body.
func (p *Provider) SignatureFor(body []byte, at time.Time) string {
	ts := strconv.FormatInt(at.Unix(), 10)
	return "t=" + ts + ",v1=" + p.sign(ts, body)
}

func (p *Provider) sign(ts string, body []byte) string {
	m := hmac.New(sha256.New, p.Secret)
	m.Write([]byte(ts + "."))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}
