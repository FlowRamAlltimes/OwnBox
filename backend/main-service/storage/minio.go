package s3storage

import (
	"context"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func InitMinIO(ctx context.Context, endpoint, id, password, bucket string) (*minio.Client, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Secure: false,
		Creds:  credentials.NewStaticV4(id, password, ""),
	})

	if err != nil {
		return nil, err
	}

	if ok, err := client.BucketExists(ctx, bucket); err != nil {
		return nil, err
	} else if !ok {
		err = client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{})
		if err != nil {
			return nil, err
		}
	}

	return client, nil
}
