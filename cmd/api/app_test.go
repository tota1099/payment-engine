package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/renanporto/payment-engine/internal/eventbus"
	"github.com/renanporto/payment-engine/internal/psp/fake"
	"github.com/renanporto/payment-engine/internal/testdb"
)

type client struct {
	t       *testing.T
	base    string
	headers map[string]string
}

type resp struct {
	status int
	body   map[string]any
}

func (c client) do(method, path string, body any, extra ...string) resp {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, r)
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	for i := 0; i+1 < len(extra); i += 2 {
		req.Header.Set(extra[i], extra[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	out := resp{status: res.StatusCode}
	_ = json.NewDecoder(res.Body).Decode(&out.body)
	return out
}

func (c client) must(want int, method, path string, body any, extra ...string) map[string]any {
	c.t.Helper()
	r := c.do(method, path, body, extra...)
	if r.status != want {
		c.t.Fatalf("%s %s: status %d, want %d: %v", method, path, r.status, want, r.body)
	}
	return r.body
}

func TestPaymentFlow(t *testing.T) {
	pool := testdb.New(t)
	jobs, err := eventbus.NewClient(pool, false)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(newApp(pool, fake.New("secret"), eventbus.Publisher{Client: jobs}, true))
	defer srv.Close()

	crm := client{t: t, base: srv.URL, headers: map[string]string{"X-Consumer-Username": "crm"}}
	acc := crm.must(201, "POST", "/api/v1/accounts", map[string]any{
		"name": "Acme", "email": "a@acme.com", "document_type": "cnpj", "document_number": "11.222.333/0001-81",
	})
	accID := acc["id"].(string)
	api := client{t: t, base: srv.URL, headers: map[string]string{"X-Consumer-Username": "crm", "X-Account-Id": accID}}
	intruder := client{t: t, base: srv.URL, headers: map[string]string{"X-Consumer-Username": "other", "X-Account-Id": accID}}
	psp := func(want int, ev map[string]any) map[string]any { return crm.must(want, "POST", "/dev/fake-psp/events", ev) }

	intruder.must(403, "GET", "/api/v1/workspaces", nil)
	ws := api.must(201, "POST", "/api/v1/workspaces", map[string]any{
		"name": "Shop", "email": "s@acme.com", "document_type": "cpf", "document_number": "12345678901", "mcc": "5411",
	})["id"].(string)
	wsPath := "/api/v1/workspaces/" + ws
	api.must(422, "POST", wsPath+"/accredit", nil) // account not active yet
	if got := api.must(200, "POST", "/api/v1/accounts/"+accID+"/accredit", nil)["status"]; got != "active" {
		t.Fatalf("account status %v", got)
	}
	api.must(200, "POST", wsPath+"/accredit", nil)
	api.must(422, "POST", wsPath+"/checkouts", checkoutBody()) // not active until the PSP says so
	psp(200, map[string]any{"type": "workspace.activated", "external_id": "acct_fake_" + ws})
	if got := api.must(200, "GET", wsPath, nil)["status"]; got != "active" {
		t.Fatalf("workspace status %v", got)
	}

	co := api.must(201, "POST", wsPath+"/checkouts", checkoutBody())["id"].(string)
	card := map[string]any{"amount": 1000, "payment_method": "credit_card", "token_id": "tok_ok", "checkout_id": co}

	// Idempotency: same key from 5 concurrent clients → one bill, one 201.
	ids, created := map[string]bool{}, 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			r := api.do("POST", wsPath+"/bills", card, "Idempotency-Key", "k1")
			mu.Lock()
			defer mu.Unlock()
			if r.status == 201 {
				created++
			} else if r.status != 200 {
				t.Errorf("same-key retry: status %d %v", r.status, r.body)
			}
			ids[fmt.Sprint(r.body["id"])] = true
		})
	}
	wg.Wait()
	if created != 1 || len(ids) != 1 {
		t.Fatalf("same key: created=%d distinct bills=%d; want 1 and 1", created, len(ids))
	}
	var bill string
	for id := range ids {
		bill = id
	}
	api.must(422, "POST", wsPath+"/bills", map[string]any{"amount": 999, "payment_method": "credit_card", "token_id": "t"}, "Idempotency-Key", "k1")

	// max_uses=2 with 1 used: of 5 concurrent payments with distinct keys, exactly 1 fits.
	created = 0
	for i := range 5 {
		wg.Go(func() {
			pix := map[string]any{"amount": 500, "payment_method": "pix", "checkout_id": co}
			r := api.do("POST", wsPath+"/bills", pix, "Idempotency-Key", fmt.Sprint("pix-", i))
			mu.Lock()
			defer mu.Unlock()
			if r.status == 201 {
				created++
			} else if r.status != 422 {
				t.Errorf("max_uses race: status %d %v", r.status, r.body)
			}
		})
	}
	wg.Wait()
	if created != 1 {
		t.Fatalf("max_uses race: %d created; want 1", created)
	}

	// Webhooks: capture, duplicate, rejected transition, chargeback, unknown object.
	capture := map[string]any{"id": "evt_cap", "type": "bill.captured", "external_id": "pi_fake_" + bill}
	psp(200, capture)
	psp(200, capture)
	psp(200, map[string]any{"type": "bill.voided", "external_id": "pi_fake_" + bill})
	psp(200, map[string]any{"type": "bill.charged_back", "external_id": "pi_fake_" + bill, "code": "fraud"})
	psp(404, map[string]any{"type": "bill.captured", "external_id": "pi_nope"})

	txs := api.must(200, "GET", wsPath+"/bills/"+bill+"/transactions", nil)["data"].([]any)
	var statuses []string
	for _, tx := range txs {
		statuses = append(statuses, tx.(map[string]any)["status"].(string))
	}
	if fmt.Sprint(statuses) != "[authorized captured charged_back]" {
		t.Fatalf("transactions = %v", statuses)
	}

	page := api.must(200, "GET", wsPath+"/checkouts/"+co+"/bills?limit=1", nil)
	if len(page["data"].([]any)) != 1 || page["next_cursor"] == nil {
		t.Fatalf("pagination: %v", page)
	}

	declined := api.must(201, "POST", wsPath+"/bills", map[string]any{"amount": 1000, "payment_method": "credit_card", "token_id": "tok_decline"}, "Idempotency-Key", "k-decline")
	if declined["status"] != "error" {
		t.Fatalf("declined bill status %v", declined["status"])
	}
	if !(declined["updated_at"].(string) > declined["created_at"].(string)) { // RFC 3339, same offset
		t.Fatalf("updated_at %v not after created_at %v: Save must refresh it", declined["updated_at"], declined["created_at"])
	}

	// Domain events became River jobs in the same transactions, in order.
	events := func() (out []string) {
		rows, _ := pool.Query(t.Context(), "SELECT args->>'event' || ':' || coalesce(args->'payload'->>'status', '') FROM river_job ORDER BY id")
		for rows.Next() {
			var s string
			_ = rows.Scan(&s)
			out = append(out, s)
		}
		return out
	}
	want := "[workspace_activation_completed: bill_update_completed:authorized bill_update_completed:captured bill_update_completed:charged_back bill_update_completed:error]"
	if got := fmt.Sprint(events()); got != want {
		t.Fatalf("events =\n%v\nwant\n%v", got, want)
	}

	// A worker drains them.
	worker, err := eventbus.NewClient(pool, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer worker.Stop(context.Background())
	deadline := time.Now().Add(10 * time.Second)
	for {
		var pending int
		_ = pool.QueryRow(t.Context(), "SELECT count(*) FROM river_job WHERE state <> 'completed'").Scan(&pending)
		if pending == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d jobs not completed", pending)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func checkoutBody() map[string]any {
	return map[string]any{
		"name": "Plan", "items": []map[string]any{{"name": "x", "amount": 1000, "quantity": 1}},
		"payment_methods": []string{"credit_card", "pix"}, "max_uses": 2,
	}
}
