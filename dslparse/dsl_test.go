package dslparse_test

import (
	"testing"

	"github.com/nexssp/flow/dslparse"
)

func TestParseManifest_SingleLine(t *testing.T) {
	mods, cfg := dslparse.ParseManifest(`users.create:http="POST /api/users":role="admin,operator":rate_limit="100/s":burst=200:concurrency=10:timeout=5s:retry=3:tag="users,write"`)

	m, ok := mods["users.create"]
	if !ok {
		t.Fatalf("users.create missing from parsed manifest: %+v", mods)
	}
	if m.Method != "POST" || m.Path != "/api/users" {
		t.Errorf("route = %s %s", m.Method, m.Path)
	}
	if len(m.Roles) != 2 || m.Roles[0] != "admin" {
		t.Errorf("Roles = %v", m.Roles)
	}
	if m.RateLimit != "100/s" || m.RateLimitRPS != 100 {
		t.Errorf("rate = %q rps = %v", m.RateLimit, m.RateLimitRPS)
	}
	if m.RateLimitBurst != 200 {
		t.Errorf("Burst = %d", m.RateLimitBurst)
	}
	if m.ConcurrencyLimit != 10 {
		t.Errorf("ConcurrencyLimit = %d", m.ConcurrencyLimit)
	}
	if m.Timeout.String() != "5s" {
		t.Errorf("Timeout = %v", m.Timeout)
	}
	if m.RetryMax != 3 {
		t.Errorf("RetryMax = %d", m.RetryMax)
	}
	if len(m.Tags) != 2 || m.Tags[0] != "users" || m.Tags[1] != "write" {
		t.Errorf("Tags = %v", m.Tags)
	}
	if cfg.Strict || len(cfg.Silent) != 0 {
		t.Errorf("config = %+v", cfg)
	}
}

func TestParseManifest_ConfigDirectives(t *testing.T) {
	_, cfg := dslparse.ParseManifest(`@config:silent=dsl/orphan-route,routes/conflict-go-dsl
@config:strict=true
users.create:http="POST /api/users"`)

	if !cfg.Silent["dsl/orphan-route"] || !cfg.Silent["routes/conflict-go-dsl"] {
		t.Errorf("Silent = %+v", cfg.Silent)
	}
	if !cfg.Strict {
		t.Errorf("Strict = false")
	}
}

func TestParseManifest_MergesDuplicateLines(t *testing.T) {
	mods, _ := dslparse.ParseManifest(`svc.op:role=admin:tag=first
svc.op:perm="x:write":tag=second`)

	m := mods["svc.op"]
	if len(m.Roles) != 1 || m.Roles[0] != "admin" {
		t.Errorf("Roles = %v", m.Roles)
	}
	if len(m.Permissions) != 1 || m.Permissions[0] != "x:write" {
		t.Errorf("Permissions = %v", m.Permissions)
	}
	if len(m.Tags) != 2 {
		t.Errorf("Tags = %v, want [first second]", m.Tags)
	}
}

func TestParseManifest_MultiTransport(t *testing.T) {
	mods, _ := dslparse.ParseManifest(`order.create:http="POST /orders":cli="order:create":cron="every 5s":nats="orders.created":topic="order.internal":a2a="orderer"`)

	m := mods["order.create"]
	want := map[string]bool{"http": false, "cli": false, "cron": false, "nats-pubsub": false, "topic": false, "a2a": false}
	for _, b := range m.Transports {
		if _, ok := want[b.Kind]; ok {
			want[b.Kind] = true
		}
	}
	for kind, found := range want {
		if !found {
			t.Errorf("missing transport %q; got %+v", kind, m.Transports)
		}
	}
}

func TestParseManifest_ArrowInsideQuotesNotPipeline(t *testing.T) {
	mods, _ := dslparse.ParseManifest(`users.create:type="CreateReq -> User":http="POST /api/users"`)

	if len(mods) != 1 {
		t.Fatalf("expected 1 action, got %d: %+v", len(mods), mods)
	}
	m, ok := mods["users.create"]
	if !ok {
		t.Fatalf("users.create not parsed")
	}
	if m.ReqType != "CreateReq" || m.ResType != "User" {
		t.Errorf("types = %q -> %q", m.ReqType, m.ResType)
	}
}
