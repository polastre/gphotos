package gphotos

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

// S3Options provide configuration over where photos should be stored in S3
type S3Options struct {
	Bucket        string // Required. s3 bucket to upload content.
	PhotosJSONKey string // s3 key for a json dump of all the photos info, default to `photos.json`
	PhotosPrefix  string // s3 key prefix for where to put the photos, defaults to `photos/`
	Width         int    // width of the image to request from Google Photos. If not provided, gets full width
	Height        int    // height of the image to request from Google Photos. If not provided, gets full height
	AddExtension  bool   // add the extension of the file onto the s3 key. Defaults to false, uploading by Google Photos ID
}

// NewS3Options creates a new S3Options object with defaults
func NewS3Options(bucket string) S3Options {
	return S3Options{
		Bucket:        bucket,
		PhotosJSONKey: "photos.json",
		PhotosPrefix:  "photos/",
	}
}

func S3Key[T any](bucket string, filename string) ([]T, error) {
	photos := []T{}
	sess, err := session.NewSession()
	if err != nil {
		return nil, err
	}
	svc := s3.New(sess)
	obj, err := svc.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(filename),
	})
	if err != nil {
		return nil, fmt.Errorf("error fetching %s from %s: %w", filename, bucket, err)
	}
	defer obj.Body.Close()
	buf, err := io.ReadAll(obj.Body)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(buf, &photos)
	if err != nil {
		fmt.Printf("error unmarshaling photos cache file:\n%s\nerror: %v", string(buf), err)
		return nil, err
	}
	return photos, nil
}

func SetS3Key[T any](bucket string, filename string, photos []T) error {
	buf, err := json.Marshal(photos)
	if err != nil {
		return err
	}
	sess, err := session.NewSession()
	if err != nil {
		return err
	}
	uploader := s3manager.NewUploader(sess)
	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(filename),
		Body:        bytes.NewBuffer(buf),
		ContentType: aws.String("application/json"),
	})

	if err != nil {
		return err
	}
	return nil
}

// PhotoJSON returns the photos metadata json file stored in S3
func (o S3Options) PhotoJSON() ([]GooglePhotosPickedItem, error) {
	return S3Key[GooglePhotosPickedItem](o.Bucket, o.PhotosJSONKey)
}
