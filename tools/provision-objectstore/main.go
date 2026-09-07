package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/theflywheel/crest/pkg/store"
)

func main() {
	cfg, ok := store.LoadS3Config()
	if !ok {
		fmt.Fprintln(os.Stderr, "S3 configuration required")
		os.Exit(1)
	}
	client, err := store.NewS3(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = client.EnsureBucket(ctx); err == nil {
		err = client.CheckBucket(ctx)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Object-store bucket provisioned; authenticated readiness passed. No business objects created.")
}
