// Temporary audit helper: sends a signed Offer(MergeRequest) activity.
// Not part of the solution.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"forgejo.org/modules/activitypub"
	"forgejo.org/modules/setting"
)

//nolint:forbidigo
func main() {
	if len(os.Args) < 5 {
		fmt.Println("usage: offerfetch <inboxURL> <privKeyFile> <keyID> <payload>")
		os.Exit(1)
	}
	inbox := os.Args[1]
	privPem, err := os.ReadFile(os.Args[2])
	if err != nil {
		fmt.Println("read key:", err)
		os.Exit(1)
	}
	keyID := os.Args[3]
	payload := []byte(os.Args[4])

	ctx := context.Background()
	setting.Federation.InsecureAllowInvalidHosts = true
	setting.Federation.MaxSize = 4 << 20
	cf, err := activitypub.NewClientFactoryWithTimeout(20 * time.Second)
	if err != nil {
		fmt.Println("client factory:", err)
		os.Exit(1)
	}
	c, err := cf.WithKeysDirect(ctx, string(privPem), keyID, nil)
	if err != nil {
		fmt.Println("withkeys:", err)
		os.Exit(1)
	}
	resp, err := c.Post(payload, inbox)
	if err != nil {
		fmt.Println("POST:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	fmt.Println("status:", resp.StatusCode)
}
