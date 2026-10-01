package fake

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/renanporto/payment-engine/internal/psp"
)

func TestParseWebhook(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	p := &Provider{Secret: []byte("s"), Now: func() time.Time { return now }}
	body := []byte(`{"id":"evt_1","type":"bill.captured","external_id":"pi_1"}`)
	header := func(sig string) http.Header { h := http.Header{}; h.Set(SignatureHeader, sig); return h }

	ev, err := p.ParseWebhook(header(p.SignatureFor(body, now)), body)
	if err != nil || ev.ID != "evt_1" || ev.Type != psp.EventBillCaptured {
		t.Fatalf("valid webhook: %+v %v", ev, err)
	}

	bad := map[string]http.Header{
		"tampered body": header(p.SignatureFor([]byte(`{}`), now)),
		"wrong secret":  header((&Provider{Secret: []byte("x")}).SignatureFor(body, now)),
		"too old":       header(p.SignatureFor(body, now.Add(-ReplayWindow-time.Second))),
		"from future":   header(p.SignatureFor(body, now.Add(ReplayWindow+time.Second))),
		"no header":     {},
	}
	for name, h := range bad {
		if _, err := p.ParseWebhook(h, body); !errors.Is(err, psp.ErrInvalidWebhook) {
			t.Errorf("%s: want ErrInvalidWebhook, got %v", name, err)
		}
	}
}
