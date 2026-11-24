package auth

import "testing"

func TestHashAndVerify(t *testing.T) {
	h := Hasher{Iterations: 1000}
	encoded, err := h.Hash("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !Verify(encoded, "correct horse") {
		t.Fatal("the right password was rejected")
	}
	if Verify(encoded, "Correct horse") {
		t.Fatal("a wrong password was accepted")
	}

	again, _ := h.Hash("correct horse")
	if again == encoded {
		t.Fatal("two hashes of the same password should differ (random salt)")
	}
}

func TestVerifyRejectsGarbage(t *testing.T) {
	for _, s := range []string{"", "plain", "pbkdf2_sha256$x$y$z", "md5$1$a$b"} {
		if Verify(s, "anything") {
			t.Fatalf("Verify(%q) accepted a password", s)
		}
	}
}

func TestTokenHashIsStable(t *testing.T) {
	token, hash := NewToken()
	if token == "" || HashToken(token) != hash {
		t.Fatal("token hash mismatch")
	}
}
