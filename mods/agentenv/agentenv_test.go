package agentenv

import "testing"

// Regression test (2026-09-20): the Agent Configuration connection test must
// actually validate keys. OpenRouter's and ollama.com's /v1/models endpoints
// are public — they 200 with any key or none at all — so probing them
// reported "key accepted" for garbage keys while Hey Lain's brain config
// (which really authenticates) 401'd. Lock in the genuine probes.
func TestGenuineKeyProbes(t *testing.T) {
	byID := map[string]Provider{}
	for _, p := range Providers() {
		byID[p.ID] = p
	}
	or, ok := byID["openrouter"]
	if !ok {
		t.Fatal("openrouter provider missing")
	}
	if or.ModelsURL != "https://openrouter.ai/api/v1/auth/key" {
		t.Errorf("openrouter probe = %q, want the key-info endpoint (their /v1/models is public)", or.ModelsURL)
	}
	oc, ok := byID["ollama-cloud"]
	if !ok {
		t.Fatal("ollama-cloud provider missing")
	}
	if oc.TestMethod != "POST" || oc.TestBody == "" || !oc.TestSoftFail {
		t.Errorf("ollama-cloud probe = method %q body-len %d softfail %v, want POST+bogus-body+softfail (their /v1/models is public)",
			oc.TestMethod, len(oc.TestBody), oc.TestSoftFail)
	}
}
