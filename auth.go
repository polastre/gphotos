package gphotos

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	tokenUrl = "https://oauth2.googleapis.com/token"
)

// Credentials represents a Google Photos OAuth2 credential
// that can be used to get a valid access token.
type Credentials struct {
	ClientID     string // ClientID is your app's client ID from Google
	ClientSecret string // ClientSecret is your app's client secret from Google
	RefreshToken string // Refresh token is the _user's_ refresh token from first authentication that can be used to get a new access token
	AccessToken  *Token // Optionally supply a valid access token, which will be used if provided
}

// Token is a Google OAuth2 Access Token
type Token struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	ExpiresAt   time.Time
	Scope       string `json:"scope"`
	TokenType   string `json:"token_type"`
}

// Token fetches an access token for the provided credentials.
// Also sets the AccessToken field of the provided credentials.
func (c *Credentials) Token() (*Token, error) {
	// check if a token is already provided and not expired
	if c.AccessToken != nil {
		// token is expired, nil it out
		if c.AccessToken.ExpiresAt.Before(time.Now()) {
			c.AccessToken = nil
		} else {
			return c.AccessToken, nil
		}
	}
	params := url.Values{}
	params.Add("client_id", c.ClientID)
	params.Add("client_secret", c.ClientSecret)
	params.Add("refresh_token", c.RefreshToken)
	params.Add("grant_type", "refresh_token")
	body := params.Encode()

	res, err := http.Post(tokenUrl, "application/x-www-form-urlencoded", bytes.NewBufferString(body))
	if err != nil {
		return nil, err
	}
	now := time.Now() // timing for when the access token expires
	defer res.Body.Close()
	token, data, err := httpReadResponse[Token](res.Body)
	if err != nil {
		return nil, err
	}
	res.Body.Close()
	// invalid token, so there must have been an error
	if token.AccessToken == "" || token.ExpiresIn == 0 {
		oauthError := GoogleOAuthError{}
		if err := json.Unmarshal(data, &oauthError); err != nil {
			return nil, err
		}
		return nil, &oauthError
	}
	token.ExpiresAt = now.Add(time.Duration(token.ExpiresIn) * time.Second)

	c.AccessToken = token
	return token, nil
}

func (c Credentials) NewUserAuthorization() {
	config := &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		RedirectURL:  "http://localhost:8080/callback", // Replace with your redirect URL
		Scopes: []string{
			"https://www.googleapis.com/auth/userinfo.profile",
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/photoslibrary.appendonly",
			"https://www.googleapis.com/auth/photoslibrary.readonly.appcreateddata",
			"https://www.googleapis.com/auth/photoslibrary.edit.appcreateddata",
			"https://www.googleapis.com/auth/photospicker.mediaitems.readonly",
		},
		Endpoint: google.Endpoint,
	}
	url := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Visit the following URL to authorize the app:\n%v\n", url)

	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Code not found", http.StatusBadRequest)
			return
		}

		token, err := config.Exchange(ctx, code)
		if err != nil {
			http.Error(w, "Failed to exchange token: "+err.Error(), http.StatusInternalServerError)
			return
		}
		// Use the token to access Google APIs
		client := config.Client(ctx, token)
		userInfo, err := client.Get("https://www.googleapis.com/oauth2/v3/userinfo")
		if err != nil {
			http.Error(w, "Failed to get user info: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer userInfo.Body.Close()
		// Process the user information
		fmt.Fprintf(w, "User info retrieved successfully!\nStore the refresh token somewhere securely.\n\n")
		// Print json version of token
		tokenJson, err := json.MarshalIndent(token, "", "  ")
		if err != nil {
			http.Error(w, "Failed to marshal token: "+err.Error(), http.StatusInternalServerError)
		}
		w.Write(tokenJson)
	})

	http.ListenAndServe(":8080", nil)
}

// GoogleOAuthError represents an error during an OAuth exchange with Google
type GoogleOAuthError struct {
	ErrorCode string `json:"error"`
	Message   string `json:"error_description"`
}

func (e GoogleOAuthError) Error() string {
	return fmt.Sprintf("%s: %s", e.ErrorCode, e.Message)
}
