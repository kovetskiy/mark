package util

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
)

type Credentials struct {
	Username string
	Password string
	BaseURL  string
	PageID   string
}

func GetCredentials(
	username string,
	password string,
	targetURL string,
	baseURL string,
	compileOnly bool,

) (*Credentials, error) {
	var err error

	// Ahead of the empty check and not after it. "-p -" does not carry a token,
	// it says where the token is, so whether one was given at all is only known
	// once standard input has been read. Checked first, the check passed on the
	// "-" itself and a run with nothing piped in went on to authenticate with
	// an empty password -- reported by Confluence as a 401, which reads as the
	// wrong credential rather than as a missing one.
	if password == "-" {
		stdin, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("unable to read password from stdin: %w", err)
		}

		password = strings.TrimSpace(string(stdin))
	}

	if password == "" {
		if !compileOnly {
			return nil, errors.New(
				"confluence password should be specified using -p " +
					"flag or be stored in configuration file",
			)
		}
		password = "none"
	}

	if compileOnly && targetURL == "" {
		targetURL = "http://localhost"
	}

	url, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("unable to parse %q as url: %w", targetURL, err)
	}

	if url.Host == "" && baseURL == "" {
		return nil, errors.New(
			"confluence base URL should be specified using -l " +
				"flag or be stored in configuration file",
		)
	}

	if baseURL == "" {
		baseURL = url.Scheme + "://" + url.Host
	}

	baseURL = strings.TrimRight(baseURL, `/`)

	pageID := url.Query().Get("pageId")

	creds := &Credentials{
		Username: username,
		Password: password,
		BaseURL:  baseURL,
		PageID:   pageID,
	}

	return creds, nil
}
