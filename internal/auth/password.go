package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"
)

// PasswordParams are persisted inside each Argon2id PHC string.
type PasswordParams struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultPasswordParams implements the S5 security decision: 64 MiB, t=3, p=2.
var DefaultPasswordParams = PasswordParams{
	Memory: 64 * 1024, Iterations: 3, Parallelism: 2, SaltLength: 16, KeyLength: 32,
}

// HashPassword produces an Argon2id PHC string.
func HashPassword(password string, params PasswordParams) (string, error) {
	return hashPassword(password, params, rand.Reader)
}

func hashPassword(password string, params PasswordParams, source io.Reader) (string, error) {
	if err := validatePasswordParams(params); err != nil {
		return "", err
	}
	salt := make([]byte, params.SaltLength)
	if _, err := io.ReadFull(source, salt); err != nil {
		return "", fmt.Errorf("read password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, params.KeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version,
		params.Memory, params.Iterations, params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword verifies a PHC string and rejects malformed or excessive
// parameters before allocating Argon2 memory.
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, errors.New("invalid Argon2id PHC string")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errors.New("unsupported Argon2id version")
	}
	params := PasswordParams{}
	var parallelism uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &params.Memory, &params.Iterations, &parallelism); err != nil || parallelism > 255 {
		return false, errors.New("invalid Argon2id parameters")
	}
	params.Parallelism = uint8(parallelism)
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, errors.New("invalid Argon2id salt")
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, errors.New("invalid Argon2id key")
	}
	params.SaltLength = uint32(len(salt))
	params.KeyLength = uint32(len(want))
	if err := validatePasswordParams(params); err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, params.KeyLength)
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

func validatePasswordParams(params PasswordParams) error {
	if params.Memory < 8*1024 || params.Memory > 256*1024 {
		return errors.New("Argon2id memory must be between 8 MiB and 256 MiB")
	}
	if params.Iterations < 1 || params.Iterations > 10 {
		return errors.New("Argon2id iterations must be between 1 and 10")
	}
	if params.Parallelism < 1 || params.Parallelism > 8 {
		return errors.New("Argon2id parallelism must be between 1 and 8")
	}
	if params.SaltLength < 16 || params.SaltLength > 64 {
		return errors.New("Argon2id salt length must be between 16 and 64 bytes")
	}
	if params.KeyLength < 16 || params.KeyLength > 64 {
		return errors.New("Argon2id key length must be between 16 and 64 bytes")
	}
	return nil
}
