// Package egobee encapsulates types and helper functions for interacting with
// the ecobee REST API in Go.
package egobee

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"
)

const ecobeeTokenURL = "https://api.ecobee.com/token"

type reauthResponse struct {
	Err  *AuthorizationErrorResponse
	Resp *TokenRefreshResponse
}

func (r *reauthResponse) ok() bool {
	if r == nil {
		return false
	}
	return r.Err == nil && r.Resp != nil
}

func (r *reauthResponse) err() error {
	if r.Err != nil && r.Err.Error != "" && r.Err.Description != "" {
		return fmt.Errorf("unable to re-authenticate: %v: %v", r.Err.Error, r.Err.Description)
	}
	return errors.New("unable to re-authenticate for unknown reasons")
}

func reauthResponseFromHTTPResponse(resp *http.Response) (*reauthResponse, error) {
	r := &reauthResponse{}
	if (resp.StatusCode / 100) != 2 {
		r.Err = &AuthorizationErrorResponse{}
		if err := r.Err.Populate(resp.Body); err != nil {
			return nil, err
		}
	} else {
		r.Resp = &TokenRefreshResponse{}
		if err := r.Resp.Populate(resp.Body); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// authorizingTransport is a RoundTripper which includes the Access token in the
// request headers as appropriate for accessing the ecobee API.
type authorizingTransport struct {
	auth      TokenStorer
	transport http.RoundTripper
	appID     string
}

func (t *authorizingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.shouldReauth() {
		if err := t.reauth(); err != nil {
			return nil, err
		}
	}
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %v", t.auth.AccessToken()))
	return t.transport.RoundTrip(req)
}

func (t *authorizingTransport) shouldReauth() bool {
	// TODO(cfunkhouser): make the timeout customizable.
	return (t.auth.ValidFor() < (time.Second * 15)) || (t.auth.AccessToken() == "")
}

func (t *authorizingTransport) sendReauth(url string) (*reauthResponse, error) {
	refreshToken, err := t.auth.RefreshToken()
	if err != nil {
		log.Fatalf("Unable to get refreshToken for request: %v", err)
	}
	tokenURL := fmt.Sprintf("%v?grant_type=refresh_token&refresh_token=%v&client_id=%v", url, refreshToken, t.appID)
	resp, err := http.Post(tokenURL, "", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return reauthResponseFromHTTPResponse(resp)
}

func (t *authorizingTransport) reauth() error {
	r, err := t.sendReauth(ecobeeTokenURL)
	if err != nil {
		return err
	}
	if !r.ok() {
		return r.err()
	}
	t.auth.Update(r.Resp)
	return nil
}

// Client for the ecobee API.
type Client struct {
	http.Client
}

// New egobee client.
func New(appID string, ts TokenStorer) *Client {
	return &Client{
		Client: http.Client{
			Transport: &authorizingTransport{
				auth:      ts,
				transport: http.DefaultTransport,
				appID:     appID,
			},
		},
	}
}
