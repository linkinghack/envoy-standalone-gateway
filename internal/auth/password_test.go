package auth

import (
	"bytes"
	"strings"
	"testing"
)

var testPasswordParams = PasswordParams{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 16}

func TestPasswordPHCRoundTrip(t *testing.T) {
	t.Parallel()
	encoded, err := hashPassword("correct horse battery staple", testPasswordParams, bytes.NewReader(make([]byte, 16)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=8192,t=1,p=1$") {
		t.Fatalf("unexpected PHC string %q", encoded)
	}
	ok, err := VerifyPassword("correct horse battery staple", encoded)
	if err != nil || !ok {
		t.Fatalf("correct password = %v, %v", ok, err)
	}
	ok, err = VerifyPassword("wrong password", encoded)
	if err != nil || ok {
		t.Fatalf("wrong password = %v, %v", ok, err)
	}
	if _, err := VerifyPassword("anything", "$argon2id$v=19$m=999999,t=1,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAA"); err == nil {
		t.Fatal("excessive memory PHC accepted")
	}
}
