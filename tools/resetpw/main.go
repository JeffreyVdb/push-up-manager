// Command resetpw prints the SQL that resets one account's password.
//
// The app has no password-reset flow, so an operator hashes the new password
// here and pipes the statements straight into sqlite3 on the host:
//
//	printf '%s' "$pw" | go run ./tools/resetpw exampleuser |
//	    sqlite3 /path/to/pushups.db
//
// The password is read from stdin so it never reaches argv, and only the
// derived hash leaves this process. Every session belonging to the account is
// dropped as well, so the old password cannot ride an existing cookie.
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"golang.org/x/crypto/argon2"
)

// Kept in step with the root package: auth.go owns the canonical parameters
// and verifyPassword parses this exact record layout.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
	argonSaltLen = 16

	minPasswordLen = 8
	maxPasswordLen = 256
)

func hashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// checkUsername mirrors validateUsername, which also keeps the name safe to
// drop into the SQL literals below.
func checkUsername(name string) error {
	if len(name) < 3 || len(name) > 32 {
		return fmt.Errorf("username must be 3-32 characters")
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' && r != '.' {
			return fmt.Errorf("username may only contain letters, digits, and _ - .")
		}
	}
	return nil
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: printf '%%s' \"$password\" | resetpw <username>")
	}
	username := os.Args[1]
	if err := checkUsername(username); err != nil {
		return err
	}

	raw, err := io.ReadAll(bufio.NewReader(os.Stdin))
	if err != nil {
		return err
	}
	// Only the line ending goes; a password may legitimately end in a space.
	password := strings.TrimRight(string(raw), "\r\n")
	if len(password) < minPasswordLen || len(password) > maxPasswordLen {
		return fmt.Errorf("password must be %d-%d characters, got %d", minPasswordLen, maxPasswordLen, len(password))
	}

	hash, err := hashPassword(password)
	if err != nil {
		return err
	}

	// .timeout matches the app's _busy_timeout: the service keeps the database
	// open, and the sqlite3 CLI would otherwise fail instantly on a lock.
	fmt.Printf(`.timeout 5000
BEGIN;
UPDATE users SET password_hash = '%s' WHERE username = '%s';
SELECT 'updated ' || changes() || ' row(s)';
DELETE FROM sessions WHERE user_id = (SELECT id FROM users WHERE username = '%s');
COMMIT;
`, hash, username, username)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "resetpw:", err)
		os.Exit(1)
	}
}
