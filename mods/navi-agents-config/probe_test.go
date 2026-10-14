package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	agentenv "github.com/rav3ndust/navi-agentenv"
)

// The bogus-model probe (used for ollama-cloud): the stub 401s on bad keys
// and 400s on good ones — auth passes, then the bogus model lookup fails.
// TestSoftFail must read the 400 as "accepted" and the 401 as "rejected".
func TestSoftFailProbe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s, want /v1/chat/completions", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type = %q, want application/json", r.Header.Get("Content-Type"))
		}
		if got := r.Header.Get("Authorization"); got == "Bearer good-key" {
			w.WriteHeader(400) // auth passed, bogus model rejected
		} else {
			w.WriteHeader(401)
		}
	}))
	defer srv.Close()

	p := agentenv.Provider{
		ID: "probe-test", Name: "Probe", EnvVar: "PROBE_KEY",
		ModelsURL: srv.URL + "/v1/chat/completions", Auth: agentenv.AuthBearer,
		TestMethod: "POST", TestBody: `{"model":"bogus"}`,
		TestSoftFail: true,
	}

	msg := testProvider(p, "good-key")().(testDoneMsg)
	if !msg.ok {
		t.Errorf("good key: ok=false, detail=%q", msg.detail)
	}
	msg = testProvider(p, "bad-key")().(testDoneMsg)
	if msg.ok {
		t.Errorf("bad key: ok=true, detail=%q", msg.detail)
	}

	// Without soft-fail, a 400 is neither accepted nor misreported as rejected.
	p.TestSoftFail = false
	msg = testProvider(p, "good-key")().(testDoneMsg)
	if msg.ok {
		t.Errorf("non-softfail 400 should not be ok, detail=%q", msg.detail)
	}
}
