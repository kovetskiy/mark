package main

import (
	"errors"
	"net/url"
	"strings"

	"github.com/reconquest/karma-go"
)

type Credentials struct {
	Token    string
	Username string
	Password string
	BaseURL  string
	PageID   string
}

func GetCredentials(
	flags Flags,
	config *Config,
) (*Credentials, error) {
	var err error

	var (
		token     = flags.Token
		targetURL = flags.TargetURL
	)

	if token == "" {
		token = config.Token
		if token == "" {
			return nil, errors.New(
				"the Confluence token should be specified using -t " +
					"flag or be stored in configuration file",
			)
		}
	}

	url, err := url.Parse(targetURL)
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to parse %q as url", targetURL,
		)
	}

	baseURL := url.Scheme + "://" + url.Host

	if url.Host == "" {
		baseURL = flags.BaseURL
		if baseURL == "" {
			baseURL = config.BaseURL
		}

		if baseURL == "" {
			return nil, errors.New(
				"the Confluence base URL should be specified using -l " +
					"flag or be stored in configuration file",
			)
		}
	}

	baseURL = strings.TrimRight(baseURL, `/`)

	pageID := url.Query().Get("pageId")

	creds := &Credentials{
		Token:   token,
		BaseURL: baseURL,
		PageID:  pageID,
	}

	return creds, nil
}
