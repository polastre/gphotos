package gphotos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

type MediaType string

var (
	TypeUnspecified = MediaType("TYPE_UNSPECIFIED")
	TypePhoto       = MediaType("PHOTO")
	TypeVideo       = MediaType("VIDEO")
)

var (
	ErrPollingCallbackFalse = errors.New("callback returned false, so polling was halted")
)

// GooglePhotosPickerSession represents a session where a user can
// pick photos from the Google Photos Picker UI.
type GooglePhotosPickerSession struct {
	ID            string                    // ID of the session created by Google for this user
	PickerURI     string                    // URI to send the user to pick photos
	PollingURI    string                    `json:"-"` // URI to poll to find out when the user is done
	PollingConfig GooglePhotosPollingConfig // Recommended polling configuration for the Polling URI from google
	ExpireTime    time.Time                 // Time that the session expires
	MediaItemsSet bool                      // True if the user has finished picking photos
	Credentials   *Credentials              `json:"-"`     // Credentials used to create this session
	Error         *GooglePhotosError        `json:"error"` // Only present if there's been an error returned by the API
}

// GooglePhotosPollingConfig is google's recommended polling config
type GooglePhotosPollingConfig struct {
	PollInterval Duration // How often the polling uri should be polled
	TimeoutIn    string   // when the picker session times out
}

func (c *Credentials) NewPickerSession() (*GooglePhotosPickerSession, error) {
	token, err := c.Token()
	if err != nil {
		return nil, err
	}
	response, err := httpRequest(token.AccessToken,
		"POST",
		"https://photospicker.googleapis.com/v1/sessions",
		bytes.NewBuffer([]byte(`{}`)))
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	gpResponse, _, err := httpReadResponse[GooglePhotosPickerSession](response.Body)
	if err != nil {
		return nil, err
	}
	if gpResponse.Error != nil {
		return nil, gpResponse.Error
	}

	gpResponse.PollingURI = fmt.Sprintf("https://photospicker.googleapis.com/v1/sessions/%s", gpResponse.ID)
	gpResponse.Credentials = c
	return gpResponse, nil
}

// Poll polls the Google Photos session API until MediaItemsSet is true
// or an error occurs.
//
// Provide a callback func if to show progress to the user or interrupt
// the polling. Returning `false` from a callback will stop the polling
// with an `ErrPollingCallbackFalse` error.
func (s *GooglePhotosPickerSession) Poll(ctx context.Context, callbacks ...func(s *GooglePhotosPickerSession) bool) ([]GooglePhotosPickedItem, error) {
	for {
		for _, cb := range callbacks {
			res := cb(s)
			if !res {
				return nil, ErrPollingCallbackFalse
			}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// sleep for the recommended interval
		time.Sleep(time.Duration(s.PollingConfig.PollInterval))
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		token, err := s.Credentials.Token()
		if err != nil {
			return nil, err
		}
		response, err := httpRequest(token.AccessToken,
			"GET",
			s.PollingURI,
			nil)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()

		resp, _, err := httpReadResponse[GooglePhotosPickerSession](response.Body)
		if err != nil {
			return nil, err
		}
		response.Body.Close()
		if resp.Error != nil {
			return nil, resp.Error
		}
		if resp.MediaItemsSet {
			break
		}
	}

	s.MediaItemsSet = true
	for _, cb := range callbacks {
		res := cb(s)
		if !res {
			return nil, ErrPollingCallbackFalse
		}
	}

	// get all the items from this session
	return s.listPickerContents()
	// after this should delete the session, but leaving it in place for now
}

type GooglePhotosPickedItems struct {
	Items         []GooglePhotosPickedItem `json:"mediaItems"`
	NextPageToken string                   `json:"nextPageToken"`
	Error         *GooglePhotosError       `json:"error"`
}

type GooglePhotosPickedItem struct {
	ID         string
	CreateTime string
	Type       MediaType
	Media      GooglePhotosPickedMedia `json:"mediaFile"`
}

type GooglePhotosPickedMedia struct {
	BaseURL  string
	MimeType string
	Filename string
	Metadata GooglePhotosPickedMetadata `json:"mediaFileMetadata"`
}

type GooglePhotosPickedMetadata struct {
	Width       int
	Height      int
	CameraMake  string
	CameraModel string
}

type Duration time.Duration

func (d *Duration) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err
	}
	duration, err := time.ParseDuration(str)
	if err != nil {
		return err
	}
	*d = Duration(duration)
	return nil
}

