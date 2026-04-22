package auth

import (
	"strings"
	"testing"
)

func TestHash_VerifyRoundTrip(t *testing.T) {
	enc, err := Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if enc.Algo != "argon2id" {
		t.Fatalf("algo = %q, want argon2id", enc.Algo)
	}
	if !Verify(enc, "correct horse battery staple") {
		t.Fatal("Verify returned false for correct password")
	}
	if Verify(enc, "wrong password") {
		t.Fatal("Verify returned true for wrong password")
	}
}

func TestHash_Unique(t *testing.T) {
	a, err := Hash("same")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Hash("same")
	if err != nil {
		t.Fatal(err)
	}
	if a.SaltB64 == b.SaltB64 {
		t.Fatal("two hashes of the same password share a salt")
	}
	if a.HashB64 == b.HashB64 {
		t.Fatal("two hashes of the same password produce identical bytes")
	}
}

func TestVerify_RejectsEmpty(t *testing.T) {
	enc, err := Hash("real")
	if err != nil {
		t.Fatal(err)
	}
	if Verify(enc, "") {
		t.Fatal("Verify accepted empty password")
	}
}

func TestVerify_MalformedEncoded_ReturnsFalse(t *testing.T) {
	// Empty struct should not crash, should return false.
	if Verify(Encoded{}, "anything") {
		t.Fatal("Verify on empty Encoded returned true")
	}
	bad := Encoded{Algo: "argon2id", SaltB64: "!!!not-base64!!!", HashB64: "aGVsbG8="}
	if Verify(bad, "anything") {
		t.Fatal("Verify on malformed salt returned true")
	}
}

func TestHash_ContainsExpectedFields(t *testing.T) {
	enc, err := Hash("x")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(enc.SaltB64) == "" || strings.TrimSpace(enc.HashB64) == "" {
		t.Fatal("Hash produced empty salt or hash")
	}
	if enc.Params.M == 0 || enc.Params.T == 0 || enc.Params.P == 0 {
		t.Fatalf("Hash params zero: %+v", enc.Params)
	}
}
