// Command seedbig mass-seeds zero-byte objects into one S3 prefix — the
// live-fixture seeder for the big-folder law (GUI-111's battery row and the
// empirical 100M-object validation against the local MinIO container).
// One process, N concurrent puts, deterministic zero-padded keys so key
// order is stable across paginators. -rm deletes exactly the same key set
// (batched), so a walk's fixture cleans up after itself.
//
// Usage:
//
//	go run ./tools/seedbig -bucket s3b-perf -prefix big/ -n 2500 -workers 16
//	go run ./tools/seedbig -bucket s3b-perf -prefix big/ -n 2500 -rm
//
// Dev-only fixture tool; never shipped. Plain main package on purpose: it
// joins the docker race-gate package list (no wails imports).
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func main() {
	endpoint := flag.String("endpoint", "http://127.0.0.1:9000", "S3 endpoint")
	key := flag.String("key", "minioadmin", "access key")
	secret := flag.String("secret", "minioadmin", "secret key")
	bucket := flag.String("bucket", "", "target bucket (created when absent)")
	prefix := flag.String("prefix", "", "key prefix (a trailing slash is added when missing)")
	n := flag.Int("n", 1000, "object count")
	workers := flag.Int("workers", 16, "concurrent puts")
	region := flag.String("region", "us-east-1", "signing region")
	rm := flag.Bool("rm", false, "delete the key set instead of seeding it")
	flag.Parse()

	if *bucket == "" {
		fmt.Fprintln(os.Stderr, "seedbig: -bucket is required")
		os.Exit(2)
	}
	if *n < 0 {
		fmt.Fprintln(os.Stderr, "seedbig: -n must not be negative")
		os.Exit(2)
	}
	p := *prefix
	if p != "" && p[len(p)-1] != '/' {
		p += "/"
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(*region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(*key, *secret, "")),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "seedbig: credentials:", err)
		os.Exit(1)
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(*endpoint)
		o.UsePathStyle = true
	})

	// The bucket comes into existence when absent (HeadBucket → Create):
	// a fixture seeder owns its scratch bucket.
	if _, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: bucket}); err != nil {
		if _, cerr := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: bucket}); cerr != nil {
			fmt.Fprintln(os.Stderr, "seedbig: create bucket:", cerr)
			os.Exit(1)
		}
		fmt.Printf("seedbig: bucket %s created\n", *bucket)
	}

	// keyOf is stable and zero-padded past the 100M digit count so the
	// listing's key order never shifts with scale.
	keyOf := func(i int) string { return fmt.Sprintf("%sbig-%09d.txt", p, i) }

	start := time.Now()
	if *rm {
		nb, err := removeSet(ctx, client, *bucket, keyOf, *n)
		if err != nil {
			fmt.Fprintln(os.Stderr, "seedbig:", err)
			os.Exit(1)
		}
		fmt.Printf("seedbig: deleted %d keys from s3://%s/%s in %s\n", nb, *bucket, p, time.Since(start).Round(time.Millisecond))
		return
	}

	var done atomic.Int64
	errCh := make(chan error, *workers)
	idx := make(chan int, *workers*2)
	var wg sync.WaitGroup
	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range idx {
				if _, err := client.PutObject(ctx, &s3.PutObjectInput{
					Bucket: bucket,
					Key:    aws.String(keyOf(i)),
					Body:   bytes.NewReader(nil),
				}); err != nil {
					select {
					case errCh <- fmt.Errorf("put %s: %w", keyOf(i), err):
					default:
					}
					cancel()
					return
				}
				if d := done.Add(1); d%10000 == 0 {
					fmt.Printf("seedbig: %d/%d keys\n", d, *n)
				}
			}
		}()
	}
feed:
	for i := 0; i < *n; i++ {
		select {
		case idx <- i:
		case <-ctx.Done(): // a worker failed: stop feeding, drain, report
			break feed
		}
	}
	close(idx)
	wg.Wait()
	select {
	case err := <-errCh:
		fmt.Fprintln(os.Stderr, "seedbig:", err)
		os.Exit(1)
	default:
	}
	el := time.Since(start).Round(time.Millisecond)
	rate := float64(*n) / time.Since(start).Seconds()
	fmt.Printf("seedbig: seeded %d keys into s3://%s/%s in %s (%.0f puts/s)\n", *n, *bucket, p, el, rate)
}

// removeSet deletes the exact key set the seeder would write, in batches
// of 1000 (the API maximum), returning the deleted count.
func removeSet(ctx context.Context, client *s3.Client, bucket string, keyOf func(int) string, n int) (int, error) {
	deleted := 0
	for start := 0; start < n; start += 1000 {
		end := min(start+1000, n)
		var objs []s3types.ObjectIdentifier
		for i := start; i < end; i++ {
			objs = append(objs, s3types.ObjectIdentifier{Key: aws.String(keyOf(i))})
		}
		out, err := client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucket),
			Delete: &s3types.Delete{Objects: objs, Quiet: aws.Bool(true)},
		})
		if err != nil {
			return deleted, fmt.Errorf("delete batch at %d: %w", start, err)
		}
		for _, e := range out.Errors {
			return deleted, fmt.Errorf("delete %s: %s", aws.ToString(e.Key), aws.ToString(e.Message))
		}
		deleted += end - start
		if deleted%10000 == 0 {
			fmt.Printf("seedbig: deleted %d/%d keys\n", deleted, n)
		}
	}
	return deleted, nil
}