func (s *GooglePhotosPickerSession) listPickerContents() ([]GooglePhotosPickedItem, error) {
	token, err := s.Credentials.Token()
	if err != nil {
		return nil, err
	}
	photos := []GooglePhotosPickedItem{}
	nextPageToken := "start"
	for nextPageToken != "" {
		baseURL := "https://photospicker.googleapis.com/v1/mediaItems"
		// Create a URL struct and add query parameters
		u, err := url.Parse(baseURL)
		if err != nil {
			return nil, err
		}
		query := u.Query()
		query.Set("sessionId", s.ID)
		if nextPageToken != "start" && nextPageToken != "" {
			query.Set("pageToken", nextPageToken)
		}
		u.RawQuery = query.Encode()

		resp, err := httpRequest(token.AccessToken,
			"GET",
			u.String(),
			nil,
		)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		items, _, err := httpReadResponse[GooglePhotosPickedItems](resp.Body)
		if err != nil {
			return nil, err
		}
		resp.Body.Close()
		if items.Error != nil {
			return nil, items.Error
		}

		photos = append(photos, items.Items...)
		nextPageToken = items.NextPageToken
	}
	return photos, nil
}

// UploadToS3 writes the photos to an S3 bucket.
//
// S3 environment variables _must_ be set, including:
//
//   - AWS_ACCESS_KEY_ID
//   - AWS_SECRET_ACCESS_KEY
//   - AWS_REGION
func (c *Credentials) UploadToS3(photos []GooglePhotosPickedItem, opts S3Options) error {
	token, err := c.Token()
	if err != nil {
		return err
	}
	for _, p := range photos {
		if err := opts.downloadAndStore(token.AccessToken, p); err != nil {
			return err
		}
	}
	return opts.SetPhotoJSON(photos)
}

func (opts S3Options) SetPhotoJSON(photos []GooglePhotosPickedItem) error {
	return SetS3Key(opts.Bucket, opts.PhotosJSONKey, photos)
}

// downloadAndStore fetches the item and overwrites whatever is already there.
// this is on purpose in case the size of the photo, etc changes then it gets updated.
func (o S3Options) downloadAndStore(token string, item GooglePhotosPickedItem) error {
	photoUrl := item.Media.BaseURL
	if o.Width != 0 {
		photoUrl = fmt.Sprintf("%s=w%d", item.Media.BaseURL, o.Width)
	}
	if o.Height != 0 {
		photoUrl = fmt.Sprintf("%s=h%d", item.Media.BaseURL, o.Height)
	}
	response, err := httpRequest(token,
		"GET",
		photoUrl,
		nil,
	)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	key := fmt.Sprintf("%s/%s", o.PhotosPrefix, item.ID)
	if o.AddExtension {
		extension := filepath.Ext(item.Media.Filename)
		if extension != "" {
			key = fmt.Sprintf("%s.%s", key, extension)
		}
	}
	sess, err := session.NewSession()
	if err != nil {
		return err
	}
	uploader := s3manager.NewUploader(sess)
	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket:      aws.String(o.Bucket),
		Key:         aws.String(key),
		Body:        response.Body,
		ContentType: aws.String(item.Media.MimeType),
	})

	return err
}

// httpRequest makes a standard google photos request
func httpRequest(token string, method string, uri string, body io.Reader) (*http.Response, error) {
	request, err := http.NewRequest(method, uri, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json; charset=UTF-8")
	request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	client := &http.Client{}
	return client.Do(request)
}

// httpReadResponse reads the response body and parses it,
// returning the type requested, the byte slice body of
// the request, and any errors that occurred.
func httpReadResponse[T any](body io.ReadCloser) (*T, []byte, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, nil, err
	}

	var resp T
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, data, err
	}
	return &resp, data, nil
}

// GooglePhotosError is the content of the error message
// returned in JSON responses from the API.
type GooglePhotosError struct {
	Code    int    `json:"code"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Details any    `json:"details"`
}

func (e GooglePhotosError) Error() string {
	return e.Message
}
