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

	if password == "" {
		if !compileOnly {
			return nil, errors.New(
				"confluence password should be specified using -p " +
					"flag or be stored in configuration file",
			)
		}
		password = "none"
	}

	if password == "-" {
		stdin, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("unable to read password from stdin: %w", err)
		}

		password = strings.TrimSpace(string(stdin))
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
