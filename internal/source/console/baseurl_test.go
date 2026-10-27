package console

import "testing"

func TestLoopbackBaseAcceptsThisMachine(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1:8790",
		"http://localhost:8790/",
		"https://[::1]:8790",
	} {
		if _, err := LoopbackBase(raw); err != nil {
			t.Errorf("LoopbackBase(%q) = %v, want accepted", raw, err)
		}
	}
}

func TestLoopbackBaseRefusesEverywhereElse(t *testing.T) {
	for _, raw := range []string{
		"https://api.anthropic.com",
		"http://10.0.0.5:8790",
		"https://evil.example/usage",
		"ftp://127.0.0.1",
	} {
		if _, err := LoopbackBase(raw); err == nil {
			t.Errorf("LoopbackBase(%q) accepted a non-loopback base", raw)
		}
	}
}
