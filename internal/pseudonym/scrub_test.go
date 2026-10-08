package pseudonym

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/amanachenko/cardo/internal/source"
)

func testRecord(raw string) source.Record {
	return source.Record{
		Source:       source.Console,
		Day:          time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC),
		ActorType:    "user_actor",
		ActorID:      "Developer@Example.com",
		ActorPath:    []string{"actor", "email_address"},
		OrgID:        "org-1",
		CustomerType: "api",
		TerminalType: "vscode",
		Raw:          json.RawMessage(raw),
	}
}

// The central test of INV-2. If this fails, a real email address is being written to storage.
func TestScrubRemovesIdentityFromPayload(t *testing.T) {
	h, _ := New(testSalt, "v1")
	rec := testRecord(`{"actor":{"type":"user_actor","email_address":"Developer@Example.com"},"core_metrics":{"num_sessions":5}}`)

	got, err := h.Scrub(rec)
	if err != nil {
		t.Fatal(err)
	}

	raw := string(got.Raw())
	if strings.Contains(strings.ToLower(raw), "developer@example.com") {
		t.Fatalf("scrubbed payload still contains the email address: %s", raw)
	}
	if strings.Contains(raw, "email_address") {
		t.Errorf("scrubbed payload still carries the email_address key: %s", raw)
	}
	if !strings.Contains(raw, got.Pseudonym()) {
		t.Errorf("scrubbed payload does not carry the pseudonym: %s", raw)
	}
	if !strings.Contains(raw, "pseudonym_salt_version") {
		t.Errorf("scrubbed payload does not carry the salt version: %s", raw)
	}
}

// Everything the source said, except who said it. A scrub that also dropped metrics would defeat
// ADR-0010's reason for storing payloads raw.
func TestScrubPreservesEverythingElse(t *testing.T) {
	h, _ := New(testSalt, "v1")
	rec := testRecord(`{"actor":{"type":"user_actor","email_address":"Developer@Example.com"},` +
		`"core_metrics":{"num_sessions":5,"lines_of_code":{"added":1543}},"unmodelled":{"a":1}}`)

	got, err := h.Scrub(rec)
	if err != nil {
		t.Fatal(err)
	}

	var doc map[string]any
	if err := json.Unmarshal(got.Raw(), &doc); err != nil {
		t.Fatal(err)
	}
	core, ok := doc["core_metrics"].(map[string]any)
	if !ok {
		t.Fatalf("core_metrics missing after scrubbing: %v", doc)
	}
	if core["num_sessions"].(float64) != 5 {
		t.Errorf("num_sessions = %v, want 5", core["num_sessions"])
	}
	if _, ok := doc["unmodelled"]; !ok {
		t.Error("an unmodelled field was dropped; raw payloads must survive verbatim (ADR-0010)")
	}
	if doc["actor"].(map[string]any)["type"] != "user_actor" {
		t.Error("actor.type was removed; only the identifier should be")
	}
}

// The safety net. If a source starts carrying identity in a field the adapter does not model,
// the run must stop rather than write it.
func TestScrubDetectsIdentityLeak(t *testing.T) {
	h, _ := New(testSalt, "v1")
	rec := testRecord(`{"actor":{"type":"user_actor","email_address":"Developer@Example.com"},` +
		`"contact":{"primary_email":"developer@example.com"}}`)

	_, err := h.Scrub(rec)
	if err == nil {
		t.Fatal("expected an identity leak error, got nil — an email address would have been stored")
	}
	var leak ErrIdentityLeak
	if !errors.As(err, &leak) {
		t.Fatalf("error = %v, want ErrIdentityLeak", err)
	}
	if !strings.Contains(err.Error(), "INV-2") {
		t.Errorf("leak error should name the invariant it protects, got: %v", err)
	}
}

// The safety net also catches someone else's email. Looking for the actor's own identifier misses
// a reviewer's or an approver's address in a field the adapter does not model, and misses every
// address beside an API key actor, whose identifier is a key name.
func TestScrubDetectsAnEmailItDidNotModel(t *testing.T) {
	h, _ := New(testSalt, "v1")
	userActor := testRecord(`{"actor":{"type":"user_actor","email_address":"Developer@Example.com"},` +
		`"approved_by":{"contact":"Someone.Else@corp.example.org"}}`)
	keyActor := testRecord(`{"actor":{"type":"api_actor","api_key_name":"ci-pipeline-key"},` +
		`"owner":"key.owner@example.com"}`)
	keyActor.ActorID, keyActor.ActorPath, keyActor.ActorType = "ci-pipeline-key", []string{"actor", "api_key_name"}, "api_actor"

	for name, rec := range map[string]source.Record{"user actor": userActor, "api key actor": keyActor} {
		t.Run(name, func(t *testing.T) {
			got, err := h.Scrub(rec)
			var leak ErrIdentityLeak
			if !errors.As(err, &leak) {
				t.Fatalf("error = %v, want ErrIdentityLeak; the payload would be stored as %s", err, got.Raw())
			}
			if strings.Contains(strings.ToLower(err.Error()), "example") {
				t.Errorf("the leak error repeats the address it found, which puts it in a log: %v", err)
			}
		})
	}
}

func TestScrubHandlesAPIKeyActor(t *testing.T) {
	h, _ := New(testSalt, "v1")
	rec := testRecord(`{"actor":{"type":"api_actor","api_key_name":"ci-pipeline-key"}}`)
	rec.ActorID = "ci-pipeline-key"
	rec.ActorPath = []string{"actor", "api_key_name"}
	rec.ActorType = "api_actor"

	got, err := h.Scrub(rec)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got.Raw()), "ci-pipeline-key") {
		t.Fatalf("api key name survived scrubbing: %s", got.Raw())
	}
	if got.ActorType() != "api_actor" {
		t.Errorf("ActorType() = %q, want api_actor", got.ActorType())
	}
}

// An adapter that does not declare where identity lives must not be able to store anything.
func TestScrubRefusesUnknownActorPath(t *testing.T) {
	h, _ := New(testSalt, "v1")
	rec := testRecord(`{"actor":{"email_address":"Developer@Example.com"}}`)
	rec.ActorPath = nil

	if _, err := h.Scrub(rec); err == nil {
		t.Fatal("expected refusal when the actor path is undeclared")
	}
}

func TestScrubCarriesDimensionsAndSaltVersion(t *testing.T) {
	h, _ := New(testSalt, "2026-q3")
	got, err := h.Scrub(testRecord(`{"actor":{"type":"user_actor","email_address":"Developer@Example.com"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.SaltVersion() != "2026-q3" {
		t.Errorf("SaltVersion() = %q, want 2026-q3", got.SaltVersion())
	}
	if got.Source() != source.Console {
		t.Errorf("Source() = %q, want console", got.Source())
	}
	if got.OrgID() != "org-1" || got.CustomerType() != "api" || got.TerminalType() != "vscode" {
		t.Errorf("dimensions not carried through: %+v", got)
	}
	if !got.Day().Equal(time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("Day() = %v, want 2026-09-08 UTC", got.Day())
	}
}

// Scrubbing the same record twice must give the same pseudonym, or re-polling a revised day would
// produce a second synthetic actor for the same person.
func TestScrubIsIdempotentAcrossCalls(t *testing.T) {
	h, _ := New(testSalt, "v1")
	rec := testRecord(`{"actor":{"type":"user_actor","email_address":"Developer@Example.com"}}`)
	a, _ := h.Scrub(rec)
	b, _ := h.Scrub(rec)
	if a.Pseudonym() != b.Pseudonym() {
		t.Fatal("pseudonym changed between scrubs of the same record")
	}
}
