package mockadmin

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/amanachenko/cardo/internal/source/console"
)

func TestHandlerAnswersTheDocumentedShape(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(Handler(Config{Days: 1, Users: 2, APIKeys: 1, Now: now}))
	defer srv.Close()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	c, err := console.New("sk-ant-test", log, console.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	recs, err := c.Fetch(context.Background(), now.AddDate(0, 0, -1))
	if err != nil {
		t.Fatalf("the stand-in must parse through the real adapter: %v", err)
	}
	if len(recs) != 3 {
		t.Fatalf("got %d records, want 3", len(recs))
	}
	types := map[string]int{}
	for _, r := range recs {
		types[r.ActorType]++
		if len(r.Unknown) != 0 {
			t.Fatalf("the stand-in must model every field the adapter knows; unknown: %v", r.Unknown)
		}
		if len(r.ActorPath) == 0 {
			t.Fatal("the adapter must report where the identity sits in the payload")
		}
	}
	if types["user_actor"] != 2 || types["api_actor"] != 1 {
		t.Fatalf("actor types = %v, want 2 user_actor and 1 api_actor", types)
	}
}

func TestHandlerCanEmitAnUnknownType(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(Handler(Config{Days: 1, Users: 0, APIKeys: 0, Unknown: 1, Now: now}))
	defer srv.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	c, _ := console.New("sk-ant-test", log, console.WithBaseURL(srv.URL))
	recs, err := c.Fetch(context.Background(), now.AddDate(0, 0, -1))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].ActorType != "service_account_actor" {
		t.Fatalf("want one unknown actor, got %+v", recs)
	}
}
