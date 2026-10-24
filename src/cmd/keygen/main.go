// keygen mencetak tiga kunci acak 32 byte (base64) untuk .env. Jangan pernah commit hasilnya.
package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
)

func main() {
	for _, name := range []string{"SESSION_ENCRYPTION_KEY", "CSRF_HMAC_KEY", "CONNECTION_ENCRYPTION_KEY"} {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			fmt.Fprintln(os.Stderr, "cannot read random bytes")
			os.Exit(1)
		}
		fmt.Printf("%s=%s\n", name, base64.StdEncoding.EncodeToString(key))
	}
}
