package main

import (
	"flag"
	"fmt"
	"net/http"
	"time"

	"github.com/amanachenko/cardo/internal/mockadmin"
)

// runMockAdmin serves the documented Analytics API shape on loopback so the poller can be verified
// without an Admin API key (ADR-0047). It is development surface only: the poller refuses any base
// URL that is not loopback, so this cannot become a production redirect.
func runMockAdmin(args []string) error {
	fs := flag.NewFlagSet("mock-admin", flag.ContinueOnError)
	useSynopsis(fs, "mock-admin")
	port := fs.Int("port", 8790, "loopback port to listen on")
	days := fs.Int("days", 2, "days of records, ending yesterday")
	users := fs.Int("users", 2, "user_actor records per day")
	keys := fs.Int("api-keys", 1, "api_actor records per day")
	unknown := fs.Int("unknown", 0, "records with an undocumented actor type, per day")
	if err := fs.Parse(args); err != nil {
		return parsed(err)
	}
	cfg := mockadmin.Config{Days: *days, Users: *users, APIKeys: *keys, Unknown: *unknown, Now: time.Now().UTC()}
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	fmt.Printf("Mock Admin API on %s://%s\n", "http", addr)
	fmt.Printf("  poll it with: cardo poll -base-url %s://%s -days %d -store clickhouse\n", "http", addr, *days)
	return http.ListenAndServe(addr, mockadmin.Handler(cfg))
}
