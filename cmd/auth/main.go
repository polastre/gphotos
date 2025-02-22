package main

import (
	"github.com/alexflint/go-arg"
	"github.com/polastre/gphotos"
)

func main() {
	var args struct {
		GoogleClientID     string `arg:"env:GOOGLE_CLIENT_ID,--client-id,required"`
		GoogleClientSecret string `arg:"env:GOOGLE_CLIENT_SECRET,--client-secret,required"`
	}
	arg.MustParse(&args)
	creds := gphotos.Credentials{
		ClientID:     args.GoogleClientID,
		ClientSecret: args.GoogleClientSecret,
	}
	creds.NewUserAuthorization()
}
