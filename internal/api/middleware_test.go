package api

import "testing"

func TestLogSafeStripsLineBreaks(t *testing.T) {
	cases := map[string]string{
		"/api/products":                 "/api/products",
		"/api/x\nlevel=INFO msg=forged": "/api/xlevel=INFO msg=forged",
		"/api/x\r\nmsg=forged":          "/api/xmsg=forged",
		"/api/orders/1\r":               "/api/orders/1",
	}
	for in, want := range cases {
		if got := logSafe(in); got != want {
			t.Errorf("logSafe(%q) = %q, want %q", in, got, want)
		}
	}
}
