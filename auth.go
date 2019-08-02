package main

import (
	"errors"
	"net/url"
	"strings"

	"github.com/reconquest/karma-go"
)

type Credentials struct {
	Username string
	Password string
	BaseURL  string
	PageID   string
}

func GetCredentials(
	args map[string]interface{},
	config *Config,
) (*Credentials, error) {
	var (
		username, _  = args["-u"].(string)
		password, _  = args["-p"].(string)
		targetURL, _ = args["-l"].(string)
	)

	var err error

	if username == "" {
		username = config.Username
		if username == "" {
			return nil, errors.New(
				"Confluence username should be specified using -u " +
					"flag or be stored in configuration file",
			)
		}
	}

	if password == "" {
		password = config.Password
		if password == "" {
			return nil, errors.New(
				"Confluence password should be specified using -p " +
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
		var ok bool
		baseURL, ok = args["--base-url"].(string)
		if !ok {
			baseURL = config.BaseURL
			if baseURL == "" {
				return nil, errors.New(
					"Confluence base URL should be specified using -l " +
						"flag or be stored in configuration file",
				)
			}
		}
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
