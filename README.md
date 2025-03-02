# gphotos

[![Go Reference](https://pkg.go.dev/badge/github.com/polastre/gphotos.svg)](https://pkg.go.dev/github.com/polastre/gphotos)

Golang Google Photos package and cli utilizing the Picker API. Includes the
ability to copy media from Google Photos to S3.

## Install

```sh
go get github.com/polastre/gphotos
```

Requires go 1.24.0 or higher.

## Why this package?

There's two reasons this exists:

1. Google stopped producing a Google Photos golang package a long time ago,
   and...
2. Google [deprecated their Google Photos album/mediaItems
   APIs](https://developers.google.com/photos/support/updates) in favor of a
   [Picker
   API](https://developers.google.com/photos/picker/guides/get-started-picker).
   The Picker API is not well documented and it would be helpful to have a
   library that actually works.

## About the Picker API

Google's move to the Picker API for Google Photos completely changes how apps
integrate with photos.

Apps now have two ways of interacting via the API:

1. Media and albums created by the app itself can be accessed through the normal
   Library API. If the app is something like a photo frame or media backup
   utility, it is highly unlikely your app created the album AND the media
   inside it. Even moving photos to an album created by your app doesn't allow
   your app to see them because the photos themselves weren't uploaded by your
   app.
2. Apps can redirect users to a "Media Picker" at Google Photos by creating a
   "picker session" for that user. After the user picks their media to share
   with the app, the app has access to those media items via the Picker API
   until the picker session expires. The session usually expires within 24
   hours, which means the app only has access to the picked media for a short
   time.

Based on these observations, I've come to the conclusion that the only logical
solution is to copy off the media from Google Photos while the picker session is
still valid, for use after the picker session is complete. AWS S3 is a logical
place to store this data so that your apps can fetch the media when they need
it.

This repo and package provides implementations to support this workflow. See the
`auth` and `picker` utilities below.

## Gettings started with the API

The `picker` utility in [cmd/picker](./cmd/picker/main.go) is a good place to
start to understand the flow.

The first thing needed is Google OAuth credentials. If you don't have them,
there's a utility in `auth` that fetches the credentials from the command line
by providing a sign in link and the capturing the results in a local callback.

With Google Credentials in hand (largely out of scope of this package), create
credentials:

```go
creds := gphotos.Credentials{
    ClientID:     args.GoogleClientID,
    ClientSecret: args.GoogleClientSecret,
    RefreshToken: args.Token,
}
```

Then create a new Picker session for that user with Google Photos:

```go
sesh, err := creds.NewPickerSession()
if err != nil {
    panic(err)
}
fmt.Printf("Visit this URL to pick photos for the app:\n%s\n\n", sesh.PickerURI)
```

If you're building this into a webapp, you can redirect the user at this point
to the `PickerURI`. Be sure to do this in a new tab because Google doesn't send
the user back to your application when they're done, instead they see a screen
that says "Done" and nothing else.

From this point, poll the Google Photos Picker API for the session to see when
it is complete. You can optionally add callbacks to abort the process or print
status.

```go
photos, err := sesh.Poll(context.Background())
if err != nil {
    panic(err)
}
fmt.Printf("%d total items, now uploading to S3\n", len(photos))
```

If you'd like to upload the media to S3 when you're done, optionally use the
`UploadToS3` func.

```go
s3options := gphotos.NewS3Options(bucketName)
err := creds.UploadToS3(photos, s3options)
```

## Utility: `auth`

The `auth` cli will perform a new OAuth authentication with the Google auth
service. This is useful to get a refresh token that can be used with the Google
Photos Picker for a specific user.

```
% GOOGLE_CLIENT_ID=your_id GOOGLE_CLIENT_SECRET=your_secret go run cmd/auth/main.go
Visit the following URL to authorize the app:
https://accounts.google.com/o/oauth2/auth...
```

You can get your client ID and secret from the Google API Console. See the
[Google Photos configuration
documentation](https://developers.google.com/photos/overview/configure-your-app).

The `refresh_token` returned will be needed as the `--token` input for the
`picker` utility.

## Utility: `picker`

The `picker` cli allows you to pick some photos from Google Photos and then copy
them to a S3 bucket of your choice.

Run the command to see all the options. Note that all of the options are
required including Google config, AWS config, token, and bucket.

```
% go run cmd/picker/main.go
Usage: main --client-id CLIENT-ID --client-secret CLIENT-SECRET --awsaccesskeyid AWSACCESSKEYID --awssecretaccesskey AWSSECRETACCESSKEY --region REGION --token TOKEN --bucket BUCKET

Options:
  --client-id CLIENT-ID [env: GOOGLE_CLIENT_ID]
  --client-secret CLIENT-SECRET [env: GOOGLE_CLIENT_SECRET]
  --awsaccesskeyid AWSACCESSKEYID [env: AWS_ACCESS_KEY_ID]
  --awssecretaccesskey AWSSECRETACCESSKEY [env: AWS_SECRET_ACCESS_KEY]
  --region REGION [env: AWS_REGION]
  --token TOKEN, -t TOKEN
                         Google OAuth Refresh Token
  --bucket BUCKET, -b BUCKET
                         Destination S3 Bucket
  --help, -h             display this help and exit
```
