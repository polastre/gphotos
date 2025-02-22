package main

import (
	"context"
	"fmt"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/polastre/gphotos"
)

func main() {
	var args struct {
		GoogleClientID     string `arg:"env:GOOGLE_CLIENT_ID,--client-id,required"`
		GoogleClientSecret string `arg:"env:GOOGLE_CLIENT_SECRET,--client-secret,required"`
		AWSAccessKeyID     string `arg:"env:AWS_ACCESS_KEY_ID,required"`
		AWSSecretAccessKey string `arg:"env:AWS_SECRET_ACCESS_KEY,required"`
		AWSRegion          string `arg:"env:AWS_REGION,--region,required"`
		Token              string `arg:"--token,-t,required" help:"Google OAuth Refresh Token"`
		Bucket             string `arg:"--bucket,-b,required" help:"Destination S3 Bucket"`
	}
	arg.MustParse(&args)

	// set AWS values in environment if they're provided by flags instead of env
	if os.Getenv("AWS_ACCESS_KEY_ID") == "" {
		os.Setenv("AWS_ACCESS_KEY_ID", args.AWSAccessKeyID)
	}
	if os.Getenv("AWS_SECRET_ACCESS_KEY") == "" {
		os.Setenv("AWS_SECRET_ACCESS_KEY", args.AWSSecretAccessKey)
	}
	if os.Getenv("AWS_REGION") == "" {
		os.Setenv("AWS_REGION", args.AWSRegion)
	}

	// need refresh token and s3 bucket as input
	creds := gphotos.Credentials{
		ClientID:     args.GoogleClientID,
		ClientSecret: args.GoogleClientSecret,
		RefreshToken: args.Token,
	}
	sesh, err := creds.NewPickerSession()
	if err != nil {
		panic(err)
	}
	fmt.Printf("Visit this URL to pick photos for the app:\n%s\n\n", sesh.PickerURI)

	photos, err := sesh.Poll(context.Background(),
		func(s *gphotos.GooglePhotosPickerSession) bool {
			if s.MediaItemsSet {
				fmt.Printf("photos have been picked for session: %s\n", s.ID)
			} else {
				fmt.Printf("checking if session is complete: %s\n", s.ID)

			}
			return true
		},
	)
	if err != nil {
		panic(err)
	}
	for _, p := range photos {
		fmt.Printf("[%s] %s (%s - %s)\n", p.ID[:8], p.Media.Filename, p.Media.Metadata.CameraMake, p.Media.Metadata.CameraModel)
	}
	fmt.Printf("%d total items, now uploading to S3\n", len(photos))

	s3opts := gphotos.NewS3Options(args.Bucket)
	s3opts.Width = 2048
	err = creds.UploadToS3(photos, s3opts)
	if err != nil {
		panic(err)
	}
	fmt.Println("uploaded photos to s3")
}
