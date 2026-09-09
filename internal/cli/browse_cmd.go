package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/bucketops"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/spf13/cobra"
)

// listingBuckets lists all buckets visible to the client (shared by
// `profile test` and `ls`).
func listingBuckets(cmd *cobra.Command, c *s3client.Client) ([]s3types.Bucket, error) {
	return listing.ListBuckets(cmd.Context(), c.S3)
}

// dirPrefix normalizes a URI key to directory-prefix form ("photos" →
// "photos/"). Explorer semantics: browsing shows folder contents.
func dirPrefix(u s3URI) string {
	p := u.Key
	if p != "" && !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p
}

func lsCmd() *cobra.Command {
	var recursive bool
	var maxKeys int32
	cmd := &cobra.Command{
		Use:   "ls [s3://bucket[/prefix]]",
		Short: "List buckets, or one directory view of a bucket",
		Long:  "Without an argument lists all buckets.\nWith s3://bucket/prefix shows one directory view (folders + objects);\n--recursive streams every object under the prefix instead.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := resolveClient(cmd.Context())
			if err != nil {
				return err
			}
			if len(args) == 0 {
				return listBucketsView(cmd, c)
			}
			u, err := parseS3URI(args[0])
			if err != nil {
				return err
			}
			prefix := ""
			if u.HasPrefix {
				prefix = dirPrefix(u)
			}
			entries, err := listing.List(cmd.Context(), c.S3, u.Bucket, prefix, listing.Options{
				Recursive: recursive,
				MaxKeys:   maxKeys,
			})
			if err != nil {
				return opErr(err)
			}
			if flagJSON {
				return printJSON(entries)
			}
			for _, e := range entries {
				printEntry(e)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&recursive, "recursive", "r", false, "stream every object under the prefix")
	f.Int32Var(&maxKeys, "max-keys", 0, "keys per page (0 = server default)")
	return cmd
}

func listBucketsView(cmd *cobra.Command, c *s3client.Client) error {
	buckets, err := listingBuckets(cmd, c)
	if err != nil {
		return opErr(err)
	}
	sort.Slice(buckets, func(i, j int) bool {
		return aws.ToString(buckets[i].Name) < aws.ToString(buckets[j].Name)
	})
	if flagJSON {
		rows := make([]map[string]any, 0, len(buckets))
		for _, b := range buckets {
			row := map[string]any{"name": aws.ToString(b.Name)}
			if b.CreationDate != nil {
				row["createdAt"] = b.CreationDate.Format(time.RFC3339)
			}
			rows = append(rows, row)
		}
		return printJSON(rows)
	}
	for _, b := range buckets {
		created := ""
		if b.CreationDate != nil {
			created = b.CreationDate.Local().Format("2006-01-02 15:04")
		}
		fmt.Printf("%s  %s\n", created, aws.ToString(b.Name))
	}
	return nil
}

func printEntry(e listing.Entry) {
	if e.IsDir {
		col.hi.Printf("%9s  %s\n", "DIR", e.Name+"/")
		return
	}
	mod := ""
	if e.LastModified != nil {
		mod = e.LastModified.Local().Format("2006-01-02 15:04")
	}
	fmt.Printf("%9s  %s  %s\n", humanSize(e.Size), mod, e.Name)
}

func treeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tree s3://bucket[/prefix]",
		Short: "Show a bucket subtree as an ASCII tree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := resolveClient(cmd.Context())
			if err != nil {
				return err
			}
			u, err := parseS3URI(args[0])
			if err != nil {
				return err
			}
			prefix := ""
			if u.HasPrefix {
				prefix = dirPrefix(u)
			}
			var keys []string
			if !flagJSON {
				fmt.Println(u.Bucket + "/" + prefix)
			}
			if err := drawTree(cmd.Context(), c, u.Bucket, prefix, "", &keys); err != nil {
				return opErr(err)
			}
			if flagJSON {
				return printJSON(keys)
			}
			return nil
		},
	}
}

// drawTree renders one directory level and recurses into folders.
func drawTree(ctx context.Context, c *s3client.Client, bucket, prefix, indent string, keys *[]string) error {
	entries, err := listing.List(ctx, c.S3, bucket, prefix, listing.Options{})
	if err != nil {
		return err
	}
	for i, e := range entries {
		connector, childIndent := "├── ", indent+"│   "
		if i == len(entries)-1 {
			connector, childIndent = "└── ", indent+"    "
		}
		if flagJSON {
			*keys = append(*keys, e.Key)
		} else if e.IsDir {
			fmt.Printf("%s%s%s/\n", indent, connector, e.Name)
		} else {
			fmt.Printf("%s%s%s\n", indent, connector, e.Name)
		}
		if e.IsDir {
			if err := drawTree(ctx, c, bucket, e.Key, childIndent, keys); err != nil {
				return err
			}
		}
	}
	return nil
}

func duCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "du s3://bucket[/prefix]",
		Short: "Count objects and total size under a prefix",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := resolveClient(cmd.Context())
			if err != nil {
				return err
			}
			u, err := parseS3URI(args[0])
			if err != nil {
				return err
			}
			prefix := ""
			if u.HasPrefix {
				prefix = dirPrefix(u)
			}
			usage, err := listing.Du(cmd.Context(), c.S3, u.Bucket, prefix)
			if err != nil {
				return opErr(err)
			}
			if flagJSON {
				return printJSON(usage)
			}
			fmt.Printf("%10s  %6d object(s)  s3://%s/%s\n",
				humanSize(usage.TotalBytes), usage.ObjectCount, u.Bucket, prefix)
			return nil
		},
	}
}

func statCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stat s3://bucket[/key]",
		Short: "Show bucket or object metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := resolveClient(cmd.Context())
			if err != nil {
				return err
			}
			u, err := parseS3URI(args[0])
			if err != nil {
				return err
			}
			if !u.HasPrefix {
				return statBucket(cmd, c, u.Bucket)
			}
			// stat on a prefix trailing "/" is a folder: report aggregate.
			if u.IsPrefix {
				prefix := dirPrefix(u)
				usage, err := listing.Du(cmd.Context(), c.S3, u.Bucket, prefix)
				if err != nil {
					return opErr(err)
				}
				if flagJSON {
					return printJSON(usage)
				}
				fmt.Printf("folder: s3://%s/%s\n", u.Bucket, prefix)
				fmt.Printf("  objects: %d\n", usage.ObjectCount)
				fmt.Printf("  size:    %s (%d bytes)\n", humanSize(usage.TotalBytes), usage.TotalBytes)
				return nil
			}
			return statObject(cmd, c, u.Bucket, u.Key)
		},
	}
}

func statBucket(cmd *cobra.Command, c *s3client.Client, bucket string) error {
	out, err := bucketops.Head(cmd.Context(), c.S3, bucket)
	if err != nil {
		return opErr(err)
	}
	region := string(out.LocationConstraint)
	if region == "" {
		region = "us-east-1" // S3 returns empty for the us-east-1 default region
	}
	endpoint := orDefault(c.Endpoint, "(AWS default)")
	if flagJSON {
		return printJSON(map[string]any{
			"bucket": bucket, "region": region, "endpoint": endpoint, "provider": c.ProviderCaps.Name,
		})
	}
	fmt.Printf("bucket:   %s\n", bucket)
	fmt.Printf("region:   %s\n", region)
	fmt.Printf("endpoint: %s\n", endpoint)
	fmt.Printf("provider: %s (policy: %s, ACL: %s)\n",
		c.ProviderCaps.Name, c.ProviderCaps.PolicySupport, c.ProviderCaps.ACLSupport)
	return nil
}

type objectStat struct {
	Bucket       string            `json:"bucket"`
	Key          string            `json:"key"`
	Size         int64             `json:"size"`
	LastModified *time.Time        `json:"lastModified,omitempty"`
	ETag         string            `json:"etag,omitempty"`
	StorageClass string            `json:"storageClass,omitempty"`
	ContentType  string            `json:"contentType,omitempty"`
	SSE          string            `json:"serverSideEncryption,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

func statObject(cmd *cobra.Command, c *s3client.Client, bucket, key string) error {
	head, err := c.S3.HeadObject(cmd.Context(), &s3.HeadObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
	})
	if err != nil {
		return opErr(err)
	}
	st := objectStat{
		Bucket:       bucket,
		Key:          key,
		Size:         aws.ToInt64(head.ContentLength),
		ETag:         strings.Trim(aws.ToString(head.ETag), `"`),
		StorageClass: string(head.StorageClass),
		ContentType:  aws.ToString(head.ContentType),
		SSE:          string(head.ServerSideEncryption),
		Metadata:     head.Metadata,
	}
	if head.LastModified != nil {
		t := *head.LastModified
		st.LastModified = &t
	}
	if flagJSON {
		return printJSON(st)
	}
	mod := ""
	if st.LastModified != nil {
		mod = st.LastModified.Local().Format("2006-01-02 15:04:05")
	}
	fmt.Printf("key:      %s\n", key)
	fmt.Printf("bucket:   %s\n", bucket)
	fmt.Printf("size:     %s (%d bytes)\n", humanSize(st.Size), st.Size)
	fmt.Printf("modified: %s\n", orDefault(mod, "(unknown)"))
	if st.ETag != "" {
		fmt.Printf("etag:     %s\n", st.ETag)
	}
	if st.StorageClass != "" {
		fmt.Printf("class:    %s\n", st.StorageClass)
	}
	if st.ContentType != "" {
		fmt.Printf("type:     %s\n", st.ContentType)
	}
	if st.SSE != "" {
		fmt.Printf("SSE:      %s\n", st.SSE)
	}
	for k, v := range st.Metadata {
		fmt.Printf("meta:     %s=%s\n", k, v)
	}
	return nil
}
